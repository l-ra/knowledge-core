package engine

import (
	"context"

	"github.com/rasekl/knowledge-core/internal/auth"
	"github.com/rasekl/knowledge-core/internal/domain"
)

func (e *Engine) CreatePackage(ctx context.Context, meta domain.WriteMeta, in domain.CreatePackageInput) (*domain.WriteResult[domain.Package], error) {
	if err := e.authorizeGlobal(ctx, auth.OpManage); err != nil {
		return nil, err
	}
	res, err := e.store.CreatePackage(ctx, meta, in)
	if err != nil {
		return nil, mapErr(err)
	}
	return res, nil
}

func (e *Engine) GetPackage(ctx context.Context, code string) (*domain.Package, error) {
	if err := e.authorizePackage(ctx, auth.OpRead, code); err != nil {
		return nil, err
	}
	p, err := e.store.GetPackageByCode(ctx, code)
	if err != nil {
		return nil, mapErr(err)
	}
	return p, nil
}

func (e *Engine) PublishRelease(ctx context.Context, meta domain.WriteMeta, code string, in domain.PublishReleaseInput) (*domain.WriteResult[domain.Release], error) {
	if err := e.authorizePackage(ctx, auth.OpManage, code); err != nil {
		return nil, err
	}
	res, err := e.store.PublishRelease(ctx, meta, code, in)
	if err != nil {
		return nil, mapErr(err)
	}
	return res, nil
}

func (e *Engine) GetRelease(ctx context.Context, code, version string) (*domain.Release, error) {
	if err := e.authorizePackage(ctx, auth.OpRead, code); err != nil {
		return nil, err
	}
	r, err := e.store.GetRelease(ctx, code, version)
	if err != nil {
		return nil, mapErr(err)
	}
	return r, nil
}

func (e *Engine) ExportReleaseBundle(ctx context.Context, code, version string) (*domain.Bundle, error) {
	if err := e.authorizePackage(ctx, auth.OpRead, code); err != nil {
		return nil, err
	}
	b, err := e.store.ExportReleaseBundle(ctx, code, version)
	if err != nil {
		return nil, mapErr(err)
	}
	return b, nil
}

func (e *Engine) ImportReleaseBundle(ctx context.Context, meta domain.WriteMeta, bundle domain.Bundle) (*domain.WriteResult[domain.Release], error) {
	if err := e.authorizeGlobal(ctx, auth.OpManage); err != nil {
		return nil, err
	}
	res, err := e.store.ImportReleaseBundle(ctx, meta, bundle)
	if err != nil {
		return nil, mapErr(err)
	}
	return res, nil
}

func (e *Engine) MutateRelease(ctx context.Context, code, version string) error {
	if err := e.authorizePackage(ctx, auth.OpManage, code); err != nil {
		return err
	}
	return mapErr(e.store.MutateRelease(ctx, code, version))
}
