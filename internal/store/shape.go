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
	var classPkg string
	err := s.pool.QueryRow(ctx, `
		SELECT e.id, COALESCE(p.code,'')
		FROM class_profile cp
		JOIN entity e ON e.id = cp.entity_id
		LEFT JOIN package p ON p.id = e.package_id
		WHERE e.public_id = $1 AND e.status <> 'deleted'
	`, in.ClassID).Scan(&classUUID, &classPkg)
	if err != nil {
		return nil, fmt.Errorf("class: %w", err)
	}
	pkgCode := in.PackageCode
	if pkgCode == "" {
		pkgCode = classPkg
	}
	var pkgID any
	if pkgCode != "" {
		id, err := s.resolvePackageIDRequired(ctx, s.pool, pkgCode)
		if err != nil {
			return nil, err
		}
		pkgID = id
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
		INSERT INTO shape_profile (id, code, class_id, document, package_id, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$6)
	`, id, in.Code, classUUID, docJSON, pkgID, now)
	if err != nil {
		return nil, err
	}
	return &domain.ShapeProfile{
		ID: id.String(), Code: in.Code, ClassID: in.ClassID, ClassPID: in.ClassID,
		PackageCode: pkgCode, Document: doc,
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}, nil
}

func scanShape(id, code, classPID, pkgCode string, docJSON []byte, createdAt, updatedAt time.Time) domain.ShapeProfile {
	shape := domain.ShapeProfile{
		ID: id, Code: code, ClassID: classPID, ClassPID: classPID, PackageCode: pkgCode,
		CreatedAt: createdAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt: updatedAt.UTC().Format(time.RFC3339Nano),
	}
	_ = json.Unmarshal(docJSON, &shape.Document)
	return shape
}

func (s *Store) GetShapeByCode(ctx context.Context, code string) (*domain.ShapeProfile, error) {
	var id, classPID, pkgCode string
	var docJSON []byte
	var createdAt, updatedAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT sp.id, sp.code, e.public_id, COALESCE(p.code,''), sp.document, sp.created_at, sp.updated_at
		FROM shape_profile sp
		JOIN entity e ON e.id = sp.class_id
		LEFT JOIN package p ON p.id = sp.package_id
		WHERE sp.code = $1
	`, code).Scan(&id, &code, &classPID, &pkgCode, &docJSON, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	sh := scanShape(id, code, classPID, pkgCode, docJSON, createdAt, updatedAt)
	return &sh, nil
}

func (s *Store) ListShapes(ctx context.Context, packageCode string) ([]domain.ShapeProfile, error) {
	q := `
		SELECT sp.id, sp.code, e.public_id, COALESCE(p.code,''), sp.document, sp.created_at, sp.updated_at
		FROM shape_profile sp
		JOIN entity e ON e.id = sp.class_id
		LEFT JOIN package p ON p.id = sp.package_id
	`
	args := []any{}
	if packageCode != "" {
		q += ` WHERE p.code = $1`
		args = append(args, packageCode)
	}
	q += ` ORDER BY sp.code`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ShapeProfile
	for rows.Next() {
		var id, code, classPID, pkgCode string
		var docJSON []byte
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &code, &classPID, &pkgCode, &docJSON, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		out = append(out, scanShape(id, code, classPID, pkgCode, docJSON, createdAt, updatedAt))
	}
	return out, rows.Err()
}

func (s *Store) LoadAllShapes(ctx context.Context) ([]domain.ShapeProfile, error) {
	return s.ListShapes(ctx, "")
}
