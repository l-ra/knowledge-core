package lens

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/rasekl/knowledge-core/internal/datatype"
	"github.com/rasekl/knowledge-core/internal/domain"
)

type StatementReader interface {
	ListEntityStatements(ctx context.Context, qid string) ([]domain.Statement, error)
}

type StatementWriter interface {
	CreateStatement(ctx context.Context, meta domain.WriteMeta, in domain.CreateStatementInput) (*domain.WriteResult[domain.Statement], error)
	ReviseStatement(ctx context.Context, meta domain.WriteMeta, sid string, in domain.ReviseStatementInput) (*domain.WriteResult[domain.Statement], error)
}

type LensStore interface {
	GetLensByCode(ctx context.Context, code string) (*domain.LensDefinition, error)
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
	statements, err := e.reader.ListEntityStatements(ctx, qid)
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
		val, err := fieldValue(field, sts)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", fieldName, err)
		}
		if val != nil {
			out[fieldName] = val
		}
	}
	return out, nil
}

func fieldValue(field domain.LensField, statements []domain.Statement) (any, error) {
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
		default:
			return nil, fmt.Errorf("unsupported op %q", op.Op)
		}
	}
	return e.ReadInstance(ctx, lensCode, key)
}

func (e *Engine) setField(ctx context.Context, meta domain.WriteMeta, qid string, field domain.LensField, val datatype.Value) error {
	if field.Cardinality == domain.CardinalityMany {
		return fmt.Errorf("set on many cardinality not supported in v1")
	}
	if err := datatype.Validate(field.Type, val); err != nil {
		return err
	}
	existing, err := e.store.FindActiveStatementBySubjectProperty(ctx, qid, field.Property)
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = e.writer.CreateStatement(ctx, meta, domain.CreateStatementInput{
			SubjectPublicID: qid, PropertyPublicID: field.Property, Value: val,
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
	existing, err := e.store.FindActiveStatementBySubjectProperty(ctx, qid, field.Property)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	empty := emptyValue(field.Type)
	_, err = e.writer.ReviseStatement(ctx, meta, existing.PublicID, domain.ReviseStatementInput{
		Value: &empty, ExpectedRevision: existing.RevisionNo,
	})
	return err
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
