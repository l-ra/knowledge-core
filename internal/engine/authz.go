package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/l-ra/knowledge-core/internal/auth"
	"github.com/l-ra/knowledge-core/internal/domain"
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
		case "createEntity", "createProperty", "createClass":
			if _, ok := auth.SubjectFromContext(ctx); !ok {
				return ErrForbidden
			}
			if err := e.authorizeGlobal(ctx, auth.OpCreate); err != nil {
				return err
			}
		case "createStatement":
			if op.Subject == "" || op.Property == "" {
				return fmt.Errorf("%w: createStatement requires subject and property", ErrInvalid)
			}
			subj := op.Subject
			if strings.HasPrefix(subj, "$") {
				continue
			}
			if err := e.authorizeEntity(ctx, auth.OpDiscover, subj); err != nil {
				return err
			}
			prop := op.Property
			if strings.HasPrefix(prop, "$") {
				continue
			}
			if err := e.authorizeStatementProperty(ctx, auth.OpUpdate, subj, prop); err != nil {
				return err
			}
		case "reviseStatement", "deprecateStatement":
			if op.Statement == "" {
				return fmt.Errorf("%w: missing statement", ErrInvalid)
			}
			if strings.HasPrefix(op.Statement, "$") {
				continue
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
		case "updateEntity", "deprecateEntity", "setEntityIRIAliases", "moveEntity", "updateProperty":
			if op.Entity == "" && op.Op != "updateProperty" {
				return fmt.Errorf("%w: missing entity", ErrInvalid)
			}
			if op.Op == "updateProperty" && op.Entity == "" && op.Property == "" {
				return fmt.Errorf("%w: missing entity", ErrInvalid)
			}
			target := op.Entity
			if target == "" {
				target = op.Property
			}
			if strings.HasPrefix(target, "$") {
				continue
			}
			if err := e.authorizeEntity(ctx, auth.OpUpdate, target); err != nil {
				return err
			}
			if op.Op == "moveEntity" && op.PackageCode != "" {
				if err := e.authorizePackage(ctx, auth.OpUpdate, op.PackageCode); err != nil {
					return err
				}
			}
		case "deleteEntity":
			if op.Entity == "" {
				return fmt.Errorf("%w: missing entity", ErrInvalid)
			}
			if strings.HasPrefix(op.Entity, "$") {
				continue
			}
			if err := e.authorizeEntity(ctx, auth.OpDelete, op.Entity); err != nil {
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
