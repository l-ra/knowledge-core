package store

import (
	"context"
	"encoding/json"

	"github.com/l-ra/knowledge-core/internal/auth"
	"github.com/l-ra/knowledge-core/internal/datatype"
)

func (s *Store) ListAuthPolicies(ctx context.Context) ([]auth.Policy, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT name, priority, document FROM auth_policy ORDER BY priority DESC, name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []auth.Policy
	for rows.Next() {
		var p auth.Policy
		var docJSON []byte
		if err := rows.Scan(&p.Name, &p.Priority, &docJSON); err != nil {
			return nil, err
		}
		p.Document, err = auth.ParsePolicyDocument(docJSON)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) UpsertAuthPolicy(ctx context.Context, name string, priority int, doc auth.PolicyDocument) error {
	docJSON, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	id := datatype.NewUUID()
	_, err = s.pool.Exec(ctx, `
		INSERT INTO auth_policy (id, name, priority, document)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (name) DO UPDATE SET priority = EXCLUDED.priority, document = EXCLUDED.document, updated_at = now()
	`, id, name, priority, docJSON)
	return err
}

func (s *Store) DeleteAuthPolicy(ctx context.Context, name string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM auth_policy WHERE name = $1`, name)
	return err
}

func (s *Store) LoadEntityAuthAttributes(ctx context.Context, entityPublicID string) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.public_id, sc.value_text
		FROM statement_current sc
		JOIN statement st ON st.id = sc.statement_id
		JOIN entity e ON e.id = st.subject_id
		JOIN property_profile pp ON pp.entity_id = st.property_id
		JOIN entity p ON p.id = pp.entity_id
		WHERE e.public_id = $1 AND sc.value_type = 'String' AND sc.value_text IS NOT NULL
	`, entityPublicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	attrs := map[string]string{}
	for rows.Next() {
		var pid, val string
		if err := rows.Scan(&pid, &val); err != nil {
			return nil, err
		}
		attrs[pid] = val
	}
	return attrs, rows.Err()
}
