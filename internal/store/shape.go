package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
)

func (s *Store) CreateShape(ctx context.Context, in domain.CreateShapeInput) (*domain.ShapeProfile, error) {
	if in.Code == "" {
		return nil, fmt.Errorf("shape code required")
	}
	if in.ClassID == "" {
		return nil, fmt.Errorf("class id required")
	}
	var classUUID string
	err := s.pool.QueryRow(ctx, `
		SELECT e.id FROM class_profile cp
		JOIN entity e ON e.id = cp.entity_id
		WHERE e.public_id = $1 AND e.status <> 'deleted'
	`, in.ClassID).Scan(&classUUID)
	if err != nil {
		return nil, fmt.Errorf("class: %w", err)
	}
	doc := in.Document
	if doc.Severity == "" {
		doc.Severity = domain.SeverityError
	}
	docJSON, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	id := datatype.NewUUID()
	now := time.Now().UTC()
	_, err = s.pool.Exec(ctx, `
		INSERT INTO shape_profile (id, code, class_id, document, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$5)
	`, id, in.Code, classUUID, docJSON, now)
	if err != nil {
		return nil, err
	}
	return &domain.ShapeProfile{
		ID: id.String(), Code: in.Code, ClassID: in.ClassID, ClassPID: in.ClassID, Document: doc,
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}, nil
}

func (s *Store) GetShapeByCode(ctx context.Context, code string) (*domain.ShapeProfile, error) {
	var shape domain.ShapeProfile
	var id string
	var classPID string
	var docJSON []byte
	var createdAt, updatedAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT sp.id, sp.code, e.public_id, sp.document, sp.created_at, sp.updated_at
		FROM shape_profile sp
		JOIN entity e ON e.id = sp.class_id
		WHERE sp.code = $1
	`, code).Scan(&id, &shape.Code, &classPID, &docJSON, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	shape.ID = id
	shape.ClassID = classPID
	shape.ClassPID = classPID
	_ = json.Unmarshal(docJSON, &shape.Document)
	shape.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	shape.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	return &shape, nil
}

func (s *Store) ListShapes(ctx context.Context) ([]domain.ShapeProfile, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT sp.id, sp.code, e.public_id, sp.document, sp.created_at, sp.updated_at
		FROM shape_profile sp
		JOIN entity e ON e.id = sp.class_id
		ORDER BY sp.code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ShapeProfile
	for rows.Next() {
		var shape domain.ShapeProfile
		var id, classPID string
		var docJSON []byte
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &shape.Code, &classPID, &docJSON, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		shape.ID = id
		shape.ClassID = classPID
		shape.ClassPID = classPID
		_ = json.Unmarshal(docJSON, &shape.Document)
		shape.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		shape.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		out = append(out, shape)
	}
	return out, rows.Err()
}

func (s *Store) LoadAllShapes(ctx context.Context) ([]domain.ShapeProfile, error) {
	return s.ListShapes(ctx)
}
