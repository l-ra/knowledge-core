package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
)

func (s *Store) CreateClass(ctx context.Context, meta domain.WriteMeta, in domain.CreateClassInput) (*domain.WriteResult[domain.ClassDefinition], error) {
	labels, err := datatype.NormalizeLabels(in.Labels)
	if err != nil {
		return nil, err
	}
	if err := datatype.RequireLabelEN(labels); err != nil {
		return nil, err
	}
	descs := in.Descriptions
	if descs == nil {
		descs = map[string]string{}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if hit, err := s.checkIdempotency(ctx, tx, meta); err != nil {
		return nil, err
	} else if hit != nil {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		var c domain.ClassDefinition
		if err := json.Unmarshal(hit.responseBody, &c); err != nil {
			return nil, err
		}
		return &domain.WriteResult[domain.ClassDefinition]{Value: c, Replay: true, ResponseRaw: hit.responseBody}, nil
	}

	id := datatype.NewUUID()
	publicID, err := s.nextPublicID(ctx, tx, "class", "C")
	if err != nil {
		return nil, err
	}
	pkgID, err := s.resolvePackageID(ctx, tx, in.PackageCode)
	if err != nil {
		return nil, err
	}
	var canonicalID *uuid.UUID
	if in.CanonicalEntityID != "" {
		var entID uuid.UUID
		var qid string
		err = tx.QueryRow(ctx, `SELECT id, public_id FROM entity WHERE public_id = $1 OR id::text = $1`, in.CanonicalEntityID).
			Scan(&entID, &qid)
		if err != nil {
			return nil, fmt.Errorf("canonical entity: %w", err)
		}
		canonicalID = &entID
	}
	doc := domain.ClassDocument{SubClassOf: in.SubClassOf}
	docJSON, _ := json.Marshal(doc)
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		INSERT INTO class_definition (id, public_id, status, package_id, canonical_entity_id, document, created_at, updated_at)
		VALUES ($1,$2,'active',$3,$4,$5,$6,$6)
	`, id, publicID, pkgID, canonicalID, docJSON, now)
	if err != nil {
		return nil, err
	}
	for lang, text := range labels {
		if _, err := tx.Exec(ctx, `INSERT INTO class_label (class_id, lang, text) VALUES ($1,$2,$3)`, id, lang, text); err != nil {
			return nil, err
		}
	}
	for lang, text := range descs {
		if _, err := tx.Exec(ctx, `INSERT INTO class_description (class_id, lang, text) VALUES ($1,$2,$3)`, id, lang, text); err != nil {
			return nil, err
		}
	}

	cs, err := s.beginChangeSetTx(ctx, tx, meta)
	if err != nil {
		return nil, err
	}
	if err := cs.addItem(ctx, tx, "class", id, publicID, "create", nil); err != nil {
		return nil, err
	}

	c := domain.ClassDefinition{
		ID: id.String(), PublicID: publicID, Status: domain.PropertyActive,
		PackageCode: in.PackageCode, Document: doc, Labels: labels, Descriptions: descs,
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}
	if in.CanonicalEntityID != "" {
		c.CanonicalEntityID = in.CanonicalEntityID
	}
	if err := s.finalizeChangeSet(ctx, tx, cs, c); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.ClassDefinition]{Value: c, ChangeSet: s.changeSetDomain(cs, meta)}, nil
}

func (s *Store) GetClassByPublicID(ctx context.Context, cid string) (*domain.ClassDefinition, error) {
	var c domain.ClassDefinition
	var id uuid.UUID
	var pkgCode *string
	var canonicalID *uuid.UUID
	var canonicalQID *string
	var docJSON []byte
	var createdAt, updatedAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT c.id, c.public_id, c.status, pkg.code, c.canonical_entity_id, ce.public_id,
			c.document, c.created_at, c.updated_at
		FROM class_definition c
		LEFT JOIN package pkg ON pkg.id = c.package_id
		LEFT JOIN entity ce ON ce.id = c.canonical_entity_id
		WHERE c.public_id = $1 AND c.status <> 'deleted'
	`, cid).Scan(&id, &c.PublicID, &c.Status, &pkgCode, &canonicalID, &canonicalQID, &docJSON, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	c.ID = id.String()
	if pkgCode != nil {
		c.PackageCode = *pkgCode
	}
	if canonicalQID != nil {
		c.CanonicalEntityQID = *canonicalQID
		c.CanonicalEntityID = canonicalID.String()
	}
	_ = json.Unmarshal(docJSON, &c.Document)
	c.Labels, _ = s.loadLabels(ctx, `SELECT lang, text FROM class_label WHERE class_id = $1`, id)
	c.Descriptions, _ = s.loadLabels(ctx, `SELECT lang, text FROM class_description WHERE class_id = $1`, id)
	c.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
	c.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
	return &c, nil
}

func (s *Store) ListClasses(ctx context.Context, opt ListOptions) ([]domain.ClassDefinition, string, error) {
	if opt.Limit <= 0 {
		opt.Limit = 50
	}
	if opt.Limit > 200 {
		opt.Limit = 200
	}
	args := []any{}
	where := `c.status <> 'deleted'`
	if opt.Cursor != "" {
		args = append(args, opt.Cursor)
		where += fmt.Sprintf(` AND c.public_id > $%d`, len(args))
	}
	args = append(args, opt.Limit+1)
	limitArg := fmt.Sprintf(`$%d`, len(args))

	rows, err := s.pool.Query(ctx, `
		SELECT c.id, c.public_id, c.status, pkg.code, ce.public_id, c.document, c.created_at, c.updated_at
		FROM class_definition c
		LEFT JOIN package pkg ON pkg.id = c.package_id
		LEFT JOIN entity ce ON ce.id = c.canonical_entity_id
		WHERE `+where+`
		ORDER BY c.public_id
		LIMIT `+limitArg, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var out []domain.ClassDefinition
	for rows.Next() {
		var c domain.ClassDefinition
		var id uuid.UUID
		var pkgCode, canonicalQID *string
		var docJSON []byte
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &c.PublicID, &c.Status, &pkgCode, &canonicalQID, &docJSON, &createdAt, &updatedAt); err != nil {
			return nil, "", err
		}
		c.ID = id.String()
		if pkgCode != nil {
			c.PackageCode = *pkgCode
		}
		if canonicalQID != nil {
			c.CanonicalEntityQID = *canonicalQID
		}
		_ = json.Unmarshal(docJSON, &c.Document)
		c.Labels, _ = s.loadLabels(ctx, `SELECT lang, text FROM class_label WHERE class_id = $1`, id)
		c.Descriptions, _ = s.loadLabels(ctx, `SELECT lang, text FROM class_description WHERE class_id = $1`, id)
		c.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		c.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > opt.Limit {
		next = out[opt.Limit-1].PublicID
		out = out[:opt.Limit]
	}
	return out, next, nil
}

func (s *Store) LoadAllClasses(ctx context.Context) ([]domain.ClassDefinition, error) {
	items, _, err := s.ListClasses(ctx, ListOptions{Limit: 1000})
	return items, err
}
