package store

import (
	"context"
	"strconv"
	"strings"

	"github.com/l-ra/knowledge-core/internal/datatype"
)

// ClassFacet is a count of entities with a direct instanceOf to classId.
type ClassFacet struct {
	ClassID string
	Count   int
}

// CountEntitiesByInstanceOf groups active entities in a package by direct instanceOf class.
func (s *Store) CountEntitiesByInstanceOf(ctx context.Context, packageCode string) ([]ClassFacet, error) {
	cfg, err := s.GetSchemaConfig(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.InstanceOfProperty) == "" {
		return nil, nil
	}
	args := []any{cfg.InstanceOfProperty}
	where := `e.status <> 'deleted'
		AND prop.public_id = $1
		AND sc.value_entity_id IS NOT NULL`
	if pkg := strings.TrimSpace(packageCode); pkg != "" {
		args = append(args, pkg)
		where += ` AND EXISTS (SELECT 1 FROM package p WHERE p.id = e.package_id AND p.code = $` + strconv.Itoa(len(args)) + `)`
	}
	q := `
SELECT cls.public_id, COUNT(DISTINCT e.id)::int
FROM entity e
JOIN statement_current sc ON sc.subject_id = e.id
JOIN entity prop ON prop.id = sc.property_id
JOIN entity cls ON cls.id = sc.value_entity_id
WHERE ` + where + `
GROUP BY cls.public_id
ORDER BY cls.public_id`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ClassFacet
	for rows.Next() {
		var f ClassFacet
		if err := rows.Scan(&f.ClassID, &f.Count); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ResolvePropertyPublicIDs maps P* ids or iriLocal names to property public ids.
func (s *Store) ResolvePropertyPublicIDs(ctx context.Context, specs []string) ([]string, error) {
	seen := map[string]struct{}{}
	var out []string
	for _, raw := range specs {
		spec := strings.TrimSpace(raw)
		if spec == "" {
			continue
		}
		if _, err := datatype.ParsePublicPropertyID(spec); err == nil {
			if _, ok := seen[spec]; ok {
				continue
			}
			seen[spec] = struct{}{}
			out = append(out, spec)
			continue
		}
		items, _, err := s.ListEntities(ctx, ListOptions{Kind: "property", IRILocal: spec, Limit: 10})
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if _, ok := seen[it.PublicID]; ok {
				continue
			}
			seen[it.PublicID] = struct{}{}
			out = append(out, it.PublicID)
		}
	}
	return out, nil
}
