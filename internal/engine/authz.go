package engine

import (
	"context"
	"errors"
	"fmt"

	"github.com/rasekl/knowledge-core/internal/auth"
	"github.com/rasekl/knowledge-core/internal/domain"
)

var ErrForbidden = errors.New("forbidden")

func (e *Engine) authorizeEntity(ctx context.Context, op auth.Operation, qid string) error {
	sub, ok := auth.SubjectFromContext(ctx)
	if !ok || e.auth == nil {
		return ErrForbidden
	}
	attrs, _ := e.store.LoadEntityAuthAttributes(ctx, qid)
	allowed, err := e.auth.Allow(ctx, sub, op, auth.Resource{
		Type:     auth.ResourceEntity,
		PublicID: qid,
		EntityID: qid,
	}, attrs)
	if err != nil {
		return err
	}
	if !allowed {
		if op == auth.OpDiscover || op == auth.OpRead {
			return ErrNotFound
		}
		return ErrForbidden
	}
	return nil
}

func (e *Engine) authorizeStatementProperty(ctx context.Context, op auth.Operation, entityID, propertyID string) error {
	sub, ok := auth.SubjectFromContext(ctx)
	if !ok || e.auth == nil {
		return ErrForbidden
	}
	attrs, _ := e.store.LoadEntityAuthAttributes(ctx, entityID)
	allowed, err := e.auth.Allow(ctx, sub, op, auth.Resource{
		Type:       auth.ResourceStatement,
		EntityID:   entityID,
		PropertyID: propertyID,
	}, attrs)
	if err != nil {
		return err
	}
	if !allowed {
		if op == auth.OpRead || op == auth.OpDiscover {
			return ErrNotFound
		}
		return ErrForbidden
	}
	return nil
}

func (e *Engine) authorizeGlobal(ctx context.Context, op auth.Operation) error {
	sub, ok := auth.SubjectFromContext(ctx)
	if !ok || e.auth == nil {
		return ErrForbidden
	}
	allowed, err := e.auth.Allow(ctx, sub, op, auth.Resource{Type: auth.ResourceGlobal}, nil)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}

func (e *Engine) authorizePackage(ctx context.Context, op auth.Operation, code string) error {
	sub, ok := auth.SubjectFromContext(ctx)
	if !ok || e.auth == nil {
		return ErrForbidden
	}
	allowed, err := e.auth.Allow(ctx, sub, op, auth.Resource{
		Type:     auth.ResourcePackage,
		PublicID: code,
	}, nil)
	if err != nil {
		return err
	}
	if !allowed {
		if op == auth.OpDiscover || op == auth.OpRead {
			return ErrNotFound
		}
		return ErrForbidden
	}
	return nil
}

func (e *Engine) authorizeChangeSet(ctx context.Context, in domain.ApplyChangeSetInput) error {
	for _, op := range in.Operations {
		switch op.Op {
		case "reviseStatement":
			if op.Statement == "" {
				return fmt.Errorf("%w: missing statement", ErrInvalid)
			}
			st, err := e.store.GetStatementByPublicID(ctx, op.Statement)
			if err != nil {
				return mapErr(err)
			}
			if err := e.authorizeEntity(ctx, auth.OpDiscover, st.SubjectQID); err != nil {
				return err
			}
			if err := e.authorizeStatementProperty(ctx, auth.OpUpdate, st.SubjectQID, st.PropertyPID); err != nil {
				return err
			}
		case "updateEntity":
			if op.Entity == "" {
				return fmt.Errorf("%w: missing entity", ErrInvalid)
			}
			if err := e.authorizeEntity(ctx, auth.OpUpdate, op.Entity); err != nil {
				return err
			}
		default:
			return fmt.Errorf("%w: unsupported operation %q", ErrInvalid, op.Op)
		}
	}
	return nil
}

func (e *Engine) filterStatements(ctx context.Context, qid string, list []domain.Statement) ([]domain.Statement, error) {
	if e.auth == nil {
		return list, nil
	}
	if err := e.authorizeEntity(ctx, auth.OpDiscover, qid); err != nil {
		return nil, err
	}
	out := make([]domain.Statement, 0, len(list))
	for i := range list {
		if err := e.authorizeStatementProperty(ctx, auth.OpRead, qid, list[i].PropertyPID); err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, err
		}
		out = append(out, list[i])
	}
	return out, nil
}
