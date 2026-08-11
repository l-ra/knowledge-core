package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/l-ra/knowledge-core/internal/auth"
	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
	"github.com/l-ra/knowledge-core/internal/store"
)

var (
	ErrNotFound            = errors.New("not found")
	ErrInvalid             = errors.New("invalid")
	ErrConflict            = errors.New("conflict")
	ErrIdempotencyConflict = errors.New("idempotency conflict")
	ErrReleaseImmutable    = store.ErrReleaseImmutable
)

type Engine struct {
	store *store.Store
	auth  *auth.Engine
}

func New(s *store.Store, authEngine *auth.Engine) *Engine {
	return &Engine{store: s, auth: authEngine}
}

func (e *Engine) CreateEntity(ctx context.Context, meta domain.WriteMeta, in domain.CreateEntityInput) (*domain.WriteResult[domain.Entity], error) {
	if err := e.authorizeGlobal(ctx, auth.OpCreate); err != nil {
		return nil, err
	}
	res, err := e.store.CreateEntity(ctx, meta, in)
	if err != nil {
		return nil, mapErr(err)
	}
	return res, nil
}

func (e *Engine) GetEntity(ctx context.Context, qid string) (*domain.Entity, error) {
	if _, err := datatype.ParsePublicEntityID(qid); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := e.authorizeEntity(ctx, auth.OpDiscover, qid); err != nil {
		return nil, err
	}
	ent, err := e.store.GetEntityByPublicID(ctx, qid)
	if err != nil {
		return nil, mapErr(err)
	}
	return ent, nil
}

func (e *Engine) UpdateEntity(ctx context.Context, meta domain.WriteMeta, qid string, in domain.UpdateEntityInput) (*domain.WriteResult[domain.Entity], error) {
	if _, err := datatype.ParsePublicEntityID(qid); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := e.authorizeEntity(ctx, auth.OpUpdate, qid); err != nil {
		return nil, err
	}
	res, err := e.store.UpdateEntity(ctx, meta, qid, in)
	if err != nil {
		return nil, mapErr(err)
	}
	return res, nil
}

func (e *Engine) CreateProperty(ctx context.Context, meta domain.WriteMeta, in domain.CreatePropertyInput) (*domain.WriteResult[domain.Property], error) {
	if err := e.authorizeGlobal(ctx, auth.OpCreate); err != nil {
		return nil, err
	}
	res, err := e.store.CreateProperty(ctx, meta, in)
	if err != nil {
		return nil, mapErr(err)
	}
	return res, nil
}

func (e *Engine) GetProperty(ctx context.Context, pid string) (*domain.Property, error) {
	if _, err := datatype.ParsePublicPropertyID(pid); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := e.authorizeGlobal(ctx, auth.OpRead); err != nil {
		return nil, err
	}
	p, err := e.store.GetPropertyByPublicID(ctx, pid)
	if err != nil {
		return nil, mapErr(err)
	}
	return p, nil
}

func (e *Engine) CreateReference(ctx context.Context, meta domain.WriteMeta, in domain.CreateReferenceInput) (*domain.WriteResult[domain.Reference], error) {
	if err := e.authorizeGlobal(ctx, auth.OpCreate); err != nil {
		return nil, err
	}
	res, err := e.store.CreateReference(ctx, meta, in)
	if err != nil {
		return nil, mapErr(err)
	}
	return res, nil
}

func (e *Engine) GetReference(ctx context.Context, rid string) (*domain.Reference, error) {
	if !strings.HasPrefix(rid, "R") {
		return nil, fmt.Errorf("%w: invalid reference id", ErrInvalid)
	}
	if err := e.authorizeGlobal(ctx, auth.OpRead); err != nil {
		return nil, err
	}
	ref, err := e.store.GetReferenceByPublicID(ctx, rid)
	if err != nil {
		return nil, mapErr(err)
	}
	return ref, nil
}

func (e *Engine) CreateStatement(ctx context.Context, meta domain.WriteMeta, in domain.CreateStatementInput) (*domain.WriteResult[domain.Statement], error) {
	if err := e.authorizeEntity(ctx, auth.OpDiscover, in.SubjectPublicID); err != nil {
		return nil, err
	}
	if err := e.authorizeStatementProperty(ctx, auth.OpUpdate, in.SubjectPublicID, in.PropertyPublicID); err != nil {
		return nil, err
	}
	if meta.ValidationMode == domain.ValidationStrict {
		if err := e.checkStrictValidation(ctx, in.SubjectPublicID, &in); err != nil {
			return nil, err
		}
	}
	res, err := e.store.CreateStatement(ctx, meta, in)
	if err != nil {
		return nil, mapErr(err)
	}
	st, err := e.presentStatement(ctx, &res.Value)
	if err != nil {
		return nil, err
	}
	res.Value = *st
	e.attachValidation(ctx, meta, in.SubjectPublicID, res)
	return res, nil
}

func (e *Engine) ReviseStatement(ctx context.Context, meta domain.WriteMeta, sid string, in domain.ReviseStatementInput) (*domain.WriteResult[domain.Statement], error) {
	if _, err := datatype.ParsePublicStatementID(sid); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	st, err := e.store.GetStatementByPublicID(ctx, sid)
	if err != nil {
		return nil, mapErr(err)
	}
	if err := e.authorizeEntity(ctx, auth.OpDiscover, st.SubjectQID); err != nil {
		return nil, err
	}
	if err := e.authorizeStatementProperty(ctx, auth.OpUpdate, st.SubjectQID, st.PropertyPID); err != nil {
		return nil, err
	}
	res, err := e.store.ReviseStatement(ctx, meta, sid, in)
	if err != nil {
		return nil, mapErr(err)
	}
	if res.Replay {
		e.attachChangeSetOnReplay(ctx, meta, res)
	}
	presented, err := e.presentStatement(ctx, &res.Value)
	if err != nil {
		return nil, err
	}
	res.Value = *presented
	return res, nil
}

func (e *Engine) DeprecateStatement(ctx context.Context, meta domain.WriteMeta, sid string, expectedRevision int) (*domain.WriteResult[domain.Statement], error) {
	if _, err := datatype.ParsePublicStatementID(sid); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	st, err := e.store.GetStatementByPublicID(ctx, sid)
	if err != nil {
		return nil, mapErr(err)
	}
	if err := e.authorizeEntity(ctx, auth.OpDiscover, st.SubjectQID); err != nil {
		return nil, err
	}
	if err := e.authorizeStatementProperty(ctx, auth.OpUpdate, st.SubjectQID, st.PropertyPID); err != nil {
		return nil, err
	}
	res, err := e.store.DeprecateStatement(ctx, meta, sid, expectedRevision)
	if err != nil {
		return nil, mapErr(err)
	}
	return res, nil
}

func (e *Engine) GetStatement(ctx context.Context, sid string) (*domain.Statement, error) {
	if _, err := datatype.ParsePublicStatementID(sid); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	st, err := e.store.GetStatementByPublicID(ctx, sid)
	if err != nil {
		return nil, mapErr(err)
	}
	if err := e.authorizeEntity(ctx, auth.OpDiscover, st.SubjectQID); err != nil {
		return nil, err
	}
	if err := e.authorizeStatementProperty(ctx, auth.OpRead, st.SubjectQID, st.PropertyPID); err != nil {
		return nil, err
	}
	return e.presentStatement(ctx, st)
}

func (e *Engine) ListEntityStatements(ctx context.Context, qid string) ([]domain.Statement, error) {
	if _, err := datatype.ParsePublicEntityID(qid); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	list, err := e.store.ListStatementsBySubject(ctx, qid)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]domain.Statement, 0, len(list))
	for i := range list {
		st, err := e.presentStatement(ctx, &list[i])
		if err != nil {
			return nil, err
		}
		out = append(out, *st)
	}
	return e.filterStatements(ctx, qid, out)
}

func (e *Engine) ApplyChangeSet(ctx context.Context, meta domain.WriteMeta, in domain.ApplyChangeSetInput) (*domain.WriteResult[domain.ChangeSet], error) {
	if err := e.authorizeChangeSet(ctx, in); err != nil {
		return nil, err
	}
	res, err := e.store.ApplyChangeSet(ctx, meta, in)
	if err != nil {
		return nil, mapErr(err)
	}
	return res, nil
}

func (e *Engine) GetChangeSet(ctx context.Context, cid string) (*domain.ChangeSet, error) {
	if err := e.authorizeGlobal(ctx, auth.OpRead); err != nil {
		return nil, err
	}
	cs, err := e.store.GetChangeSetByPublicID(ctx, cid)
	if err != nil {
		return nil, mapErr(err)
	}
	return cs, nil
}

func (e *Engine) GetEntityHistory(ctx context.Context, qid string) ([]domain.EntityRevision, error) {
	if _, err := datatype.ParsePublicEntityID(qid); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := e.authorizeEntity(ctx, auth.OpRead, qid); err != nil {
		return nil, err
	}
	h, err := e.store.GetEntityHistory(ctx, qid)
	if err != nil {
		return nil, mapErr(err)
	}
	return h, nil
}

func (e *Engine) GetStatementHistory(ctx context.Context, sid string) ([]domain.StatementRevision, error) {
	if _, err := datatype.ParsePublicStatementID(sid); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	st, err := e.store.GetStatementByPublicID(ctx, sid)
	if err != nil {
		return nil, mapErr(err)
	}
	if err := e.authorizeEntity(ctx, auth.OpDiscover, st.SubjectQID); err != nil {
		return nil, err
	}
	if err := e.authorizeStatementProperty(ctx, auth.OpRead, st.SubjectQID, st.PropertyPID); err != nil {
		return nil, err
	}
	h, err := e.store.GetStatementHistory(ctx, sid)
	if err != nil {
		return nil, mapErr(err)
	}
	for i := range h {
		if h[i].Value.Type == datatype.EntityReference && h[i].Value.EntityID != nil {
			if pub, err := lookupEntityPublic(ctx, e.store, *h[i].Value.EntityID); err == nil {
				h[i].Value.EntityID = &pub
			}
		}
	}
	return h, nil
}

func (e *Engine) presentStatement(ctx context.Context, st *domain.Statement) (*domain.Statement, error) {
	if st.Value.Type == datatype.EntityReference && st.Value.EntityID != nil {
		if pub, err := lookupEntityPublic(ctx, e.store, *st.Value.EntityID); err == nil {
			st.Value.EntityID = &pub
		}
	}
	if st.Value.Type == datatype.Quantity && st.Value.UnitEntityID != nil {
		if pub, err := lookupEntityPublic(ctx, e.store, *st.Value.UnitEntityID); err == nil {
			st.Value.UnitEntityID = &pub
		}
	}
	for i := range st.Qualifiers {
		if st.Qualifiers[i].Value.Type == datatype.EntityReference && st.Qualifiers[i].Value.EntityID != nil {
			if pub, err := lookupEntityPublic(ctx, e.store, *st.Qualifiers[i].Value.EntityID); err == nil {
				st.Qualifiers[i].Value.EntityID = &pub
			}
		}
	}
	return st, nil
}

func lookupEntityPublic(ctx context.Context, s *store.Store, idOrPublic string) (string, error) {
	var publicID string
	err := s.Pool().QueryRow(ctx, `SELECT public_id FROM entity WHERE id::text = $1 OR public_id = $1`, idOrPublic).Scan(&publicID)
	return publicID, err
}

func (e *Engine) attachChangeSetOnReplay(ctx context.Context, meta domain.WriteMeta, res *domain.WriteResult[domain.Statement]) {
	if res.ChangeSet != nil || meta.IdempotencyKey == "" {
		return
	}
	cs, err := e.store.GetChangeSetByIdempotencyKey(ctx, meta.IdempotencyKey)
	if err != nil {
		return
	}
	res.ChangeSet = cs
}

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if errors.Is(err, store.ErrConflict) {
		return ErrConflict
	}
	if errors.Is(err, store.ErrIdempotencyConflict) {
		return ErrIdempotencyConflict
	}
	if errors.Is(err, store.ErrReleaseImmutable) {
		return ErrReleaseImmutable
	}
	if errors.Is(err, store.ErrImportCollision) {
		return ErrConflict
	}
	msg := err.Error()
	if strings.Contains(msg, "label.en") ||
		strings.Contains(msg, "unknown datatype") ||
		strings.Contains(msg, "requires") ||
		strings.Contains(msg, "invalid") ||
		strings.Contains(msg, "subject:") ||
		strings.Contains(msg, "property:") ||
		strings.Contains(msg, "value entity:") ||
		strings.Contains(msg, "unsupported operation") ||
		strings.Contains(msg, "unknown field") ||
		strings.Contains(msg, "entity not found for key") ||
		strings.Contains(msg, "ambiguous lens key") ||
		strings.Contains(msg, "lens code required") ||
		strings.Contains(msg, "lens key.property") {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return err
}
