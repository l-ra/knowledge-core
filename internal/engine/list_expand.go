package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/l-ra/knowledge-core/internal/auth"
	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
	"github.com/l-ra/knowledge-core/internal/store"
)

const maxBatchReadIDs = 200

// ListExpandOptions controls optional embeds on list/batch-read.
type ListExpandOptions struct {
	EffectiveClasses bool
	Statements       bool
	Properties       []string // required when Statements
}

// BatchReadResult is one id outcome for batch-read.
type BatchReadResult struct {
	ID               string
	Entity           *domain.Entity
	Statements       []domain.Statement
	Error            string // "not_found" when missing / forbidden
}

func parseIncludeCSV(raw string) (eff, stmts bool) {
	for _, part := range strings.Split(raw, ",") {
		switch strings.TrimSpace(strings.ToLower(part)) {
		case "effectiveclasses":
			eff = true
		case "statements":
			stmts = true
		}
	}
	return eff, stmts
}

func ParseListExpand(includeCSV string, propertiesCSV string) (ListExpandOptions, error) {
	eff, stmts := parseIncludeCSV(includeCSV)
	var props []string
	for _, p := range strings.Split(propertiesCSV, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			props = append(props, p)
		}
	}
	if stmts && len(props) == 0 {
		return ListExpandOptions{}, fmt.Errorf("%w: properties required when include=statements", ErrInvalid)
	}
	return ListExpandOptions{
		EffectiveClasses: eff,
		Statements:       stmts,
		Properties:       props,
	}, nil
}

func (e *Engine) AttachEffectiveClasses(ctx context.Context, items []domain.Entity) error {
	for i := range items {
		classes, err := e.store.EntityEffectiveClasses(ctx, items[i].PublicID)
		if err != nil {
			return mapErr(err)
		}
		items[i].EffectiveClasses = classes
	}
	return nil
}

// StatementsBySubjects returns active statements for subjects, filtered to property public ids.
func (e *Engine) StatementsBySubjects(ctx context.Context, qids []string, propertySpecs []string) (map[string][]domain.Statement, error) {
	propIDs, err := e.store.ResolvePropertyPublicIDs(ctx, propertySpecs)
	if err != nil {
		return nil, mapErr(err)
	}
	if len(propIDs) == 0 {
		return map[string][]domain.Statement{}, nil
	}
	allow := map[string]struct{}{}
	for _, p := range propIDs {
		allow[p] = struct{}{}
	}
	out := make(map[string][]domain.Statement, len(qids))
	for _, qid := range qids {
		list, err := e.store.ListStatementsBySubject(ctx, qid, "")
		if err != nil {
			return nil, mapErr(err)
		}
		filtered := make([]domain.Statement, 0)
		for i := range list {
			if list[i].Status != domain.StatementActive {
				continue
			}
			if _, ok := allow[list[i].PropertyPID]; !ok {
				continue
			}
			st, err := e.presentStatement(ctx, &list[i])
			if err != nil {
				return nil, err
			}
			filtered = append(filtered, *st)
		}
		out[qid] = filtered
	}
	return out, nil
}

func (e *Engine) CountEntityFacetsByInstanceOf(ctx context.Context, packageCode string) ([]store.ClassFacet, error) {
	if _, ok := auth.SubjectFromContext(ctx); !ok {
		return nil, ErrForbidden
	}
	facets, err := e.store.CountEntitiesByInstanceOf(ctx, packageCode)
	if err != nil {
		return nil, mapErr(err)
	}
	return facets, nil
}

type BatchReadInput struct {
	IDs        []string
	Include    []string
	Properties []string
}

func (e *Engine) BatchReadEntities(ctx context.Context, in BatchReadInput) ([]BatchReadResult, error) {
	if _, ok := auth.SubjectFromContext(ctx); !ok {
		return nil, ErrForbidden
	}
	if len(in.IDs) == 0 {
		return nil, fmt.Errorf("%w: ids required", ErrInvalid)
	}
	if len(in.IDs) > maxBatchReadIDs {
		return nil, fmt.Errorf("%w: batch limit exceeded (max %d ids)", ErrInvalid, maxBatchReadIDs)
	}
	includeCSV := strings.Join(in.Include, ",")
	propsCSV := strings.Join(in.Properties, ",")
	expand, err := ParseListExpand(includeCSV, propsCSV)
	if err != nil {
		return nil, err
	}

	results := make([]BatchReadResult, 0, len(in.IDs))
	qidsForStmts := make([]string, 0, len(in.IDs))

	for _, id := range in.IDs {
		id = strings.TrimSpace(id)
		res := BatchReadResult{ID: id}
		if _, _, err := datatype.ParsePublicGraphID(id); err != nil {
			res.Error = "not_found"
			results = append(results, res)
			continue
		}
		ent, err := e.GetEntity(ctx, id)
		if err != nil {
			res.Error = "not_found"
			results = append(results, res)
			continue
		}
		if !expand.EffectiveClasses {
			ent.EffectiveClasses = nil
		}
		res.Entity = ent
		if expand.Statements {
			qidsForStmts = append(qidsForStmts, id)
		}
		results = append(results, res)
	}

	if expand.Statements && len(qidsForStmts) > 0 {
		stmtMap, err := e.StatementsBySubjects(ctx, qidsForStmts, expand.Properties)
		if err != nil {
			return nil, err
		}
		for i := range results {
			if results[i].Entity == nil {
				continue
			}
			results[i].Statements = stmtMap[results[i].ID]
		}
	}

	return results, nil
}
