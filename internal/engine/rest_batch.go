package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
	"github.com/l-ra/knowledge-core/internal/store"
)

// applySingleOp runs exactly one ChangeOperation through ApplyChangeSet (SoT).
// Authz runs inside ApplyChangeSet via authorizeChangeSet.
func (e *Engine) applySingleOp(ctx context.Context, meta domain.WriteMeta, op domain.ChangeOperation) (*domain.WriteResult[domain.ChangeSet], map[string]any, error) {
	if meta.OperationType == "" {
		meta.OperationType = op.Op
	}
	res, err := e.ApplyChangeSet(ctx, meta, domain.ApplyChangeSetInput{
		OperationType: meta.OperationType,
		Operations:    []domain.ChangeOperation{op},
	})
	if err != nil {
		return nil, nil, err
	}
	if res.ChangeSet == nil && !res.Replay && res.Value.PublicID != "" {
		cs := res.Value
		res.ChangeSet = &cs
	}
	results := parseBatchResults(res.ResponseRaw)
	var first map[string]any
	if len(results) > 0 {
		first = results[0]
	}
	return res, first, nil
}

func parseBatchResults(raw []byte) []map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var parsed struct {
		Results []map[string]any `json:"results"`
	}
	if json.Unmarshal(raw, &parsed) != nil {
		return nil
	}
	return parsed.Results
}

func (e *Engine) withOpenRead(ctx context.Context, meta domain.WriteMeta) context.Context {
	if meta.OpenChangeSetID == "" {
		return ctx
	}
	return store.WithActiveChangeSets(ctx, []string{meta.OpenChangeSetID})
}

func resultString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return v
}

func (e *Engine) loadEntityAfterWrite(ctx context.Context, meta domain.WriteMeta, publicID string) (*domain.Entity, error) {
	ctx = e.withOpenRead(ctx, meta)
	ent, err := e.store.GetEntityByPublicID(ctx, publicID)
	if err != nil {
		return nil, mapErr(err)
	}
	return ent, nil
}

func (e *Engine) loadPropertyAfterWrite(ctx context.Context, meta domain.WriteMeta, publicID string) (*domain.Property, error) {
	ctx = e.withOpenRead(ctx, meta)
	p, err := e.store.GetPropertyByPublicID(ctx, publicID)
	if err == nil {
		return p, nil
	}
	ent, err2 := e.store.GetEntityByPublicID(ctx, publicID)
	if err2 != nil || ent.PropertyProfile == nil {
		return nil, mapErr(err)
	}
	return &domain.Property{
		ID: ent.ID, PublicID: ent.PublicID, Datatype: ent.PropertyProfile.Datatype,
		Status: domain.PropertyActive, PackageCode: ent.PackageCode, IRILocal: ent.IRILocal, IRI: ent.IRI,
		Labels: ent.Labels, Descriptions: ent.Descriptions, Constraints: ent.PropertyProfile.Constraints,
		RevisionNo: ent.RevisionNo, CreatedAt: ent.CreatedAt, UpdatedAt: ent.UpdatedAt,
	}, nil
}

func (e *Engine) loadStatementAfterWrite(ctx context.Context, meta domain.WriteMeta, publicID string) (*domain.Statement, error) {
	ctx = e.withOpenRead(ctx, meta)
	st, err := e.store.GetStatementByPublicID(ctx, publicID)
	if err != nil {
		return nil, mapErr(err)
	}
	return e.presentStatement(ctx, st)
}

func (e *Engine) loadClassAfterWrite(ctx context.Context, meta domain.WriteMeta, publicID string) (*domain.ClassDefinition, error) {
	ctx = e.withOpenRead(ctx, meta)
	ent, err := e.store.GetEntityByPublicID(ctx, publicID)
	if err != nil {
		return nil, mapErr(err)
	}
	sub := ""
	if ent.ClassProfile != nil {
		sub = ent.ClassProfile.SubClassOf
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return &domain.ClassDefinition{
		ID: publicID, PublicID: publicID, PackageCode: ent.PackageCode, IRILocal: ent.IRILocal, IRI: ent.IRI,
		CanonicalEntityID: ent.ID.String(), CanonicalEntityQID: publicID,
		Status: domain.PropertyActive, Labels: ent.Labels, Descriptions: ent.Descriptions,
		Document:  domain.ClassDocument{SubClassOf: sub},
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

// --- REST graph writes as 1-op batch wrappers ---

func (e *Engine) CreateEntity(ctx context.Context, meta domain.WriteMeta, in domain.CreateEntityInput) (*domain.WriteResult[domain.Entity], error) {
	batch, first, err := e.applySingleOp(ctx, meta, domain.ChangeOperation{
		Op: "createEntity", PackageCode: in.PackageCode, Labels: in.Labels,
		Descriptions: in.Descriptions, IRILocal: in.IRILocal,
	})
	if err != nil {
		return nil, err
	}
	eid := resultString(first, "entity")
	if eid == "" {
		return nil, fmt.Errorf("%w: batch createEntity missing entity id", ErrInvalid)
	}
	ent, err := e.loadEntityAfterWrite(ctx, meta, eid)
	if err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Entity]{Value: *ent, ChangeSet: batch.ChangeSet, Replay: batch.Replay}, nil
}

func (e *Engine) UpdateEntity(ctx context.Context, meta domain.WriteMeta, qid string, in domain.UpdateEntityInput) (*domain.WriteResult[domain.Entity], error) {
	if _, _, err := datatype.ParsePublicGraphID(qid); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	op := domain.ChangeOperation{
		Op: "updateEntity", Entity: qid, Labels: in.Labels, Descriptions: in.Descriptions,
		ExpectedRevision: in.ExpectedRevision,
	}
	if in.IRILocal != nil {
		op.IRILocal = *in.IRILocal
	}
	batch, first, err := e.applySingleOp(ctx, meta, op)
	if err != nil {
		return nil, err
	}
	eid := resultString(first, "entity")
	if eid == "" {
		eid = qid
	}
	ent, err := e.loadEntityAfterWrite(ctx, meta, eid)
	if err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Entity]{Value: *ent, ChangeSet: batch.ChangeSet, Replay: batch.Replay}, nil
}

func (e *Engine) DeprecateEntity(ctx context.Context, meta domain.WriteMeta, qid string, expectedRevision int) (*domain.WriteResult[domain.Entity], error) {
	if _, _, err := datatype.ParsePublicGraphID(qid); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	batch, first, err := e.applySingleOp(ctx, meta, domain.ChangeOperation{
		Op: "deprecateEntity", Entity: qid, ExpectedRevision: expectedRevision,
	})
	if err != nil {
		return nil, err
	}
	eid := resultString(first, "entity")
	if eid == "" {
		eid = qid
	}
	ent, err := e.loadEntityAfterWrite(ctx, meta, eid)
	if err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Entity]{Value: *ent, ChangeSet: batch.ChangeSet, Replay: batch.Replay}, nil
}

func (e *Engine) DeleteEntity(ctx context.Context, meta domain.WriteMeta, qid string, expectedRevision int) (*domain.WriteResult[domain.Entity], error) {
	if _, _, err := datatype.ParsePublicGraphID(qid); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	batch, first, err := e.applySingleOp(ctx, meta, domain.ChangeOperation{
		Op: "deleteEntity", Entity: qid, ExpectedRevision: expectedRevision,
	})
	if err != nil {
		return nil, err
	}
	eid := resultString(first, "entity")
	if eid == "" {
		eid = qid
	}
	ent, err := e.loadEntityAfterWrite(ctx, meta, eid)
	if err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Entity]{Value: *ent, ChangeSet: batch.ChangeSet, Replay: batch.Replay}, nil
}

func (e *Engine) SetEntityIRIAliases(ctx context.Context, meta domain.WriteMeta, qid string, aliases []domain.EntityIRIAlias) (*domain.WriteResult[domain.Entity], error) {
	if _, _, err := datatype.ParsePublicGraphID(qid); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	batch, first, err := e.applySingleOp(ctx, meta, domain.ChangeOperation{
		Op: "setEntityIRIAliases", Entity: qid, Aliases: aliases,
	})
	if err != nil {
		return nil, err
	}
	eid := resultString(first, "entity")
	if eid == "" {
		eid = qid
	}
	ent, err := e.loadEntityAfterWrite(ctx, meta, eid)
	if err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Entity]{Value: *ent, ChangeSet: batch.ChangeSet, Replay: batch.Replay}, nil
}

func (e *Engine) CreateProperty(ctx context.Context, meta domain.WriteMeta, in domain.CreatePropertyInput) (*domain.WriteResult[domain.Property], error) {
	op := domain.ChangeOperation{
		Op: "createProperty", PackageCode: in.PackageCode, Datatype: string(in.Datatype),
		Labels: in.Labels, Descriptions: in.Descriptions, IRILocal: in.IRILocal,
	}
	if in.Constraints.DomainClasses != nil || in.Constraints.RangeClasses != nil || in.Constraints.MinCount != nil || in.Constraints.MaxCount != nil {
		c := in.Constraints
		op.Constraints = &c
	}
	batch, first, err := e.applySingleOp(ctx, meta, op)
	if err != nil {
		return nil, err
	}
	pid := resultString(first, "property")
	if pid == "" {
		return nil, fmt.Errorf("%w: batch createProperty missing id", ErrInvalid)
	}
	p, err := e.loadPropertyAfterWrite(ctx, meta, pid)
	if err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Property]{Value: *p, ChangeSet: batch.ChangeSet, Replay: batch.Replay}, nil
}

func (e *Engine) UpdateProperty(ctx context.Context, meta domain.WriteMeta, pid string, in domain.UpdatePropertyInput) (*domain.WriteResult[domain.Property], error) {
	if _, err := datatype.ParsePublicPropertyID(pid); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	op := domain.ChangeOperation{Op: "updateProperty", Entity: pid, ExpectedRevision: in.ExpectedRevision}
	if in.Constraints != nil {
		op.Constraints = in.Constraints
	}
	batch, first, err := e.applySingleOp(ctx, meta, op)
	if err != nil {
		return nil, err
	}
	id := resultString(first, "property")
	if id == "" {
		id = pid
	}
	p, err := e.loadPropertyAfterWrite(ctx, meta, id)
	if err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Property]{Value: *p, ChangeSet: batch.ChangeSet, Replay: batch.Replay}, nil
}

func (e *Engine) MoveEntity(ctx context.Context, meta domain.WriteMeta, qid string, in domain.MoveEntityInput) (*domain.WriteResult[domain.Entity], error) {
	if _, _, err := datatype.ParsePublicGraphID(qid); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	batch, first, err := e.applySingleOp(ctx, meta, domain.ChangeOperation{
		Op: "moveEntity", Entity: qid, PackageCode: in.PackageCode, ExpectedRevision: in.ExpectedRevision,
	})
	if err != nil {
		return nil, err
	}
	eid := resultString(first, "entity")
	if eid == "" {
		eid = qid
	}
	ent, err := e.loadEntityAfterWrite(ctx, meta, eid)
	if err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Entity]{Value: *ent, ChangeSet: batch.ChangeSet, Replay: batch.Replay}, nil
}

func (e *Engine) CreateClass(ctx context.Context, meta domain.WriteMeta, in domain.CreateClassInput) (*domain.WriteResult[domain.ClassDefinition], error) {
	batch, first, err := e.applySingleOp(ctx, meta, domain.ChangeOperation{
		Op: "createClass", PackageCode: in.PackageCode, Labels: in.Labels, Descriptions: in.Descriptions,
		SubClassOf: in.SubClassOf, IRILocal: in.IRILocal,
	})
	if err != nil {
		return nil, err
	}
	cid := resultString(first, "class")
	if cid == "" {
		return nil, fmt.Errorf("%w: batch createClass missing id", ErrInvalid)
	}
	c, err := e.loadClassAfterWrite(ctx, meta, cid)
	if err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.ClassDefinition]{Value: *c, ChangeSet: batch.ChangeSet, Replay: batch.Replay}, nil
}

func (e *Engine) CreateStatement(ctx context.Context, meta domain.WriteMeta, in domain.CreateStatementInput) (*domain.WriteResult[domain.Statement], error) {
	if meta.ValidationMode == domain.ValidationStrict {
		if err := e.checkStrictValidation(ctx, in.SubjectPublicID, &in); err != nil {
			return nil, err
		}
	}
	batch, first, err := e.applySingleOp(ctx, meta, domain.ChangeOperation{
		Op: "createStatement", PackageCode: in.PackageCode, Subject: in.SubjectPublicID, Property: in.PropertyPublicID,
		Value: in.Value, Qualifiers: in.Qualifiers, ReferenceIDs: in.ReferenceIDs,
		ValidFrom: in.ValidFrom, ValidTo: in.ValidTo, Upsert: in.Upsert,
	})
	if err != nil {
		return nil, err
	}
	sid := resultString(first, "statement")
	if sid == "" {
		return nil, fmt.Errorf("%w: batch createStatement missing id", ErrInvalid)
	}
	st, err := e.loadStatementAfterWrite(ctx, meta, sid)
	if err != nil {
		return nil, err
	}
	out := &domain.WriteResult[domain.Statement]{Value: *st, ChangeSet: batch.ChangeSet, Replay: batch.Replay}
	if hit, _ := first["upsertHit"].(bool); hit {
		out.Replay = true
		if batch.ChangeSet == nil || batch.Replay {
			out.ChangeSet = nil
		}
	}
	e.attachValidation(ctx, meta, in.SubjectPublicID, out)
	return out, nil
}

func (e *Engine) ReviseStatement(ctx context.Context, meta domain.WriteMeta, sid string, in domain.ReviseStatementInput) (*domain.WriteResult[domain.Statement], error) {
	if _, err := datatype.ParsePublicStatementID(sid); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	op := domain.ChangeOperation{
		Op: "reviseStatement", Statement: sid, ExpectedRevision: in.ExpectedRevision,
		Qualifiers: in.Qualifiers, ReplaceQualifiers: in.ReplaceQualifiers,
		ReferenceIDs: in.ReferenceIDs, ReplaceReferences: in.ReplaceReferences,
		ValidFrom: in.ValidFrom, ValidTo: in.ValidTo, ReplaceValidTime: in.ReplaceValidTime,
	}
	if in.Value != nil {
		op.Value = *in.Value
	}
	batch, first, err := e.applySingleOp(ctx, meta, op)
	if err != nil {
		return nil, err
	}
	id := resultString(first, "statement")
	if id == "" {
		id = sid
	}
	st, err := e.loadStatementAfterWrite(ctx, meta, id)
	if err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Statement]{Value: *st, ChangeSet: batch.ChangeSet, Replay: batch.Replay}, nil
}

func (e *Engine) DeprecateStatement(ctx context.Context, meta domain.WriteMeta, sid string, expectedRevision int) (*domain.WriteResult[domain.Statement], error) {
	if _, err := datatype.ParsePublicStatementID(sid); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	batch, first, err := e.applySingleOp(ctx, meta, domain.ChangeOperation{
		Op: "deprecateStatement", Statement: sid, ExpectedRevision: expectedRevision,
	})
	if err != nil {
		return nil, err
	}
	id := resultString(first, "statement")
	if id == "" {
		id = sid
	}
	st, err := e.loadStatementAfterWrite(ctx, meta, id)
	if err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Statement]{Value: *st, ChangeSet: batch.ChangeSet, Replay: batch.Replay}, nil
}
