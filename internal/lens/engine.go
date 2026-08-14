package lens

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/jackc/pgx/v5"
	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
)

type StatementReader interface {
	ListEntityStatements(ctx context.Context, qid, propertyPID string) ([]domain.Statement, error)
}

type StatementWriter interface {
	CreateStatement(ctx context.Context, meta domain.WriteMeta, in domain.CreateStatementInput) (*domain.WriteResult[domain.Statement], error)
	ReviseStatement(ctx context.Context, meta domain.WriteMeta, sid string, in domain.ReviseStatementInput) (*domain.WriteResult[domain.Statement], error)
	DeprecateStatement(ctx context.Context, meta domain.WriteMeta, sid string, expectedRevision int) (*domain.WriteResult[domain.Statement], error)
}

type LensStore interface {
	GetLensByCode(ctx context.Context, code string) (*domain.LensDefinition, error)
	GetEntityByPublicID(ctx context.Context, qid string) (*domain.Entity, error)
	ResolveEntityByLensKey(ctx context.Context, doc domain.LensDocument, keyValue string) (string, error)
	FindActiveStatementBySubjectProperty(ctx context.Context, qid, pid string) (*domain.Statement, error)
	ListActiveStatementsBySubjectProperty(ctx context.Context, qid, pid string) ([]domain.Statement, error)
}

type Engine struct {
	store  LensStore
	reader StatementReader
	writer StatementWriter
}

func NewEngine(store LensStore, reader StatementReader, writer StatementWriter) *Engine {
	return &Engine{store: store, reader: reader, writer: writer}
}

func (e *Engine) ReadInstance(ctx context.Context, lensCode, key string) (map[string]any, error) {
	lens, err := e.store.GetLensByCode(ctx, lensCode)
	if err != nil {
		return nil, err
	}
	qid, err := e.store.ResolveEntityByLensKey(ctx, lens.Document, key)
	if err != nil {
		return nil, err
	}
	return e.readByEntity(ctx, lens, qid)
}

func (e *Engine) ReadInstanceByEntity(ctx context.Context, lensCode, qid string) (map[string]any, error) {
	lens, err := e.store.GetLensByCode(ctx, lensCode)
	if err != nil {
		return nil, err
	}
	return e.readByEntity(ctx, lens, qid)
}

func (e *Engine) readByEntity(ctx context.Context, lens *domain.LensDefinition, qid string) (map[string]any, error) {
	statements, err := e.reader.ListEntityStatements(ctx, qid, "")
	if err != nil {
		return nil, err
	}
	byProperty := map[string][]domain.Statement{}
	for _, st := range statements {
		byProperty[st.PropertyPID] = append(byProperty[st.PropertyPID], st)
	}
	out := map[string]any{}
	for fieldName, field := range lens.Document.Fields {
		sts := byProperty[field.Property]
		val, err := e.fieldValue(ctx, field, sts)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", fieldName, err)
		}
		if val != nil {
			out[fieldName] = val
		}
	}
	return out, nil
}

func (e *Engine) fieldValue(ctx context.Context, field domain.LensField, statements []domain.Statement) (any, error) {
	if field.NestedLens != "" {
		return e.nestedFieldValue(ctx, field, statements)
	}
	switch field.Cardinality {
	case domain.CardinalityMany:
		vals := make([]any, 0, len(statements))
		for i := range statements {
			v, err := valueToJSON(statements[i].Value, field.Type)
			if err != nil {
				return nil, err
			}
			vals = append(vals, v)
		}
		if len(vals) == 0 {
			return nil, nil
		}
		return vals, nil
	case domain.CardinalityZeroOrOne:
		if len(statements) == 0 {
			return nil, nil
		}
		return valueToJSON(statements[0].Value, field.Type)
	default:
		if len(statements) == 0 {
			return nil, nil
		}
		return valueToJSON(statements[0].Value, field.Type)
	}
}

func (e *Engine) nestedFieldValue(ctx context.Context, field domain.LensField, statements []domain.Statement) (any, error) {
	resolveOne := func(st domain.Statement) (any, error) {
		if st.Value.EntityID == nil || *st.Value.EntityID == "" {
			return nil, nil
		}
		return e.ReadInstanceByEntity(ctx, field.NestedLens, *st.Value.EntityID)
	}
	switch field.Cardinality {
	case domain.CardinalityMany:
		vals := make([]any, 0, len(statements))
		for i := range statements {
			v, err := resolveOne(statements[i])
			if err != nil {
				return nil, err
			}
			if v != nil {
				vals = append(vals, v)
			}
		}
		if len(vals) == 0 {
			return nil, nil
		}
		return vals, nil
	default:
		if len(statements) == 0 {
			return nil, nil
		}
		return resolveOne(statements[0])
	}
}

func valueToJSON(v datatype.Value, dt datatype.Type) (any, error) {
	switch dt {
	case datatype.String, datatype.URI:
		if v.String != nil {
			return *v.String, nil
		}
		if v.URI != nil {
			return *v.URI, nil
		}
	case datatype.LocalizedString:
		if v.LangMap != nil {
			if en, ok := v.LangMap["en"]; ok {
				return en, nil
			}
			return v.LangMap, nil
		}
	case datatype.EntityReference:
		if v.EntityID != nil {
			return *v.EntityID, nil
		}
	case datatype.Boolean:
		if v.Bool != nil {
			return *v.Bool, nil
		}
	case datatype.Integer:
		if v.Int64 != nil {
			return *v.Int64, nil
		}
	case datatype.Decimal:
		if v.Decimal != nil {
			return *v.Decimal, nil
		}
	}
	if err := datatype.Validate(dt, v); err != nil {
		return nil, err
	}
	return nil, nil
}

func (e *Engine) PatchInstance(ctx context.Context, meta domain.WriteMeta, lensCode, key string, in domain.LensPatchInput) (map[string]any, error) {
	lens, err := e.store.GetLensByCode(ctx, lensCode)
	if err != nil {
		return nil, err
	}
	qid, err := e.store.ResolveEntityByLensKey(ctx, lens.Document, key)
	if err != nil {
		return nil, err
	}

	for _, op := range in.Operations {
		field, ok := lens.Document.Fields[op.Field]
		if !ok {
			return nil, fmt.Errorf("unknown field %q", op.Field)
		}
		switch op.Op {
		case "set":
			if err := e.setField(ctx, meta, qid, field, op.Value); err != nil {
				return nil, err
			}
		case "clear":
			if err := e.clearField(ctx, meta, qid, field); err != nil {
				return nil, err
			}
		case "add":
			if err := e.addField(ctx, meta, qid, field, op.Value); err != nil {
				return nil, err
			}
		case "remove":
			if err := e.removeField(ctx, meta, qid, field, op.Value); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("unsupported op %q", op.Op)
		}
	}
	return e.ReadInstance(ctx, lensCode, key)
}

func (e *Engine) setField(ctx context.Context, meta domain.WriteMeta, qid string, field domain.LensField, val datatype.Value) error {
	if field.Cardinality == domain.CardinalityMany {
		return fmt.Errorf("set on many cardinality not supported; use add/remove")
	}
	if err := datatype.Validate(field.Type, val); err != nil {
		return err
	}
	existing, err := e.store.FindActiveStatementBySubjectProperty(ctx, qid, field.Property)
	if errors.Is(err, pgx.ErrNoRows) {
		pkg, err := e.packageForEntity(ctx, qid)
		if err != nil {
			return err
		}
		_, err = e.writer.CreateStatement(ctx, meta, domain.CreateStatementInput{
			PackageCode: pkg, SubjectPublicID: qid, PropertyPublicID: field.Property, Value: val,
		})
		return err
	}
	if err != nil {
		return err
	}
	_, err = e.writer.ReviseStatement(ctx, meta, existing.PublicID, domain.ReviseStatementInput{
		Value: &val, ExpectedRevision: existing.RevisionNo,
	})
	return err
}

func (e *Engine) clearField(ctx context.Context, meta domain.WriteMeta, qid string, field domain.LensField) error {
	if field.Cardinality == domain.CardinalityMany {
		sts, err := e.store.ListActiveStatementsBySubjectProperty(ctx, qid, field.Property)
		if err != nil {
			return err
		}
		for i := range sts {
			if _, err := e.writer.DeprecateStatement(ctx, meta, sts[i].PublicID, sts[i].RevisionNo); err != nil {
				return err
			}
		}
		return nil
	}
	existing, err := e.store.FindActiveStatementBySubjectProperty(ctx, qid, field.Property)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = e.writer.DeprecateStatement(ctx, meta, existing.PublicID, existing.RevisionNo)
	return err
}

func (e *Engine) addField(ctx context.Context, meta domain.WriteMeta, qid string, field domain.LensField, val datatype.Value) error {
	if field.Cardinality != domain.CardinalityMany {
		return fmt.Errorf("add requires many cardinality")
	}
	if err := datatype.Validate(field.Type, val); err != nil {
		return err
	}
	existing, err := e.store.ListActiveStatementsBySubjectProperty(ctx, qid, field.Property)
	if err != nil {
		return err
	}
	for i := range existing {
		if valuesEqual(existing[i].Value, val) {
			return nil
		}
	}
	pkg, err := e.packageForEntity(ctx, qid)
	if err != nil {
		return err
	}
	_, err = e.writer.CreateStatement(ctx, meta, domain.CreateStatementInput{
		PackageCode: pkg, SubjectPublicID: qid, PropertyPublicID: field.Property, Value: val,
	})
	return err
}

func (e *Engine) packageForEntity(ctx context.Context, qid string) (string, error) {
	ent, err := e.store.GetEntityByPublicID(ctx, qid)
	if err != nil {
		return "", err
	}
	if ent.PackageCode == "" {
		return "", fmt.Errorf("package code required")
	}
	return ent.PackageCode, nil
}

func (e *Engine) removeField(ctx context.Context, meta domain.WriteMeta, qid string, field domain.LensField, val datatype.Value) error {
	if field.Cardinality != domain.CardinalityMany {
		return fmt.Errorf("remove requires many cardinality")
	}
	existing, err := e.store.ListActiveStatementsBySubjectProperty(ctx, qid, field.Property)
	if err != nil {
		return err
	}
	for i := range existing {
		if valuesEqual(existing[i].Value, val) {
			_, err = e.writer.DeprecateStatement(ctx, meta, existing[i].PublicID, existing[i].RevisionNo)
			return err
		}
	}
	return nil
}

func valuesEqual(a, b datatype.Value) bool {
	if a.Type != "" && b.Type != "" && a.Type != b.Type {
		return false
	}
	return reflect.DeepEqual(normalizeComparable(a), normalizeComparable(b))
}

func normalizeComparable(v datatype.Value) map[string]any {
	m := map[string]any{}
	if v.EntityID != nil {
		m["entityId"] = *v.EntityID
	}
	if v.String != nil {
		m["string"] = *v.String
	}
	if v.URI != nil {
		m["uri"] = *v.URI
	}
	if v.Bool != nil {
		m["bool"] = *v.Bool
	}
	if v.Int64 != nil {
		m["int64"] = *v.Int64
	}
	if v.Decimal != nil {
		m["decimal"] = *v.Decimal
	}
	if v.Date != nil {
		m["date"] = *v.Date
	}
	if v.DateTime != nil {
		m["dateTime"] = *v.DateTime
	}
	if v.LangMap != nil {
		m["langMap"] = v.LangMap
	}
	return m
}

func emptyValue(dt datatype.Type) datatype.Value {
	switch dt {
	case datatype.String:
		s := ""
		return datatype.Value{Type: dt, String: &s}
	case datatype.URI:
		s := ""
		return datatype.Value{Type: dt, URI: &s}
	default:
		return datatype.Value{Type: dt}
	}
}
