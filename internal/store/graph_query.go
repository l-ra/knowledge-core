package store

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
)

func (s *Store) ResolveEntityPublicID(ctx context.Context, idOrPublic string) (string, error) {
	var publicID string
	err := s.pool.QueryRow(ctx, `SELECT public_id FROM entity WHERE id::text = $1 OR public_id = $1`, idOrPublic).Scan(&publicID)
	return publicID, err
}

func (s *Store) classDescendants(ctx context.Context, rootPublicID string) ([]string, error) {
	classes, err := s.LoadAllClasses(ctx)
	if err != nil {
		return nil, err
	}
	children := map[string][]string{}
	for _, c := range classes {
		if c.Document.SubClassOf != "" {
			children[c.Document.SubClassOf] = append(children[c.Document.SubClassOf], c.PublicID)
		}
	}
	seen := map[string]bool{}
	var walk func(string)
	walk = func(id string) {
		for _, ch := range children[id] {
			if seen[ch] {
				continue
			}
			seen[ch] = true
			walk(ch)
		}
	}
	walk(rootPublicID)
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	return out, nil
}

func (s *Store) ClassAncestry(ctx context.Context, classPublicID string) ([]string, error) {
	classes, err := s.LoadAllClasses(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string]domain.ClassDefinition{}
	for _, c := range classes {
		byID[c.PublicID] = c
	}
	return expandClassIDs([]string{classPublicID}, byID), nil
}

func expandClassIDs(direct []string, classes map[string]domain.ClassDefinition) []string {
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
	return out
}

func (s *Store) EntityEffectiveClasses(ctx context.Context, qid string) ([]string, error) {
	cfg, err := s.GetSchemaConfig(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.InstanceOfProperty) == "" {
		return nil, nil
	}
	classes, err := s.LoadAllClasses(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string]domain.ClassDefinition{}
	for _, c := range classes {
		byID[c.PublicID] = c
	}
	stmts, err := s.ListStatementsBySubject(ctx, qid, cfg.InstanceOfProperty)
	if err != nil {
		return nil, err
	}
	var direct []string
	for _, st := range stmts {
		if st.Status != domain.StatementActive {
			continue
		}
		if st.Value.Type != datatype.EntityReference || st.Value.EntityID == nil {
			continue
		}
		pub, err := s.ResolveEntityPublicID(ctx, *st.Value.EntityID)
		if err != nil {
			continue
		}
		direct = append(direct, pub)
	}
	return expandClassIDs(direct, byID), nil
}

func (s *Store) ListStatementsByObject(ctx context.Context, qid string, propertyPID string) ([]domain.Statement, error) {
	out, _, err := s.ListStatements(ctx, StatementListOptions{Limit: 200, ObjectQID: qid, PropertyPID: propertyPID})
	return out, err
}

func (s *Store) findDuplicateStatementTx(ctx context.Context, tx pgx.Tx, subjectID, propertyID uuid.UUID, sv storedValue) (string, error) {
	var publicID string
	err := tx.QueryRow(ctx, `
		SELECT st.public_id
		FROM statement_current sc
		JOIN statement st ON st.id = sc.statement_id
		WHERE sc.subject_id = $1 AND sc.property_id = $2 AND st.status = 'active'
		  AND sc.value_type = $3
		  AND sc.value_entity_id IS NOT DISTINCT FROM $4
		  AND sc.value_text IS NOT DISTINCT FROM $5
		  AND sc.value_bool IS NOT DISTINCT FROM $6
		  AND sc.value_int64 IS NOT DISTINCT FROM $7
		  AND sc.value_numeric IS NOT DISTINCT FROM $8
		  AND sc.value_date IS NOT DISTINCT FROM $9
		  AND sc.value_timestamptz IS NOT DISTINCT FROM $10
		  AND sc.value_json IS NOT DISTINCT FROM $11
		ORDER BY st.public_id
		LIMIT 1
	`, subjectID, propertyID, sv.Type, sv.EntityID, sv.Text, sv.Bool, sv.Int64, sv.Numeric, sv.Date, sv.Timestamptz, sv.JSON).Scan(&publicID)
	if err != nil {
		return "", err
	}
	return publicID, nil
}
