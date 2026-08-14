package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
	"github.com/l-ra/knowledge-core/internal/pkgversion"
	"github.com/shopspring/decimal"
)

var ErrReleaseImmutable = errors.New("release is immutable")

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (s *Store) resolvePackageID(ctx context.Context, q rowQuerier, code string) (*uuid.UUID, error) {
	if code == "" {
		return nil, nil
	}
	var id uuid.UUID
	err := q.QueryRow(ctx, `SELECT id FROM package WHERE code = $1`, code).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("package %q: %w", code, err)
	}
	return &id, nil
}

func (s *Store) resolvePackageIDRequired(ctx context.Context, q rowQuerier, code string) (uuid.UUID, error) {
	if strings.TrimSpace(code) == "" {
		return uuid.Nil, fmt.Errorf("package code required")
	}
	id, err := s.resolvePackageID(ctx, q, code)
	if err != nil {
		return uuid.Nil, err
	}
	if id == nil {
		return uuid.Nil, fmt.Errorf("package %q not found", code)
	}
	return *id, nil
}

func (s *Store) CreatePackage(ctx context.Context, meta domain.WriteMeta, in domain.CreatePackageInput) (*domain.WriteResult[domain.Package], error) {
	if in.Code == "" {
		return nil, fmt.Errorf("package code required")
	}
	if in.Lifecycle == "" {
		in.Lifecycle = domain.PackageReleased
	}
	labels := in.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	if err := datatype.RequireLabelEN(labels); err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	id := datatype.NewUUID()
	now := time.Now().UTC()
	labelsJSON, _ := json.Marshal(labels)
	iriBase, err := datatype.NormalizeIRIBase(in.IRIBase)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO package (id, code, lifecycle, labels, iri_base, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$6)
	`, id, in.Code, string(in.Lifecycle), labelsJSON, iriBase, now)
	if err != nil {
		return nil, err
	}
	for _, dep := range in.Dependencies {
		_, err = tx.Exec(ctx, `
			INSERT INTO package_dependency (package_id, depends_on_code, version_range)
			VALUES ($1,$2,$3)
		`, id, dep.DependsOnCode, dep.VersionRange)
		if err != nil {
			return nil, err
		}
	}

	cs, err := s.beginChangeSetTx(ctx, tx, meta)
	if err != nil {
		return nil, err
	}
	if err := cs.addItem(ctx, tx, "package", id, in.Code, "create", nil); err != nil {
		return nil, err
	}

	pkg := domain.Package{
		ID: id, Code: in.Code, Lifecycle: in.Lifecycle, IRIBase: iriBase,
		Labels: labels, Dependencies: in.Dependencies,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.finalizeChangeSet(ctx, tx, cs, pkg); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Package]{Value: pkg, ChangeSet: s.changeSetDomain(cs, meta)}, nil
}

func (s *Store) GetPackageByCode(ctx context.Context, code string) (*domain.Package, error) {
	var pkg domain.Package
	var labelsJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, code, lifecycle, labels, COALESCE(iri_base,''), created_at, updated_at FROM package WHERE code = $1
	`, code).Scan(&pkg.ID, &pkg.Code, &pkg.Lifecycle, &labelsJSON, &pkg.IRIBase, &pkg.CreatedAt, &pkg.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(labelsJSON, &pkg.Labels)

	rows, err := s.pool.Query(ctx, `
		SELECT depends_on_code, version_range FROM package_dependency WHERE package_id = $1
	`, pkg.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d domain.PackageDependency
		if err := rows.Scan(&d.DependsOnCode, &d.VersionRange); err != nil {
			return nil, err
		}
		pkg.Dependencies = append(pkg.Dependencies, d)
	}
	return &pkg, rows.Err()
}

func (s *Store) UpdatePackage(ctx context.Context, meta domain.WriteMeta, code string, in domain.UpdatePackageInput) (*domain.WriteResult[domain.Package], error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	pkg, err := s.GetPackageByCode(ctx, code)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if in.IRIBase != nil {
		base, err := datatype.NormalizeIRIBase(*in.IRIBase)
		if err != nil {
			return nil, err
		}
		pkg.IRIBase = base
	}
	if in.Labels != nil {
		if err := datatype.RequireLabelEN(in.Labels); err != nil {
			return nil, err
		}
		pkg.Labels = in.Labels
	}
	labelsJSON, _ := json.Marshal(pkg.Labels)
	_, err = tx.Exec(ctx, `
		UPDATE package SET iri_base = $2, labels = $3, updated_at = $4 WHERE id = $1
	`, pkg.ID, pkg.IRIBase, labelsJSON, now)
	if err != nil {
		return nil, err
	}
	pkg.UpdatedAt = now

	cs, err := s.beginChangeSetTx(ctx, tx, meta)
	if err != nil {
		return nil, err
	}
	if err := cs.addItem(ctx, tx, "package", pkg.ID, pkg.Code, "update", map[string]any{"iriBase": pkg.IRIBase}); err != nil {
		return nil, err
	}
	if err := s.finalizeChangeSet(ctx, tx, cs, *pkg); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Package]{Value: *pkg, ChangeSet: s.changeSetDomain(cs, meta)}, nil
}

func (s *Store) findMatchingRelease(ctx context.Context, tx pgx.Tx, depCode, rangeSpec string) (string, error) {
	rows, err := tx.Query(ctx, `
		SELECT r.version FROM release r
		JOIN package p ON p.id = r.package_id
		WHERE p.code = $1
		ORDER BY r.published_at DESC
	`, depCode)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var ver string
		if err := rows.Scan(&ver); err != nil {
			return "", err
		}
		ok, err := pkgversion.MatchesRange(ver, rangeSpec)
		if err != nil {
			return "", err
		}
		if ok {
			return ver, nil
		}
	}
	return "", fmt.Errorf("no release of %q matching %q", depCode, rangeSpec)
}

func (s *Store) PublishRelease(ctx context.Context, meta domain.WriteMeta, packageCode string, in domain.PublishReleaseInput) (*domain.WriteResult[domain.Release], error) {
	if _, _, _, err := pkgversion.ParseVersion(in.Version); err != nil {
		return nil, fmt.Errorf("invalid version: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var packageID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM package WHERE code = $1`, packageCode).Scan(&packageID)
	if err != nil {
		return nil, err
	}

	var exists bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM release WHERE package_id = $1 AND version = $2)`, packageID, in.Version).Scan(&exists)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("%w: release %s@%s already exists", ErrReleaseImmutable, packageCode, in.Version)
	}

	// Resolve dependencies
	depRows, err := tx.Query(ctx, `SELECT depends_on_code, version_range FROM package_dependency WHERE package_id = $1`, packageID)
	if err != nil {
		return nil, err
	}
	var deps []domain.ReleaseDependency
	for depRows.Next() {
		var code, rng string
		if err := depRows.Scan(&code, &rng); err != nil {
			depRows.Close()
			return nil, err
		}
		ver, err := s.findMatchingRelease(ctx, tx, code, rng)
		if err != nil {
			depRows.Close()
			return nil, err
		}
		deps = append(deps, domain.ReleaseDependency{DependencyCode: code, DependencyVersion: ver})
	}
	depRows.Close()
	if err := depRows.Err(); err != nil {
		return nil, err
	}

	releaseID := datatype.NewUUID()
	now := time.Now().UTC()
	var objects []domain.ReleaseObject

	// Ordinary entities (not property/class profiles)
	entRows, err := tx.Query(ctx, `
		SELECT e.public_id, e.current_revision_no FROM entity e
		WHERE e.package_id = $1
		  AND NOT EXISTS (SELECT 1 FROM property_profile pp WHERE pp.entity_id = e.id)
		  AND NOT EXISTS (SELECT 1 FROM class_profile cp WHERE cp.entity_id = e.id)
	`, packageID)
	if err != nil {
		return nil, err
	}
	for entRows.Next() {
		var pid string
		var rev int
		if err := entRows.Scan(&pid, &rev); err != nil {
			entRows.Close()
			return nil, err
		}
		objects = append(objects, domain.ReleaseObject{ObjectType: "entity", ObjectPublicID: pid, RevisionNo: rev})
	}
	entRows.Close()

	propRows, err := tx.Query(ctx, `
		SELECT e.public_id, e.current_revision_no
		FROM entity e
		JOIN property_profile pp ON pp.entity_id = e.id
		WHERE e.package_id = $1
	`, packageID)
	if err != nil {
		return nil, err
	}
	for propRows.Next() {
		var pid string
		var rev int
		if err := propRows.Scan(&pid, &rev); err != nil {
			propRows.Close()
			return nil, err
		}
		objects = append(objects, domain.ReleaseObject{ObjectType: "property", ObjectPublicID: pid, RevisionNo: rev})
	}
	propRows.Close()

	classRows, err := tx.Query(ctx, `
		SELECT e.public_id, e.current_revision_no
		FROM entity e
		JOIN class_profile cp ON cp.entity_id = e.id
		WHERE e.package_id = $1
	`, packageID)
	if err != nil {
		return nil, err
	}
	for classRows.Next() {
		var cid string
		var rev int
		if err := classRows.Scan(&cid, &rev); err != nil {
			classRows.Close()
			return nil, err
		}
		objects = append(objects, domain.ReleaseObject{ObjectType: "class", ObjectPublicID: cid, RevisionNo: rev})
	}
	classRows.Close()

	stmtRows, err := tx.Query(ctx, `
		SELECT public_id, current_revision_no FROM statement WHERE package_id = $1
	`, packageID)
	if err != nil {
		return nil, err
	}
	for stmtRows.Next() {
		var pid string
		var rev int
		if err := stmtRows.Scan(&pid, &rev); err != nil {
			stmtRows.Close()
			return nil, err
		}
		objects = append(objects, domain.ReleaseObject{ObjectType: "statement", ObjectPublicID: pid, RevisionNo: rev})
	}
	stmtRows.Close()

	shapeRows, err := tx.Query(ctx, `
		SELECT code FROM shape_profile WHERE package_id = $1 ORDER BY code
	`, packageID)
	if err != nil {
		return nil, err
	}
	for shapeRows.Next() {
		var code string
		if err := shapeRows.Scan(&code); err != nil {
			shapeRows.Close()
			return nil, err
		}
		objects = append(objects, domain.ReleaseObject{ObjectType: "shape", ObjectPublicID: code, RevisionNo: 1})
	}
	shapeRows.Close()

	manifest := domain.BundleManifest{
		FormatVersion: 1,
		Package:       packageCode,
		Version:       in.Version,
		PublishedAt:   now.UTC().Format(time.RFC3339Nano),
		Dependencies:  deps,
		ObjectIndex:   objects,
	}
	manifestJSON, _ := json.Marshal(manifest)

	_, err = tx.Exec(ctx, `
		INSERT INTO release (id, package_id, version, published_at, manifest)
		VALUES ($1,$2,$3,$4,$5)
	`, releaseID, packageID, in.Version, now, manifestJSON)
	if err != nil {
		return nil, err
	}

	for _, d := range deps {
		_, err = tx.Exec(ctx, `
			INSERT INTO release_dependency (release_id, dependency_code, dependency_version)
			VALUES ($1,$2,$3)
		`, releaseID, d.DependencyCode, d.DependencyVersion)
		if err != nil {
			return nil, err
		}
	}

	for _, obj := range objects {
		_, err = tx.Exec(ctx, `
			INSERT INTO release_object (release_id, object_type, object_public_id, revision_no)
			VALUES ($1,$2,$3,$4)
		`, releaseID, obj.ObjectType, obj.ObjectPublicID, obj.RevisionNo)
		if err != nil {
			return nil, err
		}
	}

	cs, err := s.beginChangeSetTx(ctx, tx, meta)
	if err != nil {
		return nil, err
	}
	if err := cs.addItem(ctx, tx, "release", releaseID, packageCode+"@"+in.Version, "publish", nil); err != nil {
		return nil, err
	}

	rel := domain.Release{
		ID: releaseID, PackageCode: packageCode, Version: in.Version,
		PublishedAt: now, Dependencies: deps, Objects: objects,
	}
	if err := s.finalizeChangeSet(ctx, tx, cs, rel); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Release]{Value: rel, ChangeSet: s.changeSetDomain(cs, meta)}, nil
}

func (s *Store) GetRelease(ctx context.Context, packageCode, version string) (*domain.Release, error) {
	var rel domain.Release
	var releaseID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT r.id, p.code, r.version, r.published_at
		FROM release r JOIN package p ON p.id = r.package_id
		WHERE p.code = $1 AND r.version = $2
	`, packageCode, version).Scan(&releaseID, &rel.PackageCode, &rel.Version, &rel.PublishedAt)
	if err != nil {
		return nil, err
	}
	rel.ID = releaseID

	depRows, err := s.pool.Query(ctx, `
		SELECT dependency_code, dependency_version FROM release_dependency WHERE release_id = $1
	`, releaseID)
	if err != nil {
		return nil, err
	}
	defer depRows.Close()
	for depRows.Next() {
		var d domain.ReleaseDependency
		if err := depRows.Scan(&d.DependencyCode, &d.DependencyVersion); err != nil {
			return nil, err
		}
		rel.Dependencies = append(rel.Dependencies, d)
	}

	objRows, err := s.pool.Query(ctx, `
		SELECT object_type, object_public_id, revision_no FROM release_object WHERE release_id = $1 ORDER BY object_type, object_public_id
	`, releaseID)
	if err != nil {
		return nil, err
	}
	defer objRows.Close()
	for objRows.Next() {
		var o domain.ReleaseObject
		if err := objRows.Scan(&o.ObjectType, &o.ObjectPublicID, &o.RevisionNo); err != nil {
			return nil, err
		}
		rel.Objects = append(rel.Objects, o)
	}
	return &rel, objRows.Err()
}

func (s *Store) ExportReleaseBundle(ctx context.Context, packageCode, version string) (*domain.Bundle, error) {
	rel, err := s.GetRelease(ctx, packageCode, version)
	if err != nil {
		return nil, err
	}

	bundle := &domain.Bundle{
		Manifest: domain.BundleManifest{
			FormatVersion: 1,
			Package:       rel.PackageCode,
			Version:       rel.Version,
			PublishedAt:   rel.PublishedAt.UTC().Format(time.RFC3339Nano),
			Dependencies:  rel.Dependencies,
			ObjectIndex:   rel.Objects,
		},
		Releases: []domain.BundleManifest{{
			FormatVersion: 1,
			Package:       rel.PackageCode,
			Version:       rel.Version,
			PublishedAt:   rel.PublishedAt.UTC().Format(time.RFC3339Nano),
			Dependencies:  rel.Dependencies,
			ObjectIndex:   rel.Objects,
		}},
	}

	// Include dependency closure objects
	allObjects := append([]domain.ReleaseObject{}, rel.Objects...)
	for _, dep := range rel.Dependencies {
		depRel, err := s.GetRelease(ctx, dep.DependencyCode, dep.DependencyVersion)
		if err != nil {
			return nil, err
		}
		bundle.Releases = append(bundle.Releases, domain.BundleManifest{
			FormatVersion: 1,
			Package:       depRel.PackageCode,
			Version:       depRel.Version,
			PublishedAt:   depRel.PublishedAt.UTC().Format(time.RFC3339Nano),
			Dependencies:  depRel.Dependencies,
			ObjectIndex:   depRel.Objects,
		})
		allObjects = append(allObjects, depRel.Objects...)
	}

	refIDs := map[string]struct{}{}
	seen := map[string]struct{}{}
	for _, obj := range allObjects {
		key := obj.ObjectType + ":" + obj.ObjectPublicID
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		switch obj.ObjectType {
		case "entity":
			be, err := s.exportEntityAtRevision(ctx, obj.ObjectPublicID, obj.RevisionNo)
			if err != nil {
				return nil, err
			}
			bundle.Entities = append(bundle.Entities, *be)
		case "property":
			bp, err := s.exportPropertyAtRevision(ctx, obj.ObjectPublicID, obj.RevisionNo)
			if err != nil {
				return nil, err
			}
			bundle.Properties = append(bundle.Properties, *bp)
		case "class":
			bc, err := s.exportClassAtRevision(ctx, obj.ObjectPublicID, obj.RevisionNo)
			if err != nil {
				return nil, err
			}
			bundle.Classes = append(bundle.Classes, *bc)
		case "statement":
			bs, err := s.exportStatementAtRevision(ctx, obj.ObjectPublicID, obj.RevisionNo)
			if err != nil {
				return nil, err
			}
			bundle.Statements = append(bundle.Statements, *bs)
			for _, rid := range bs.ReferenceIDs {
				refIDs[rid] = struct{}{}
			}
		case "shape":
			sh, err := s.GetShapeByCode(ctx, obj.ObjectPublicID)
			if err != nil {
				return nil, err
			}
			bundle.Shapes = append(bundle.Shapes, domain.BundleShape{
				Code: sh.Code, PackageCode: sh.PackageCode, ClassID: sh.ClassPID,
				Document: sh.Document, RevisionNo: 1,
			})
		}
	}
	for rid := range refIDs {
		br, err := s.exportReference(ctx, rid)
		if err != nil {
			return nil, err
		}
		bundle.References = append(bundle.References, *br)
	}
	return bundle, nil
}

func (s *Store) exportReference(ctx context.Context, rid string) (*domain.BundleReference, error) {
	var br domain.BundleReference
	var fieldsJSON []byte
	err := s.pool.QueryRow(ctx, `SELECT public_id, fields FROM reference WHERE public_id = $1`, rid).
		Scan(&br.PublicID, &fieldsJSON)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(fieldsJSON, &br.Fields)
	return &br, nil
}

func (s *Store) exportEntityAtRevision(ctx context.Context, qid string, rev int) (*domain.BundleEntity, error) {
	var be domain.BundleEntity
	be.PublicID = qid
	be.RevisionNo = rev
	var labelsJSON, descJSON []byte
	var pkgCode *string
	err := s.pool.QueryRow(ctx, `
		SELECT er.status, er.labels, er.descriptions, pkg.code
		FROM entity_revision er
		JOIN entity e ON e.id = er.entity_id
		LEFT JOIN package pkg ON pkg.id = e.package_id
		WHERE e.public_id = $1 AND er.revision_no = $2
	`, qid, rev).Scan(&be.Status, &labelsJSON, &descJSON, &pkgCode)
	if err != nil {
		return nil, err
	}
	be.Labels, _ = jsonToLabels(labelsJSON)
	be.Descriptions, _ = jsonToLabels(descJSON)
	if pkgCode != nil {
		be.PackageCode = *pkgCode
	}
	return &be, nil
}

func (s *Store) exportPropertyAtRevision(ctx context.Context, pid string, rev int) (*domain.BundleProperty, error) {
	var bp domain.BundleProperty
	bp.PublicID = pid
	bp.RevisionNo = rev
	var labelsJSON, descJSON []byte
	var dt string
	var pkgCode *string
	err := s.pool.QueryRow(ctx, `
		SELECT er.status, pp.datatype, er.labels, er.descriptions, pkg.code
		FROM entity_revision er
		JOIN entity e ON e.id = er.entity_id
		JOIN property_profile pp ON pp.entity_id = e.id
		LEFT JOIN package pkg ON pkg.id = e.package_id
		WHERE e.public_id = $1 AND er.revision_no = $2
	`, pid, rev).Scan(&bp.Status, &dt, &labelsJSON, &descJSON, &pkgCode)
	if err != nil {
		return nil, err
	}
	bp.Datatype = datatype.Type(dt)
	bp.Labels, _ = jsonToLabels(labelsJSON)
	bp.Descriptions, _ = jsonToLabels(descJSON)
	if pkgCode != nil {
		bp.PackageCode = *pkgCode
	}
	return &bp, nil
}

func (s *Store) exportClassAtRevision(ctx context.Context, cid string, rev int) (*domain.BundleClass, error) {
	var bc domain.BundleClass
	bc.PublicID = cid
	bc.RevisionNo = rev
	var labelsJSON, descJSON, docJSON []byte
	var pkgCode *string
	err := s.pool.QueryRow(ctx, `
		SELECT er.status, er.labels, er.descriptions, pkg.code, cp.document
		FROM entity_revision er
		JOIN entity e ON e.id = er.entity_id
		JOIN class_profile cp ON cp.entity_id = e.id
		LEFT JOIN package pkg ON pkg.id = e.package_id
		WHERE e.public_id = $1 AND er.revision_no = $2
	`, cid, rev).Scan(&bc.Status, &labelsJSON, &descJSON, &pkgCode, &docJSON)
	if err != nil {
		return nil, err
	}
	bc.Labels, _ = jsonToLabels(labelsJSON)
	bc.Descriptions, _ = jsonToLabels(descJSON)
	if pkgCode != nil {
		bc.PackageCode = *pkgCode
	}
	var doc domain.ClassDocument
	_ = json.Unmarshal(docJSON, &doc)
	bc.SubClassOf = doc.SubClassOf
	return &bc, nil
}

func (s *Store) exportStatementAtRevision(ctx context.Context, sid string, rev int) (*domain.BundleStatement, error) {
	var statementID uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT id FROM statement WHERE public_id = $1`, sid).Scan(&statementID)
	if err != nil {
		return nil, err
	}

	row := s.pool.QueryRow(ctx, `
		SELECT sr.status, e.public_id, p.public_id, pkg.code,
			sr.value_type, sr.value_bool, sr.value_int64, sr.value_numeric, sr.value_date, sr.value_timestamptz,
			sr.value_text, sr.value_entity_id, sr.value_json
		FROM statement_revision sr
		JOIN statement st ON st.id = sr.statement_id
		JOIN entity e ON e.id = st.subject_id
		JOIN property_profile pp ON pp.entity_id = st.property_id
		JOIN entity p ON p.id = pp.entity_id
		LEFT JOIN package pkg ON pkg.id = st.package_id
		WHERE st.public_id = $1 AND sr.revision_no = $2
	`, sid, rev)
	var bs domain.BundleStatement
	bs.PublicID = sid
	bs.RevisionNo = rev
	var sv storedValue
	var numeric *decimal.Decimal
	var pkgCode *string
	err = row.Scan(&bs.Status, &bs.Subject, &bs.Property, &pkgCode,
		&sv.Type, &sv.Bool, &sv.Int64, &numeric, &sv.Date, &sv.Timestamptz,
		&sv.Text, &sv.EntityID, &sv.JSON)
	if err != nil {
		return nil, err
	}
	sv.Numeric = numeric
	val, err := decodeValue(sv)
	if err != nil {
		return nil, err
	}
	bs.Value = val
	if pkgCode != nil {
		bs.PackageCode = *pkgCode
	}

	quals, refs, err := s.loadStatementProvenanceAtRevision(ctx, s.pool, statementID, rev)
	if err != nil {
		return nil, err
	}
	for _, q := range quals {
		bs.Qualifiers = append(bs.Qualifiers, domain.QualifierInput{Property: q.PropertyPID, Value: q.Value})
	}
	bs.ReferenceIDs = refs
	return &bs, nil
}

// MutateRelease rejects any attempt to alter published release (A13).
func (s *Store) MutateRelease(ctx context.Context, packageCode, version string) error {
	_, err := s.GetRelease(ctx, packageCode, version)
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: cannot modify release %s@%s", ErrReleaseImmutable, packageCode, version)
}

func (s *Store) ListReleases(ctx context.Context, packageCode string) ([]domain.Release, error) {
	packageID, err := s.resolvePackageIDRequired(ctx, s.pool, packageCode)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, version, published_at
		FROM release
		WHERE package_id = $1
		ORDER BY published_at DESC, version DESC
	`, packageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Release
	for rows.Next() {
		var rel domain.Release
		rel.PackageCode = packageCode
		if err := rows.Scan(&rel.ID, &rel.Version, &rel.PublishedAt); err != nil {
			return nil, err
		}
		out = append(out, rel)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range out {
		depRows, err := s.pool.Query(ctx, `
			SELECT dependency_code, dependency_version
			FROM release_dependency WHERE release_id = $1
			ORDER BY dependency_code
		`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for depRows.Next() {
			var d domain.ReleaseDependency
			if err := depRows.Scan(&d.DependencyCode, &d.DependencyVersion); err != nil {
				depRows.Close()
				return nil, err
			}
			out[i].Dependencies = append(out[i].Dependencies, d)
		}
		depRows.Close()
		if err := depRows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) ListPackageObjects(ctx context.Context, packageCode string) ([]domain.PackageObject, error) {
	packageID, err := s.resolvePackageIDRequired(ctx, s.pool, packageCode)
	if err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT object_type, public_id, revision_no, entity_id FROM (
			SELECT 'entity'::text AS object_type, e.public_id, e.current_revision_no AS revision_no, e.id AS entity_id
			FROM entity e
			WHERE e.package_id = $1
			  AND NOT EXISTS (SELECT 1 FROM property_profile pp WHERE pp.entity_id = e.id)
			  AND NOT EXISTS (SELECT 1 FROM class_profile cp WHERE cp.entity_id = e.id)
			UNION ALL
			SELECT 'property', e.public_id, e.current_revision_no, e.id
			FROM entity e
			JOIN property_profile pp ON pp.entity_id = e.id
			WHERE e.package_id = $1
			UNION ALL
			SELECT 'class', e.public_id, e.current_revision_no, e.id
			FROM entity e
			JOIN class_profile cp ON cp.entity_id = e.id
			WHERE e.package_id = $1
			UNION ALL
			SELECT 'shape', sp.code, 1, NULL::uuid
			FROM shape_profile sp
			WHERE sp.package_id = $1
			UNION ALL
			SELECT 'statement', st.public_id, st.current_revision_no, NULL::uuid
			FROM statement st
			WHERE st.package_id = $1
		) t
		ORDER BY object_type, public_id
	`, packageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.PackageObject
	for rows.Next() {
		var o domain.PackageObject
		var entityID *uuid.UUID
		if err := rows.Scan(&o.ObjectType, &o.PublicID, &o.RevisionNo, &entityID); err != nil {
			return nil, err
		}
		if entityID != nil {
			o.Labels, _ = s.loadLabels(ctx, `SELECT lang, text FROM entity_label WHERE entity_id = $1`, *entityID)
		}
		if o.Labels == nil {
			o.Labels = map[string]string{}
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (s *Store) ListObjectReleases(ctx context.Context, publicID string) ([]domain.ObjectRelease, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.code, r.version, ro.revision_no
		FROM release_object ro
		JOIN release r ON r.id = ro.release_id
		JOIN package p ON p.id = r.package_id
		WHERE ro.object_public_id = $1
		ORDER BY p.code, r.published_at DESC, r.version DESC
	`, publicID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.ObjectRelease
	for rows.Next() {
		var o domain.ObjectRelease
		if err := rows.Scan(&o.PackageCode, &o.Version, &o.RevisionNo); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}
