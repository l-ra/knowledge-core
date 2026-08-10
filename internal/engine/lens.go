package engine

import (
	"context"

	"github.com/l-ra/knowledge-core/internal/auth"
	"github.com/l-ra/knowledge-core/internal/domain"
	"github.com/l-ra/knowledge-core/internal/lens"
)

func (e *Engine) lensEngine() *lens.Engine {
	return lens.NewEngine(e.store, e, e)
}

func (e *Engine) CreateLens(ctx context.Context, in domain.CreateLensInput) (*domain.LensDefinition, error) {
	if err := e.authorizeGlobal(ctx, auth.OpManage); err != nil {
		return nil, err
	}
	l, err := e.store.CreateLens(ctx, in)
	if err != nil {
		return nil, mapErr(err)
	}
	return l, nil
}

func (e *Engine) GetLens(ctx context.Context, code string) (*domain.LensDefinition, error) {
	if err := e.authorizeGlobal(ctx, auth.OpRead); err != nil {
		return nil, err
	}
	l, err := e.store.GetLensByCode(ctx, code)
	if err != nil {
		return nil, mapErr(err)
	}
	return l, nil
}

func (e *Engine) ReadLensInstance(ctx context.Context, lensCode, key string) (map[string]any, error) {
	out, err := e.lensEngine().ReadInstance(ctx, lensCode, key)
	if err != nil {
		return nil, mapErr(err)
	}
	return out, nil
}

func (e *Engine) PatchLensInstance(ctx context.Context, meta domain.WriteMeta, lensCode, key string, in domain.LensPatchInput) (map[string]any, error) {
	out, err := e.lensEngine().PatchInstance(ctx, meta, lensCode, key, in)
	if err != nil {
		return nil, mapErr(err)
	}
	return out, nil
}
