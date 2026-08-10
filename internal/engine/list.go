package engine

import (
	"context"
	"errors"

	"github.com/l-ra/knowledge-core/internal/auth"
	"github.com/l-ra/knowledge-core/internal/domain"
	"github.com/l-ra/knowledge-core/internal/store"
)

func (e *Engine) ListEntities(ctx context.Context, opt store.ListOptions) ([]domain.Entity, string, error) {
	if _, ok := auth.SubjectFromContext(ctx); !ok {
		return nil, "", ErrForbidden
	}
	limit := opt.Limit
	if limit <= 0 {
		limit = 50
	}
	fetch := opt
	fetch.Limit = limit * 3
	if fetch.Limit > 200 {
		fetch.Limit = 200
	}
	items, _, err := e.store.ListEntities(ctx, fetch)
	if err != nil {
		return nil, "", mapErr(err)
	}
	out := make([]domain.Entity, 0, limit)
	for i := range items {
		if err := e.authorizeEntity(ctx, auth.OpDiscover, items[i].PublicID); err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return nil, "", err
		}
		out = append(out, items[i])
		if len(out) >= limit {
			break
		}
	}
	next := ""
	if len(out) == limit {
		next = out[len(out)-1].PublicID
	}
	return out, next, nil
}

func (e *Engine) ListProperties(ctx context.Context, opt store.ListOptions) ([]domain.Property, string, error) {
	if _, ok := auth.SubjectFromContext(ctx); !ok {
		return nil, "", ErrForbidden
	}
	items, next, err := e.store.ListProperties(ctx, opt)
	if err != nil {
		return nil, "", mapErr(err)
	}
	return items, next, nil
}

func (e *Engine) ListLenses(ctx context.Context) ([]domain.LensDefinition, error) {
	if _, ok := auth.SubjectFromContext(ctx); !ok {
		return nil, ErrForbidden
	}
	items, err := e.store.ListLenses(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	return items, nil
}

func (e *Engine) ListPackages(ctx context.Context) ([]domain.Package, error) {
	if _, ok := auth.SubjectFromContext(ctx); !ok {
		return nil, ErrForbidden
	}
	items, err := e.store.ListPackages(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	return items, nil
}
