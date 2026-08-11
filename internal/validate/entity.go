package validate

import (
	"context"
	"fmt"
	"slices"

	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
	"github.com/l-ra/knowledge-core/internal/store"
)

type Options struct {
	PendingStatement *domain.CreateStatementInput
}

type snapshot struct {
	instanceOf   string
	classes      map[string]domain.ClassDefinition
	classByCanon map[string]string
	constraints  map[string]domain.PropertyConstraints
	shapes       []domain.ShapeProfile
}

func Entity(ctx context.Context, st *store.Store, qid string, opt Options) (*domain.ValidationResult, error) {
	snap, err := loadSnapshot(ctx, st)
	if err != nil {
		return nil, err
	}
	stmts, err := st.ListStatementsBySubject(ctx, qid)
	if err != nil {
		return nil, err
	}
	if opt.PendingStatement != nil {
		stmts = append(stmts, pendingStatement(*opt.PendingStatement))
	}
	entityClasses := resolveEntityClasses(snap, stmts)
	findings := validateStatements(qid, stmts, entityClasses, snap)
	findings = append(findings, validateShapes(qid, stmts, entityClasses, snap)...)
	return &domain.ValidationResult{
		EntityID: qid,
		Findings: findings,
		Summary:  summarize(findings),
	}, nil
}

func loadSnapshot(ctx context.Context, st *store.Store) (*snapshot, error) {
	cfg, err := st.GetSchemaConfig(ctx)
	if err != nil {
		return nil, err
	}
	classes, err := st.LoadAllClasses(ctx)
	if err != nil {
		return nil, err
	}
	constraints, err := st.LoadPropertyConstraints(ctx)
	if err != nil {
		return nil, err
	}
	shapes, err := st.LoadAllShapes(ctx)
	if err != nil {
		return nil, err
	}
	s := &snapshot{
		instanceOf:   cfg.InstanceOfProperty,
		constraints:  constraints,
		shapes:       shapes,
		classes:      map[string]domain.ClassDefinition{},
		classByCanon: map[string]string{},
	}
	for _, c := range classes {
		s.classes[c.PublicID] = c
		if c.CanonicalEntityQID != "" {
			s.classByCanon[c.CanonicalEntityQID] = c.PublicID
		}
	}
	return s, nil
}

func pendingStatement(in domain.CreateStatementInput) domain.Statement {
	return domain.Statement{
		SubjectQID:  in.SubjectPublicID,
		PropertyPID: in.PropertyPublicID,
		Value:       in.Value,
		Status:      domain.StatementActive,
	}
}

func resolveEntityClasses(s *snapshot, stmts []domain.Statement) []string {
	if s.instanceOf == "" {
		return nil
	}
	var direct []string
	for _, st := range stmts {
		if st.PropertyPID != s.instanceOf || st.Status != domain.StatementActive {
			continue
		}
		if st.Value.Type != datatype.EntityReference || st.Value.EntityID == nil {
			continue
		}
		target := *st.Value.EntityID
		if cid, ok := s.classByCanon[target]; ok {
			direct = append(direct, cid)
		}
	}
	return expandClasses(direct, s.classes)
}

func expandClasses(direct []string, classes map[string]domain.ClassDefinition) []string {
	seen := map[string]bool{}
	var walk func(string)
	walk = func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		if c, ok := classes[id]; ok {
			walk(c.Document.SubClassOf)
		}
	}
	for _, id := range direct {
		walk(id)
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

func hasClassIntersection(entityClasses, required []string) bool {
	if len(required) == 0 {
		return true
	}
	for _, r := range required {
		if slices.Contains(entityClasses, r) {
			return true
		}
	}
	return false
}

func severityOrDefault(c domain.PropertyConstraints, fallback domain.ValidationSeverity) domain.ValidationSeverity {
	if c.Severity != "" {
		return c.Severity
	}
	return fallback
}

func validateStatements(qid string, stmts []domain.Statement, entityClasses []string, s *snapshot) []domain.ValidationFinding {
	var findings []domain.ValidationFinding
	byProp := map[string][]domain.Statement{}
	for _, st := range stmts {
		if st.Status != domain.StatementActive {
			continue
		}
		byProp[st.PropertyPID] = append(byProp[st.PropertyPID], st)
	}
	for pid, group := range byProp {
		c, ok := s.constraints[pid]
		if !ok {
			continue
		}
		sev := severityOrDefault(c, domain.SeverityError)
		if len(c.DomainClasses) > 0 && !hasClassIntersection(entityClasses, c.DomainClasses) {
			findings = append(findings, domain.ValidationFinding{
				Code: "domain_mismatch", Severity: sev,
				Message: fmt.Sprintf("property %s requires domain %v, entity has %v", pid, c.DomainClasses, entityClasses),
				EntityID: qid, PropertyID: pid,
			})
		}
		if c.MinCount != nil && len(group) < *c.MinCount {
			findings = append(findings, domain.ValidationFinding{
				Code: "cardinality_min", Severity: sev,
				Message: fmt.Sprintf("property %s has %d values, minimum is %d", pid, len(group), *c.MinCount),
				EntityID: qid, PropertyID: pid,
			})
		}
		if c.MaxCount != nil && len(group) > *c.MaxCount {
			findings = append(findings, domain.ValidationFinding{
				Code: "cardinality_max", Severity: sev,
				Message: fmt.Sprintf("property %s has %d values, maximum is %d", pid, len(group), *c.MaxCount),
				EntityID: qid, PropertyID: pid,
			})
		}
		if len(c.RangeClasses) > 0 {
			for _, st := range group {
				if st.Value.Type != datatype.EntityReference || st.Value.EntityID == nil {
					continue
				}
				targetClasses := resolveTargetClasses(*st.Value.EntityID, s)
				if !hasClassIntersection(targetClasses, c.RangeClasses) {
					findings = append(findings, domain.ValidationFinding{
						Code: "range_mismatch", Severity: sev,
						Message: fmt.Sprintf("property %s value %s requires range %v", pid, *st.Value.EntityID, c.RangeClasses),
						EntityID: qid, PropertyID: pid, StatementID: st.PublicID,
					})
				}
			}
		}
	}
	return findings
}

func resolveTargetClasses(targetQID string, s *snapshot) []string {
	if cid, ok := s.classByCanon[targetQID]; ok {
		return expandClasses([]string{cid}, s.classes)
	}
	return nil
}

func validateShapes(qid string, stmts []domain.Statement, entityClasses []string, s *snapshot) []domain.ValidationFinding {
	var findings []domain.ValidationFinding
	activeProps := map[string]bool{}
	for _, st := range stmts {
		if st.Status == domain.StatementActive {
			activeProps[st.PropertyPID] = true
		}
	}
	for _, shape := range s.shapes {
		if !slices.Contains(entityClasses, shape.ClassPID) {
			continue
		}
		sev := shape.Document.Severity
		if sev == "" {
			sev = domain.SeverityError
		}
		for _, req := range shape.Document.RequiredProperties {
			if !activeProps[req] {
				findings = append(findings, domain.ValidationFinding{
					Code: "shape_required_missing", Severity: sev,
					Message: fmt.Sprintf("shape %s requires property %s", shape.Code, req),
					EntityID: qid, ClassID: shape.ClassPID, ShapeCode: shape.Code, PropertyID: req,
				})
			}
		}
		allowed := shape.Document.AllowedProperties
		if shape.Document.Closed || len(allowed) > 0 {
			allowedSet := map[string]bool{}
			for _, p := range allowed {
				allowedSet[p] = true
			}
			if s.instanceOf != "" {
				allowedSet[s.instanceOf] = true
			}
			for pid := range activeProps {
				if shape.Document.Closed || len(allowed) > 0 {
					if !allowedSet[pid] {
						findings = append(findings, domain.ValidationFinding{
							Code: "shape_extra_property", Severity: domain.SeverityWarning,
							Message: fmt.Sprintf("property %s is outside shape %s model", pid, shape.Code),
							EntityID: qid, ClassID: shape.ClassPID, ShapeCode: shape.Code, PropertyID: pid,
						})
					}
				}
			}
		}
	}
	return findings
}

func summarize(findings []domain.ValidationFinding) domain.ValidationSummary {
	var sum domain.ValidationSummary
	for _, f := range findings {
		switch f.Severity {
		case domain.SeverityError:
			sum.Errors++
		case domain.SeverityWarning:
			sum.Warnings++
		default:
			sum.Infos++
		}
	}
	return sum
}

func HasErrors(res *domain.ValidationResult) bool {
	return res != nil && res.Summary.Errors > 0
}
