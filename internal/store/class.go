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
	localID := generatedIRILocal("class")
	pkgID, err := s.resolvePackageIDRequired(ctx, tx, in.PackageCode)
	if err != nil {
		return nil, err
	}
	if in.SubClassOf != "" {
		if _, err := datatype.ParsePublicClassID(in.SubClassOf); err != nil {
			return nil, fmt.Errorf("subClassOf: %w", err)
		}
		var exists bool
		err = tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM class_profile cp
				JOIN entity e ON e.id = cp.entity_id
				WHERE e.public_id = $1 AND e.status <> 'deleted'
			)
		`, in.SubClassOf).Scan(&exists)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, fmt.Errorf("subClassOf class not found")
		}
	}
	doc := domain.ClassDocument{SubClassOf: in.SubClassOf}
	docJSON, _ := json.Marshal(doc)
	iriLocal, err := normalizeOptionalIRILocal(in.IRILocal)
	if err != nil {
		return nil, err
	}
	pkgCode, iriBase, err := s.packageIRIBaseByID(ctx, tx, pkgID)
	if err != nil {
		return nil, err
	}
	publicID := resolvePublicIRI(iriBase, iriLocal, localID, pkgCode)
	if iriLocal == "" {
		iriLocal = localID
	}
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		INSERT INTO entity (id, public_id, status, current_revision_no, package_id, iri_local, created_at, updated_at)
		VALUES ($1,$2,'active',1,$3,$4,$5,$5)
	`, id, publicID, pkgID, iriLocal, now)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO class_profile (entity_id, document) VALUES ($1,$2)`, id, docJSON)
	if err != nil {
		return nil, err
	}
	for lang, text := range labels {
		if _, err := tx.Exec(ctx, `INSERT INTO entity_label (entity_id, lang, text) VALUES ($1,$2,$3)`, id, lang, text); err != nil {
			return nil, err
		}
	}
	for lang, text := range descs {
		if _, err := tx.Exec(ctx, `INSERT INTO entity_description (entity_id, lang, text) VALUES ($1,$2,$3)`, id, lang, text); err != nil {
			return nil, err
		}
	}

	cs, err := s.beginChangeSetTx(ctx, tx, meta)
	if err != nil {
		return nil, err
	}
	labelsJSON, _ := labelsToJSON(labels)
	descJSON, _ := labelsToJSON(descs)
	_, err = tx.Exec(ctx, `
		INSERT INTO entity_revision (id, entity_id, revision_no, status, labels, descriptions, change_set_id, actor, created_at)
		VALUES ($1,$2,1,'active',$3,$4,$5,$6,$7)
	`, datatype.NewUUID(), id, labelsJSON, descJSON, cs.id, meta.Actor, now)
	if err != nil {
		return nil, err
	}
	if err := cs.addItem(ctx, tx, "class", id, publicID, "create", docJSON); err != nil {
		return nil, err
	}

	c := domain.ClassDefinition{
		ID: id.String(), PublicID: publicID, Status: domain.PropertyActive,
		PackageCode: in.PackageCode, IRILocal: iriLocal, Document: doc, Labels: labels, Descriptions: descs,
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
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
	var iriBase string
	var docJSON []byte
	var createdAt, updatedAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT e.id, e.public_id, e.status, pkg.code, COALESCE(e.iri_local,''), COALESCE(pkg.iri_base,''), cp.document, e.created_at, e.updated_at
		FROM class_profile cp
		JOIN entity e ON e.id = cp.entity_id
		LEFT JOIN package pkg ON pkg.id = e.package_id
		WHERE e.public_id = $1 AND e.status <> 'deleted'
	`, cid).Scan(&id, &c.PublicID, &c.Status, &pkgCode, &c.IRILocal, &iriBase, &docJSON, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	c.ID = id.String()
	if pkgCode != nil {
		c.PackageCode = *pkgCode
	}
	c.IRI = datatype.ResolveIRI(iriBase, c.IRILocal, c.PublicID, fallbackNSForPublicID(c.PublicID))
	_ = json.Unmarshal(docJSON, &c.Document)
	c.Labels, _ = s.loadLabels(ctx, `SELECT lang, text FROM entity_label WHERE entity_id = $1`, id)
	c.Descriptions, _ = s.loadLabels(ctx, `SELECT lang, text FROM entity_description WHERE entity_id = $1`, id)
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
	where := `e.status <> 'deleted'`
	if opt.Cursor != "" {
		args = append(args, opt.Cursor)
		where += fmt.Sprintf(` AND e.public_id > $%d`, len(args))
	}
	args = append(args, opt.Limit+1)
	limitArg := fmt.Sprintf(`$%d`, len(args))

	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.public_id, e.status, pkg.code, COALESCE(e.iri_local,''), COALESCE(pkg.iri_base,''), cp.document, e.created_at, e.updated_at
		FROM class_profile cp
		JOIN entity e ON e.id = cp.entity_id
		LEFT JOIN package pkg ON pkg.id = e.package_id
		WHERE `+where+`
		ORDER BY e.public_id
		LIMIT `+limitArg, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var out []domain.ClassDefinition
	for rows.Next() {
		var c domain.ClassDefinition
		var id uuid.UUID
		var pkgCode *string
		var iriBase string
		var docJSON []byte
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &c.PublicID, &c.Status, &pkgCode, &c.IRILocal, &iriBase, &docJSON, &createdAt, &updatedAt); err != nil {
			return nil, "", err
		}
		c.ID = id.String()
		if pkgCode != nil {
			c.PackageCode = *pkgCode
		}
		c.IRI = datatype.ResolveIRI(iriBase, c.IRILocal, c.PublicID, fallbackNSForPublicID(c.PublicID))
		_ = json.Unmarshal(docJSON, &c.Document)
		c.Labels, _ = s.loadLabels(ctx, `SELECT lang, text FROM entity_label WHERE entity_id = $1`, id)
		c.Descriptions, _ = s.loadLabels(ctx, `SELECT lang, text FROM entity_description WHERE entity_id = $1`, id)
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
