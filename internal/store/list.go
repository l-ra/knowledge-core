package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
)

type ListOptions struct {
	Limit             int
	Cursor            string
	Query             string
	Kind              string // "", "entity", "property", "class"
	PackageCode       string
	IRILocal          string
	IRI               string
	InstanceOf        string
	IncludeSubclasses bool
	PropertyPID       string // filter statements by property
}

func (s *Store) ListEntities(ctx context.Context, opt ListOptions) ([]domain.Entity, string, error) {
	if opt.Limit <= 0 {
		opt.Limit = 50
	}
	if opt.Limit > 200 {
		opt.Limit = 200
	}
	q := strings.TrimSpace(opt.Query)
	args := []any{}
	where := `e.status <> 'deleted'`
	switch strings.ToLower(strings.TrimSpace(opt.Kind)) {
	case "entity", "q":
		where += ` AND NOT EXISTS (SELECT 1 FROM property_profile pp WHERE pp.entity_id = e.id)
			AND NOT EXISTS (SELECT 1 FROM class_profile cp WHERE cp.entity_id = e.id)`
	case "property", "p":
		where += ` AND EXISTS (SELECT 1 FROM property_profile pp WHERE pp.entity_id = e.id)`
	case "class", "c":
		where += ` AND EXISTS (SELECT 1 FROM class_profile cp WHERE cp.entity_id = e.id)`
	}
	if pkg := strings.TrimSpace(opt.PackageCode); pkg != "" {
		args = append(args, pkg)
		where += ` AND EXISTS (SELECT 1 FROM package p WHERE p.id = e.package_id AND p.code = $` + strconv.Itoa(len(args)) + `)`
	}
	if local := strings.TrimSpace(opt.IRILocal); local != "" {
		args = append(args, local)
		where += ` AND e.iri_local = $` + strconv.Itoa(len(args))
	}
	if iri := strings.TrimSpace(opt.IRI); iri != "" {
		args = append(args, iri)
		n := strconv.Itoa(len(args))
		where += ` AND (
			EXISTS (SELECT 1 FROM entity_iri_alias a WHERE a.entity_id = e.id AND a.iri = $` + n + `)
			OR (COALESCE(p.iri_base,'') || COALESCE(NULLIF(e.iri_local,''), e.public_id)) = $` + n + `
		)`
	}
	if inst := strings.TrimSpace(opt.InstanceOf); inst != "" {
		cfg, err := s.GetSchemaConfig(ctx)
		if err != nil {
			return nil, "", err
		}
		if strings.TrimSpace(cfg.InstanceOfProperty) == "" {
			return nil, "", fmt.Errorf("instanceOf filter requires schema-config instanceOfProperty")
		}
		classIDs := []string{inst}
		if opt.IncludeSubclasses {
			desc, err := s.classDescendants(ctx, inst)
			if err != nil {
				return nil, "", err
			}
			classIDs = append(classIDs, desc...)
		}
		args = append(args, cfg.InstanceOfProperty)
		propN := strconv.Itoa(len(args))
		args = append(args, classIDs)
		clsN := strconv.Itoa(len(args))
		where += ` AND EXISTS (
			SELECT 1 FROM statement_current sc
			JOIN entity prop ON prop.id = sc.property_id
			JOIN entity cls ON cls.id = sc.value_entity_id
			WHERE sc.subject_id = e.id
			  AND prop.public_id = $` + propN + `
			  AND cls.public_id = ANY($` + clsN + `)
		)`
	}
	if opt.Cursor != "" {
		args = append(args, opt.Cursor)
		where += ` AND e.public_id > $` + strconv.Itoa(len(args))
	}
	if q != "" {
		args = append(args, "%"+strings.ToLower(q)+"%")
		n := strconv.Itoa(len(args))
		where += ` AND (
			lower(e.public_id) LIKE $` + n + `
			OR EXISTS (
				SELECT 1 FROM entity_label el
				WHERE el.entity_id = e.id AND lower(el.text) LIKE $` + n + `
			)
		)`
	}
	args = append(args, opt.Limit+1)
	limitArg := `$` + strconv.Itoa(len(args))

	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.public_id, e.status, e.current_revision_no, e.created_at, e.updated_at,
			p.code, COALESCE(e.iri_local,''), COALESCE(p.iri_base,''),
			EXISTS(SELECT 1 FROM property_profile pp WHERE pp.entity_id = e.id),
			EXISTS(SELECT 1 FROM class_profile cp WHERE cp.entity_id = e.id)
		FROM entity e
		LEFT JOIN package p ON p.id = e.package_id
		WHERE `+where+`
		ORDER BY e.public_id
		LIMIT `+limitArg+`
	`, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var out []domain.Entity
	for rows.Next() {
		var e domain.Entity
		var id uuid.UUID
		var created, updated time.Time
		var pkgCode *string
		var iriBase string
		var isProp, isClass bool
		if err := rows.Scan(&id, &e.PublicID, &e.Status, &e.RevisionNo, &created, &updated, &pkgCode, &e.IRILocal, &iriBase, &isProp, &isClass); err != nil {
			return nil, "", err
		}
		e.ID = id
		e.CreatedAt = created
		e.UpdatedAt = updated
		if pkgCode != nil {
			e.PackageCode = *pkgCode
		}
		e.IRI = datatype.ResolveIRI(iriBase, e.IRILocal, e.PublicID, fallbackNSForPublicID(e.PublicID))
		e.Kind = domain.EntityKindEntity
		if isProp {
			e.Kind = domain.EntityKindProperty
		} else if isClass {
			e.Kind = domain.EntityKindClass
		}
		labels, _ := s.loadLabels(ctx, `SELECT lang, text FROM entity_label WHERE entity_id = $1`, id)
		descs, _ := s.loadLabels(ctx, `SELECT lang, text FROM entity_description WHERE entity_id = $1`, id)
		e.Labels = labels
		e.Descriptions = descs
		out = append(out, e)
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

func (s *Store) ListProperties(ctx context.Context, opt ListOptions) ([]domain.Property, string, error) {
	if opt.Limit <= 0 {
		opt.Limit = 50
	}
	if opt.Limit > 200 {
		opt.Limit = 200
	}
	q := strings.TrimSpace(opt.Query)
	args := []any{}
	where := `e.status <> 'deleted' AND EXISTS (SELECT 1 FROM property_profile pp WHERE pp.entity_id = e.id)`
	if opt.Cursor != "" {
		args = append(args, opt.Cursor)
		where += ` AND e.public_id > $` + strconv.Itoa(len(args))
	}
	if q != "" {
		args = append(args, "%"+strings.ToLower(q)+"%")
		n := strconv.Itoa(len(args))
		where += ` AND (
			lower(e.public_id) LIKE $` + n + `
			OR EXISTS (
				SELECT 1 FROM entity_label el
				WHERE el.entity_id = e.id AND lower(el.text) LIKE $` + n + `
			)
		)`
	}
	args = append(args, opt.Limit+1)
	limitArg := `$` + strconv.Itoa(len(args))

	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.public_id, pp.datatype, e.status, e.current_revision_no, pp.constraints,
			e.created_at, e.updated_at, pkg.code, COALESCE(e.iri_local,''), COALESCE(pkg.iri_base,'')
		FROM entity e
		JOIN property_profile pp ON pp.entity_id = e.id
		LEFT JOIN package pkg ON pkg.id = e.package_id
		WHERE `+where+`
		ORDER BY e.public_id
		LIMIT `+limitArg+`
	`, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var out []domain.Property
	for rows.Next() {
		var p domain.Property
		var id uuid.UUID
		var dt string
		var constraintsJSON []byte
		var created, updated time.Time
		var pkgCode *string
		var iriBase string
		if err := rows.Scan(&id, &p.PublicID, &dt, &p.Status, &p.RevisionNo, &constraintsJSON, &created, &updated, &pkgCode, &p.IRILocal, &iriBase); err != nil {
			return nil, "", err
		}
		p.ID = id
		p.Datatype = datatype.Type(dt)
		if pkgCode != nil {
			p.PackageCode = *pkgCode
		}
		p.IRI = datatype.ResolveIRI(iriBase, p.IRILocal, p.PublicID, rdfNSProperty)
		if len(constraintsJSON) > 0 {
			_ = json.Unmarshal(constraintsJSON, &p.Constraints)
		}
		p.CreatedAt = created
		p.UpdatedAt = updated
		labels, _ := s.loadLabels(ctx, `SELECT lang, text FROM entity_label WHERE entity_id = $1`, id)
		descs, _ := s.loadLabels(ctx, `SELECT lang, text FROM entity_description WHERE entity_id = $1`, id)
		p.Labels = labels
		p.Descriptions = descs
		out = append(out, p)
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

func (s *Store) ListLenses(ctx context.Context) ([]domain.LensDefinition, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, code, version, labels, document, created_at, updated_at
		FROM lens_definition ORDER BY code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.LensDefinition
	for rows.Next() {
		var lens domain.LensDefinition
		var id string
		var labelsJSON, docJSON []byte
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&id, &lens.Code, &lens.Version, &labelsJSON, &docJSON, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		lens.ID = id
		_ = json.Unmarshal(labelsJSON, &lens.Labels)
		_ = json.Unmarshal(docJSON, &lens.Document)
		lens.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		lens.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		out = append(out, lens)
	}
	return out, rows.Err()
}

func (s *Store) ListPackages(ctx context.Context) ([]domain.Package, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, code, lifecycle, labels, COALESCE(iri_base,''), created_at, updated_at
		FROM package ORDER BY code
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Package
	var ids []uuid.UUID
	for rows.Next() {
		var p domain.Package
		var labelsJSON []byte
		if err := rows.Scan(&p.ID, &p.Code, &p.Lifecycle, &labelsJSON, &p.IRIBase, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(labelsJSON, &p.Labels)
		depRows, err := s.pool.Query(ctx, `
			SELECT depends_on_code, version_range FROM package_dependency WHERE package_id = $1
		`, p.ID)
		if err != nil {
			return nil, err
		}
		for depRows.Next() {
			var d domain.PackageDependency
			if err := depRows.Scan(&d.DependsOnCode, &d.VersionRange); err != nil {
				depRows.Close()
				return nil, err
			}
			p.Dependencies = append(p.Dependencies, d)
		}
		depRows.Close()
		if err := depRows.Err(); err != nil {
			return nil, err
		}
		ids = append(ids, p.ID)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	latest, err := s.latestReleaseVersionsByPackageID(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].LatestReleaseVersion = latest[out[i].ID]
		if out[i].LatestReleaseVersion == "" {
			continue
		}
		dirty, err := s.packageModifiedAfterRelease(ctx, out[i].ID, out[i].LatestReleaseVersion)
		if err != nil {
			return nil, err
		}
		out[i].ModifiedAfterRelease = dirty
	}
	return out, nil
}
