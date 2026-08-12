package engine

import (
	"context"
	"fmt"

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

func (e *Engine) GetChangeSetDraft(ctx context.Context) (*domain.ChangeSetDraft, error) {
	sub, ok := auth.SubjectFromContext(ctx)
	if !ok {
		return nil, ErrForbidden
	}
	d, err := e.store.GetChangeSetDraft(ctx, sub.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	return d, nil
}

func (e *Engine) PutChangeSetDraft(ctx context.Context, draft domain.ChangeSetDraft) (*domain.ChangeSetDraft, error) {
	sub, ok := auth.SubjectFromContext(ctx)
	if !ok {
		return nil, ErrForbidden
	}
	d, err := e.store.PutChangeSetDraft(ctx, sub.ID, draft)
	if err != nil {
		return nil, mapErr(err)
	}
	return d, nil
}

func (e *Engine) DeleteChangeSetDraft(ctx context.Context) error {
	sub, ok := auth.SubjectFromContext(ctx)
	if !ok {
		return ErrForbidden
	}
	return mapErr(e.store.DeleteChangeSetDraft(ctx, sub.ID))
}

func (e *Engine) CommitChangeSetDraft(ctx context.Context, meta domain.WriteMeta) (*domain.WriteResult[domain.ChangeSet], error) {
	sub, ok := auth.SubjectFromContext(ctx)
	if !ok {
		return nil, ErrForbidden
	}
	draft, err := e.store.GetChangeSetDraft(ctx, sub.ID)
	if err != nil {
		return nil, mapErr(err)
	}
	if !draft.Open || len(draft.Operations) == 0 {
		return nil, fmt.Errorf("%w: draft is empty or not open", ErrInvalid)
	}
	res, err := e.ApplyChangeSet(ctx, meta, domain.ApplyChangeSetInput{
		OperationType: "draftCommit",
		Comment:       draft.Title,
		Operations:    draft.Operations,
	})
	if err != nil {
		return nil, err
	}
	_ = e.store.DeleteChangeSetDraft(ctx, sub.ID)
	return res, nil
}
