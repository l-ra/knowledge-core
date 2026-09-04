package engine

import (
	"context"

	"github.com/l-ra/knowledge-core/internal/auth"
	"github.com/l-ra/knowledge-core/internal/domain"
)

func (e *Engine) ListReleases(ctx context.Context, code string) ([]domain.Release, error) {
	if err := e.authorizePackage(ctx, auth.OpRead, code); err != nil {
		return nil, err
	}
	items, err := e.store.ListReleases(ctx, code)
	if err != nil {
		return nil, mapErr(err)
	}
	return items, nil
}

func (e *Engine) ListPackageObjects(ctx context.Context, code string) ([]domain.PackageObject, error) {
	if err := e.authorizePackage(ctx, auth.OpRead, code); err != nil {
		return nil, err
	}
	items, err := e.store.ListPackageObjects(ctx, code)
	if err != nil {
		return nil, mapErr(err)
	}
	return items, nil
}

func (e *Engine) ListObjectReleases(ctx context.Context, publicID string) ([]domain.ObjectRelease, error) {
	if _, ok := auth.SubjectFromContext(ctx); !ok {
		return nil, ErrForbidden
	}
	items, err := e.store.ListObjectReleases(ctx, publicID)
	if err != nil {
		return nil, mapErr(err)
	}
	return items, nil
}

func (e *Engine) authorizeOpenChangeSetActor(ctx context.Context, actor string) error {
	sub, ok := auth.SubjectFromContext(ctx)
	if !ok {
		return ErrForbidden
	}
	if actor == "" || actor == sub.ID {
		return nil
	}
	if err := e.authorizeGlobal(ctx, auth.OpUpdate); err == nil {
		return nil
	}
	return ErrForbidden
}

func (e *Engine) OpenChangeSet(ctx context.Context, meta domain.WriteMeta, in domain.OpenChangeSetInput) (*domain.ChangeSet, error) {
	if err := e.authorizeGlobal(ctx, auth.OpCreate); err != nil {
		return nil, err
	}
	cs, err := e.store.OpenChangeSet(ctx, meta, in)
	if err != nil {
		return nil, mapErr(err)
	}
	return cs, nil
}

func (e *Engine) CommitOpenChangeSet(ctx context.Context, meta domain.WriteMeta, publicID string) (*domain.WriteResult[domain.ChangeSet], error) {
	cs, err := e.store.GetChangeSetByPublicID(ctx, publicID)
	if err != nil {
		return nil, mapErr(err)
	}
	if err := e.authorizeOpenChangeSetActor(ctx, cs.Actor); err != nil {
		return nil, err
	}
	if err := e.authorizeGlobal(ctx, auth.OpUpdate); err != nil {
		return nil, err
	}
	res, err := e.store.CommitOpenChangeSet(ctx, meta, publicID)
	if err != nil {
		return nil, mapErr(err)
	}
	return res, nil
}

func (e *Engine) CancelOpenChangeSet(ctx context.Context, meta domain.WriteMeta, publicID string) (*domain.ChangeSet, error) {
	cs, err := e.store.GetChangeSetByPublicID(ctx, publicID)
	if err != nil {
		return nil, mapErr(err)
	}
	if err := e.authorizeOpenChangeSetActor(ctx, cs.Actor); err != nil {
		return nil, err
	}
	out, err := e.store.CancelOpenChangeSet(ctx, meta, publicID)
	if err != nil {
		return nil, mapErr(err)
	}
	return out, nil
}
