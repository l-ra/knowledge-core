package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
)

func (s *Store) GetSchemaConfig(ctx context.Context) (*domain.ModelSchemaConfig, error) {
	var instanceOf string
	var updatedAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT instance_of_property, updated_at FROM model_schema_config WHERE id = 1
	`).Scan(&instanceOf, &updatedAt)
	if err != nil {
		return nil, err
	}
	return &domain.ModelSchemaConfig{
		InstanceOfProperty: instanceOf,
		UpdatedAt:          updatedAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

func (s *Store) UpdateSchemaConfig(ctx context.Context, cfg domain.ModelSchemaConfig) (*domain.ModelSchemaConfig, error) {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		UPDATE model_schema_config SET instance_of_property = $1, updated_at = $2 WHERE id = 1
	`, cfg.InstanceOfProperty, now)
	if err != nil {
		return nil, err
	}
	return &domain.ModelSchemaConfig{
		InstanceOfProperty: cfg.InstanceOfProperty,
		UpdatedAt:          now.Format(time.RFC3339Nano),
	}, nil
}

func (s *Store) CreateValidationReport(ctx context.Context, actor string, scope, entityQID string, result domain.ValidationResult) (*domain.ValidationReport, error) {
	findingsJSON, err := json.Marshal(result.Findings)
	if err != nil {
		return nil, err
	}
	summaryJSON, err := json.Marshal(result.Summary)
	if err != nil {
		return nil, err
	}
	id := datatype.NewUUID()
	now := time.Now().UTC()
	var entityID *string
	if entityQID != "" {
		var eid string
		if err := s.pool.QueryRow(ctx, `SELECT id::text FROM entity WHERE public_id = $1`, entityQID).Scan(&eid); err == nil {
			entityID = &eid
		}
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO validation_report (id, scope, entity_id, entity_qid, findings, summary, created_by, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`, id, scope, entityID, entityQID, findingsJSON, summaryJSON, actor, now)
	if err != nil {
		return nil, err
	}
	return &domain.ValidationReport{
		ID: id.String(), Scope: scope, EntityQID: entityQID,
		Findings: result.Findings, Summary: result.Summary,
		CreatedBy: actor, CreatedAt: now.Format(time.RFC3339Nano),
	}, nil
}

func (s *Store) GetValidationReport(ctx context.Context, id string) (*domain.ValidationReport, error) {
	var rep domain.ValidationReport
	var findingsJSON, summaryJSON []byte
	var createdAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT id, scope, COALESCE(entity_qid,''), findings, summary, created_by, created_at
		FROM validation_report WHERE id = $1
	`, id).Scan(&rep.ID, &rep.Scope, &rep.EntityQID, &findingsJSON, &summaryJSON, &rep.CreatedBy, &createdAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(findingsJSON, &rep.Findings)
	_ = json.Unmarshal(summaryJSON, &rep.Summary)
	rep.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	return &rep, nil
}

func (s *Store) LoadPropertyConstraints(ctx context.Context) (map[string]domain.PropertyConstraints, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.public_id, pp.constraints
		FROM property_profile pp
		JOIN entity e ON e.id = pp.entity_id
		WHERE e.status <> 'deleted'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]domain.PropertyConstraints{}
	for rows.Next() {
		var pid string
		var raw []byte
		if err := rows.Scan(&pid, &raw); err != nil {
			return nil, err
		}
		var c domain.PropertyConstraints
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &c)
		}
		out[pid] = c
	}
	return out, rows.Err()
}
