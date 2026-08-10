package engine

import (
	"context"
	"errors"

	"github.com/l-ra/knowledge-core/internal/auth"
	"github.com/l-ra/knowledge-core/internal/domain"
	"github.com/l-ra/knowledge-core/internal/projector"
)

func (e *Engine) ProcessOutbox(ctx context.Context, limit int) (int, error) {
	if err := e.authorizeGlobal(ctx, auth.OpManage); err != nil {
		return 0, err
	}
	n, err := projector.ProcessPending(ctx, e.store, limit)
	if err != nil {
		return 0, mapErr(err)
	}
	return n, nil
}

// ProcessOutboxInternal runs outbox processing without HTTP auth (CLI / CronJob).
func (e *Engine) ProcessOutboxInternal(ctx context.Context, limit int) (int, error) {
	n, err := projector.ProcessPending(ctx, e.store, limit)
	if err != nil {
		return 0, mapErr(err)
	}
	return n, nil
}

func (e *Engine) RebuildSearchProjection(ctx context.Context) error {
	if err := e.authorizeGlobal(ctx, auth.OpManage); err != nil {
		return err
	}
	if err := e.store.RebuildSearchProjection(ctx); err != nil {
		return mapErr(err)
	}
	return nil
}

func (e *Engine) SearchProjection(ctx context.Context, query string, limit int) ([]domain.SearchHit, error) {
	if _, ok := auth.SubjectFromContext(ctx); !ok {
		return nil, ErrForbidden
	}
	if limit <= 0 {
		limit = 20
	}
	// Oversample so ACL filtering cannot shrink page size in a leaky way.
	fetchLimit := limit * 5
	if fetchLimit < 50 {
		fetchLimit = 50
	}
	candidates, err := e.store.SearchProjection(ctx, query, fetchLimit)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]domain.SearchHit, 0, limit)
	for _, hit := range candidates {
		ok, err := e.allowSearchHit(ctx, hit)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		out = append(out, hit)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (e *Engine) allowSearchHit(ctx context.Context, hit domain.SearchHit) (bool, error) {
	switch hit.ObjectType {
	case "entity":
		err := e.authorizeEntity(ctx, auth.OpDiscover, hit.PublicID)
		if err == nil {
			return true, nil
		}
		if errors.Is(err, ErrNotFound) {
			return false, nil
		}
		return false, err
	case "statement":
		subject := hit.SubjectQID
		if subject == "" {
			return false, nil
		}
		if err := e.authorizeEntity(ctx, auth.OpDiscover, subject); err != nil {
			if errors.Is(err, ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		if hit.PropertyPID == "" {
			return false, nil
		}
		if err := e.authorizeStatementProperty(ctx, auth.OpRead, subject, hit.PropertyPID); err != nil {
			if errors.Is(err, ErrNotFound) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	default:
		return false, nil
	}
}

func (e *Engine) RebuildRDFProjection(ctx context.Context) error {
	if err := e.authorizeGlobal(ctx, auth.OpManage); err != nil {
		return err
	}
	if err := e.store.RebuildRDFProjection(ctx); err != nil {
		return mapErr(err)
	}
	return nil
}

func (e *Engine) ExportRDF(ctx context.Context) (string, error) {
	if err := e.authorizeGlobal(ctx, auth.OpRead); err != nil {
		return "", err
	}
	nt, err := e.store.ExportRDF(ctx)
	if err != nil {
		return "", mapErr(err)
	}
	return nt, nil
}
