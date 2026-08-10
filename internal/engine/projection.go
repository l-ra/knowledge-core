package engine

import (
	"context"

	"github.com/rasekl/knowledge-core/internal/auth"
	"github.com/rasekl/knowledge-core/internal/domain"
	"github.com/rasekl/knowledge-core/internal/projector"
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
	if err := e.authorizeGlobal(ctx, auth.OpRead); err != nil {
		return nil, err
	}
	hits, err := e.store.SearchProjection(ctx, query, limit)
	if err != nil {
		return nil, mapErr(err)
	}
	return hits, nil
}
