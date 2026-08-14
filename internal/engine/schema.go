package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/l-ra/knowledge-core/internal/auth"
	"github.com/l-ra/knowledge-core/internal/domain"
	"github.com/l-ra/knowledge-core/internal/store"
	"github.com/l-ra/knowledge-core/internal/validate"
)

var ErrValidation = errors.New("validation failed")

type ValidationFailure struct {
	Result domain.ValidationResult
}

func (e ValidationFailure) Error() string {
	b, _ := json.Marshal(e.Result.Summary)
	return fmt.Sprintf("validation failed: %s", string(b))
}

func (e *Engine) ValidateEntity(ctx context.Context, qid string, opt validate.Options) (*domain.ValidationResult, error) {
	if err := e.authorizeEntity(ctx, auth.OpRead, qid); err != nil {
		return nil, err
	}
	res, err := validate.Entity(ctx, e.store, qid, opt)
	if err != nil {
		return nil, mapErr(err)
	}
	return res, nil
}

func (e *Engine) CreateValidationReport(ctx context.Context, meta domain.WriteMeta, in domain.CreateValidationReportInput) (*domain.ValidationReport, error) {
	if err := e.authorizeGlobal(ctx, auth.OpRead); err != nil {
		return nil, err
	}
	var all []domain.ValidationFinding
	summary := domain.ValidationSummary{}
	for _, qid := range in.EntityQIDs {
		res, err := e.ValidateEntity(ctx, qid, validate.Options{})
		if err != nil {
			return nil, err
		}
		all = append(all, res.Findings...)
		summary.Errors += res.Summary.Errors
		summary.Warnings += res.Summary.Warnings
		summary.Infos += res.Summary.Infos
	}
	result := domain.ValidationResult{Findings: all, Summary: summary}
	if !in.Persist {
		return &domain.ValidationReport{
			Scope: in.Scope, Findings: all, Summary: summary,
		}, nil
	}
	scope := in.Scope
	if scope == "" {
		scope = "batch"
	}
	entityQID := ""
	if len(in.EntityQIDs) == 1 {
		entityQID = in.EntityQIDs[0]
	}
	return e.store.CreateValidationReport(ctx, meta.Actor, scope, entityQID, result)
}

func (e *Engine) GetValidationReport(ctx context.Context, id string) (*domain.ValidationReport, error) {
	if err := e.authorizeGlobal(ctx, auth.OpRead); err != nil {
		return nil, err
	}
	rep, err := e.store.GetValidationReport(ctx, id)
	if err != nil {
		return nil, mapErr(err)
	}
	return rep, nil
}

func (e *Engine) checkStrictValidation(ctx context.Context, qid string, pending *domain.CreateStatementInput) error {
	res, err := validate.Entity(ctx, e.store, qid, validate.Options{PendingStatement: pending})
	if err != nil {
		return err
	}
	if validate.HasErrors(res) {
		return ValidationFailure{Result: *res}
	}
	return nil
}

func (e *Engine) attachValidation(ctx context.Context, meta domain.WriteMeta, qid string, res *domain.WriteResult[domain.Statement]) {
	if meta.ValidationMode == domain.ValidationOff {
		return
	}
	v, err := validate.Entity(ctx, e.store, qid, validate.Options{})
	if err != nil {
		return
	}
	res.Validation = v
}

func (e *Engine) CreateClass(ctx context.Context, meta domain.WriteMeta, in domain.CreateClassInput) (*domain.WriteResult[domain.ClassDefinition], error) {
	if err := e.authorizeGlobal(ctx, auth.OpCreate); err != nil {
		return nil, err
	}
	res, err := e.store.CreateClass(ctx, meta, in)
	if err != nil {
		return nil, mapErr(err)
	}
	return res, nil
}

func (e *Engine) GetClass(ctx context.Context, cid string) (*domain.ClassDefinition, error) {
	if err := e.authorizeGlobal(ctx, auth.OpRead); err != nil {
		return nil, err
	}
	c, err := e.store.GetClassByPublicID(ctx, cid)
	if err != nil {
		return nil, mapErr(err)
	}
	anc, err := e.store.ClassAncestry(ctx, cid)
	if err != nil {
		return nil, mapErr(err)
	}
	c.EffectiveClasses = anc
	return c, nil
}

func (e *Engine) ListClasses(ctx context.Context, limit int, cursor string) ([]domain.ClassDefinition, string, error) {
	if err := e.authorizeGlobal(ctx, auth.OpRead); err != nil {
		return nil, "", err
	}
	return e.store.ListClasses(ctx, store.ListOptions{Limit: limit, Cursor: cursor})
}

func (e *Engine) CreateShape(ctx context.Context, in domain.CreateShapeInput) (*domain.ShapeProfile, error) {
	if err := e.authorizeGlobal(ctx, auth.OpCreate); err != nil {
		return nil, err
	}
	sh, err := e.store.CreateShape(ctx, in)
	if err != nil {
		return nil, mapErr(err)
	}
	return sh, nil
}

func (e *Engine) GetShape(ctx context.Context, code string) (*domain.ShapeProfile, error) {
	if err := e.authorizeGlobal(ctx, auth.OpRead); err != nil {
		return nil, err
	}
	sh, err := e.store.GetShapeByCode(ctx, code)
	if err != nil {
		return nil, mapErr(err)
	}
	return sh, nil
}

func (e *Engine) ListShapes(ctx context.Context, packageCode string) ([]domain.ShapeProfile, error) {
	if err := e.authorizeGlobal(ctx, auth.OpRead); err != nil {
		return nil, err
	}
	return e.store.ListShapes(ctx, packageCode)
}

func (e *Engine) GetSchemaConfig(ctx context.Context) (*domain.ModelSchemaConfig, error) {
	if err := e.authorizeGlobal(ctx, auth.OpRead); err != nil {
		return nil, err
	}
	return e.store.GetSchemaConfig(ctx)
}

func (e *Engine) UpdateSchemaConfig(ctx context.Context, cfg domain.ModelSchemaConfig) (*domain.ModelSchemaConfig, error) {
	if err := e.authorizeGlobal(ctx, auth.OpCreate); err != nil {
		return nil, err
	}
	return e.store.UpdateSchemaConfig(ctx, cfg)
}
