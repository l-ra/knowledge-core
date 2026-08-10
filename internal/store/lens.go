package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rasekl/knowledge-core/internal/datatype"
	"github.com/rasekl/knowledge-core/internal/domain"
)

func (s *Store) CreateLens(ctx context.Context, in domain.CreateLensInput) (*domain.LensDefinition, error) {
	if in.Code == "" {
		return nil, fmt.Errorf("lens code required")
	}
	labels := in.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	if err := datatype.RequireLabelEN(labels); err != nil {
		return nil, err
	}
	if err := validateLensDocument(&in.Document); err != nil {
		return nil, err
	}

	id := datatype.NewUUID()
	now := time.Now().UTC()
	labelsJSON, _ := json.Marshal(labels)
	docJSON, err := json.Marshal(in.Document)
	if err != nil {
		return nil, err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO lens_definition (id, code, version, labels, document, created_at, updated_at)
		VALUES ($1,$2,1,$3,$4,$5,$5)
	`, id, in.Code, labelsJSON, docJSON, now)
	if err != nil {
		return nil, err
	}
	return &domain.LensDefinition{
		ID: id.String(), Code: in.Code, Version: 1, Labels: labels,
		Document: in.Document,
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}, nil
}

func (s *Store) GetLensByCode(ctx context.Context, code string) (*domain.LensDefinition, error) {
	var lens domain.LensDefinition
	var id string
	var labelsJSON, docJSON []byte
	var createdAt, updatedAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id, code, version, labels, document, created_at, updated_at
		FROM lens_definition WHERE code = $1
	`, code).Scan(&id, &lens.Code, &lens.Version, &labelsJSON, &docJSON, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	lens.ID = id
	_ = json.Unmarshal(labelsJSON, &lens.Labels)
	if err := json.Unmarshal(docJSON, &lens.Document); err != nil {
		return nil, err
	}
	lens.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	lens.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	return &lens, nil
}

func validateLensDocument(doc *domain.LensDocument) error {
	if doc.Key.Property == "" {
		return fmt.Errorf("lens key.property required")
	}
	if len(doc.Fields) == 0 {
		return fmt.Errorf("lens requires at least one field")
	}
	for name, f := range doc.Fields {
		if f.Property == "" {
			return fmt.Errorf("field %q missing property", name)
		}
		if f.Cardinality == "" {
			f.Cardinality = domain.CardinalityOne
			doc.Fields[name] = f
		}
	}
	return nil
}

func (s *Store) ResolveEntityByLensKey(ctx context.Context, doc domain.LensDocument, keyValue string) (string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT e.public_id
		FROM entity e
		JOIN statement st ON st.subject_id = e.id AND st.status = 'active'
		JOIN property_definition pk ON pk.id = st.property_id
		JOIN statement_current sc ON sc.statement_id = st.id
		WHERE pk.public_id = $1 AND sc.value_type = 'String' AND sc.value_text = $2
	`, doc.Key.Property, keyValue)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var matches []string
	for rows.Next() {
		var qid string
		if err := rows.Scan(&qid); err != nil {
			return "", err
		}
		matches = append(matches, qid)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("entity not found for key")
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous lens key")
	}
	return matches[0], nil
}

func (s *Store) FindActiveStatementBySubjectProperty(ctx context.Context, qid, pid string) (*domain.Statement, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT st.id, st.public_id, st.subject_id, e.public_id, st.property_id, p.public_id, st.status,
			st.value_type, st.value_bool, st.value_int64, st.value_numeric, st.value_date, st.value_timestamptz,
			st.value_text, st.value_entity_id, st.value_json, st.valid_from, st.valid_to,
			st.current_revision_no, st.created_at, st.updated_at
		FROM statement st
		JOIN entity e ON e.id = st.subject_id
		JOIN property_definition p ON p.id = st.property_id
		WHERE e.public_id = $1 AND p.public_id = $2 AND st.status = 'active'
		ORDER BY st.public_id
		LIMIT 1
	`, qid, pid)
	st, err := scanStatement(row)
	if err != nil {
		return nil, err
	}
	if err := s.enrichStatement(ctx, s.pool, st); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Store) ListActiveStatementsBySubjectProperty(ctx context.Context, qid, pid string) ([]domain.Statement, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT st.id, st.public_id, st.subject_id, e.public_id, st.property_id, p.public_id, st.status,
			st.value_type, st.value_bool, st.value_int64, st.value_numeric, st.value_date, st.value_timestamptz,
			st.value_text, st.value_entity_id, st.value_json, st.valid_from, st.valid_to,
			st.current_revision_no, st.created_at, st.updated_at
		FROM statement st
		JOIN entity e ON e.id = st.subject_id
		JOIN property_definition p ON p.id = st.property_id
		WHERE e.public_id = $1 AND p.public_id = $2 AND st.status = 'active'
		ORDER BY st.public_id
	`, qid, pid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Statement
	for rows.Next() {
		st, err := scanStatement(rows)
		if err != nil {
			return nil, err
		}
		if err := s.enrichStatement(ctx, s.pool, st); err != nil {
			return nil, err
		}
		out = append(out, *st)
	}
	return out, rows.Err()
}
