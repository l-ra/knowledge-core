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
	"github.com/l-ra/knowledge-core/internal/pkgcompat"
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
	rootID, err := s.createPackageRootInTx(ctx, tx, cs, meta, pkg, in.Descriptions)
	if err != nil {
		return nil, err
	}
	pkg.RootEntityID = rootID
	if len(in.Descriptions) > 0 {
		pkg.Descriptions = in.Descriptions
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ver, ok, err := s.latestPackageReleaseVersion(ctx, code)
	if err != nil {
		return nil, err
	}
	if ok {
		pkg.LatestReleaseVersion = ver
		dirty, err := s.packageModifiedAfterRelease(ctx, pkg.ID, ver)
		if err != nil {
			return nil, err
		}
		pkg.ModifiedAfterRelease = dirty
	}
	if err := s.fillPackageRoot(ctx, &pkg); err != nil {
		return nil, err
	}
	return &pkg, nil
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
	oldBase := pkg.IRIBase
	iriBaseChanged := false
	labelsChanged := false
	if in.IRIBase != nil {
		base, err := datatype.NormalizeIRIBase(*in.IRIBase)
		if err != nil {
			return nil, err
		}
		if base != pkg.IRIBase {
			iriBaseChanged = true
		}
		pkg.IRIBase = base
	}
	if in.Labels != nil {
		if err := datatype.RequireLabelEN(in.Labels); err != nil {
			return nil, err
		}
		pkg.Labels = in.Labels
		labelsChanged = true
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
	if iriBaseChanged {
		if oldBase == "" && pkg.IRIBase != "" {
			rootID, err := s.createPackageRootInTx(ctx, tx, cs, meta, *pkg, pkg.Descriptions)
			if err != nil {
				return nil, err
			}
			pkg.RootEntityID = rootID
		} else if pkg.IRIBase != "" {
			if err := s.relocatePackageRootTx(ctx, tx, *pkg, oldBase, pkg.IRIBase); err != nil {
				return nil, err
			}
			pkg.RootEntityID = pkg.IRIBase
			if err := s.ensurePackageRootStatementsTx(ctx, tx, cs, meta, *pkg, pkg.IRIBase); err != nil {
				return nil, err
			}
		}
	}
	if labelsChanged {
		if err := s.syncRootLabelsFromPackageTx(ctx, tx, cs, meta, *pkg); err != nil {
			return nil, err
		}
	}
	if err := s.finalizeChangeSet(ctx, tx, cs, *pkg); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	_ = s.fillPackageRoot(ctx, pkg)
	return &domain.WriteResult[domain.Package]{Value: *pkg, ChangeSet: s.changeSetDomain(cs, meta)}, nil
}

func (s *Store) DeletePackage(ctx context.Context, meta domain.WriteMeta, code string) (*domain.WriteResult[domain.Package], error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var pkg domain.Package
	var labelsJSON []byte
	if err := tx.QueryRow(ctx, `
		SELECT id, code, lifecycle, labels, COALESCE(iri_base,''), created_at, updated_at
		FROM package
		WHERE code = $1
	`, code).Scan(&pkg.ID, &pkg.Code, &pkg.Lifecycle, &labelsJSON, &pkg.IRIBase, &pkg.CreatedAt, &pkg.UpdatedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(labelsJSON, &pkg.Labels)

	depRows, err := tx.Query(ctx, `
		SELECT depends_on_code, version_range FROM package_dependency WHERE package_id = $1
	`, pkg.ID)
	if err != nil {
		return nil, err
	}
	for depRows.Next() {
		var d domain.PackageDependency
		if err := depRows.Scan(&d.DependsOnCode, &d.VersionRange); err != nil {
			depRows.Close()
			return nil, err
		}
		pkg.Dependencies = append(pkg.Dependencies, d)
	}
	if err := depRows.Err(); err != nil {
		depRows.Close()
		return nil, err
	}
	depRows.Close()

	cs, err := s.beginChangeSetTx(ctx, tx, meta)
	if err != nil {
		return nil, err
	}

	statementRows, err := tx.Query(ctx, `
		WITH pkg_entities AS (
			SELECT id FROM entity WHERE package_id = $1
		)
		SELECT DISTINCT st.id
		FROM statement st
		LEFT JOIN statement_qualifier sq ON sq.statement_id = st.id
		WHERE st.package_id = $1
		   OR st.subject_id IN (SELECT id FROM pkg_entities)
		   OR st.property_id IN (SELECT id FROM pkg_entities)
		   OR st.value_entity_id IN (SELECT id FROM pkg_entities)
		   OR sq.property_id IN (SELECT id FROM pkg_entities)
		   OR sq.value_entity_id IN (SELECT id FROM pkg_entities)
	`, pkg.ID)
	if err != nil {
		return nil, err
	}
	statementIDs := make([]uuid.UUID, 0)
	for statementRows.Next() {
		var id uuid.UUID
		if err := statementRows.Scan(&id); err != nil {
			statementRows.Close()
			return nil, err
		}
		statementIDs = append(statementIDs, id)
	}
	if err := statementRows.Err(); err != nil {
		statementRows.Close()
		return nil, err
	}
	statementRows.Close()
	if len(statementIDs) > 0 {
		if _, err := tx.Exec(ctx, `DELETE FROM statement WHERE id = ANY($1)`, statementIDs); err != nil {
			return nil, err
		}
	}

	if _, err := tx.Exec(ctx, `DELETE FROM package_dependency WHERE depends_on_code = $1`, pkg.Code); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM entity WHERE package_id = $1`, pkg.ID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		DELETE FROM reference r
		WHERE NOT EXISTS (SELECT 1 FROM statement_reference sr WHERE sr.reference_id = r.id)
		  AND NOT EXISTS (SELECT 1 FROM statement_revision_reference srr WHERE srr.reference_id = r.id)
	`); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM package WHERE id = $1`, pkg.ID); err != nil {
		return nil, err
	}

	if err := cs.addItem(ctx, tx, "package", pkg.ID, pkg.Code, "delete", nil); err != nil {
		return nil, err
	}
	if err := s.finalizeChangeSet(ctx, tx, cs, pkg); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Package]{Value: pkg, ChangeSet: s.changeSetDomain(cs, meta)}, nil
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
	var pkgIRIBase, pkgLifecycle string
	var pkgLabelsJSON []byte
	err = tx.QueryRow(ctx, `
		SELECT id, COALESCE(iri_base,''), lifecycle, labels FROM package WHERE code = $1
	`, packageCode).Scan(&packageID, &pkgIRIBase, &pkgLifecycle, &pkgLabelsJSON)
	if err != nil {
		return nil, err
	}
	pkgLabels, _ := jsonToLabels(pkgLabelsJSON)

	var exists bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM release WHERE package_id = $1 AND version = $2)`, packageID, in.Version).Scan(&exists)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("%w: release %s@%s already exists", ErrReleaseImmutable, packageCode, in.Version)
	}

	// BC vs previous release (if any): live current must be backward-compatible with last published snapshot.
	if prevVer, ok, err := s.latestPackageReleaseVersion(ctx, packageCode); err != nil {
		return nil, err
	} else if ok {
		prevBundle, err := s.ExportReleaseBundle(ctx, packageCode, prevVer)
		if err != nil {
			return nil, err
		}
		live, err := s.livePackageSnapshot(ctx, packageCode)
		if err != nil {
			return nil, err
		}
		old := pkgcompat.FilterByPackage(pkgcompat.SnapshotFromBundle(*prevBundle), packageCode)
		if err := s.ensureBackwardCompatible(fmt.Sprintf("publish %s@%s from %s", packageCode, in.Version, prevVer), old, live); err != nil {
			return nil, err
		}
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
		IRIBase:       pkgIRIBase,
		Lifecycle:     domain.PackageLifecycle(pkgLifecycle),
		Labels:        pkgLabels,
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

	var pkgIRIBase, pkgLifecycle string
	var pkgLabelsJSON []byte
	_ = s.pool.QueryRow(ctx, `
		SELECT COALESCE(iri_base,''), lifecycle, labels FROM package WHERE code = $1
	`, packageCode).Scan(&pkgIRIBase, &pkgLifecycle, &pkgLabelsJSON)
	pkgLabels, _ := jsonToLabels(pkgLabelsJSON)

	mainManifest := domain.BundleManifest{
		FormatVersion: 1,
		Package:       rel.PackageCode,
		Version:       rel.Version,
		PublishedAt:   rel.PublishedAt.UTC().Format(time.RFC3339Nano),
		IRIBase:       pkgIRIBase,
		Lifecycle:     domain.PackageLifecycle(pkgLifecycle),
		Labels:        pkgLabels,
		Dependencies:  rel.Dependencies,
		ObjectIndex:   rel.Objects,
	}

	bundle := &domain.Bundle{
		Manifest: mainManifest,
		Releases: []domain.BundleManifest{mainManifest},
	}

	// Include dependency closure objects. Stamp PackageCode from the owning
	// release so FilterByPackage stays correct even if entities were later moved.
	type indexedObj struct {
		obj         domain.ReleaseObject
		packageCode string
	}
	allObjects := make([]indexedObj, 0, len(rel.Objects))
	for _, o := range rel.Objects {
		allObjects = append(allObjects, indexedObj{obj: o, packageCode: packageCode})
	}
	for _, dep := range rel.Dependencies {
		depRel, err := s.GetRelease(ctx, dep.DependencyCode, dep.DependencyVersion)
		if err != nil {
			return nil, err
		}
		var depBase, depLife string
		var depLabelsJSON []byte
		_ = s.pool.QueryRow(ctx, `
			SELECT COALESCE(iri_base,''), lifecycle, labels FROM package WHERE code = $1
		`, dep.DependencyCode).Scan(&depBase, &depLife, &depLabelsJSON)
		depLabels, _ := jsonToLabels(depLabelsJSON)
		bundle.Releases = append(bundle.Releases, domain.BundleManifest{
			FormatVersion: 1,
			Package:       depRel.PackageCode,
			Version:       depRel.Version,
			PublishedAt:   depRel.PublishedAt.UTC().Format(time.RFC3339Nano),
			IRIBase:       depBase,
			Lifecycle:     domain.PackageLifecycle(depLife),
			Labels:        depLabels,
			Dependencies:  depRel.Dependencies,
			ObjectIndex:   depRel.Objects,
		})
		for _, o := range depRel.Objects {
			allObjects = append(allObjects, indexedObj{obj: o, packageCode: dep.DependencyCode})
		}
	}

	refIDs := map[string]struct{}{}
	seen := map[string]struct{}{}
	for _, item := range allObjects {
		obj := item.obj
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
			be.PackageCode = item.packageCode
			bundle.Entities = append(bundle.Entities, *be)
		case "property":
			bp, err := s.exportPropertyAtRevision(ctx, obj.ObjectPublicID, obj.RevisionNo)
			if err != nil {
				return nil, err
			}
			bp.PackageCode = item.packageCode
			bundle.Properties = append(bundle.Properties, *bp)
		case "class":
			bc, err := s.exportClassAtRevision(ctx, obj.ObjectPublicID, obj.RevisionNo)
			if err != nil {
				return nil, err
			}
			bc.PackageCode = item.packageCode
			bundle.Classes = append(bundle.Classes, *bc)
		case "statement":
			bs, err := s.exportStatementAtRevision(ctx, obj.ObjectPublicID, obj.RevisionNo)
			if err != nil {
				return nil, err
			}
			bs.PackageCode = item.packageCode
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
				Code: sh.Code, PackageCode: item.packageCode, ClassID: sh.ClassPID,
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
	var iriLocal string
	err := s.pool.QueryRow(ctx, `
		SELECT er.status, er.labels, er.descriptions, pkg.code, COALESCE(e.iri_local,'')
		FROM entity_revision er
		JOIN entity e ON e.id = er.entity_id
		LEFT JOIN package pkg ON pkg.id = e.package_id
		WHERE e.public_id = $1 AND er.revision_no = $2
	`, qid, rev).Scan(&be.Status, &labelsJSON, &descJSON, &pkgCode, &iriLocal)
	if err != nil {
		return nil, err
	}
	be.Labels, _ = jsonToLabels(labelsJSON)
	be.Descriptions, _ = jsonToLabels(descJSON)
	if pkgCode != nil {
		be.PackageCode = *pkgCode
	}
	be.IRILocal = iriLocal
	return &be, nil
}

func (s *Store) exportPropertyAtRevision(ctx context.Context, pid string, rev int) (*domain.BundleProperty, error) {
	var bp domain.BundleProperty
	bp.PublicID = pid
	bp.RevisionNo = rev
	var labelsJSON, descJSON, constraintsJSON []byte
	var dt string
	var pkgCode *string
	var iriLocal string
	err := s.pool.QueryRow(ctx, `
		SELECT er.status, pp.datatype, er.labels, er.descriptions, pkg.code, COALESCE(e.iri_local,''), pp.constraints
		FROM entity_revision er
		JOIN entity e ON e.id = er.entity_id
		JOIN property_profile pp ON pp.entity_id = e.id
		LEFT JOIN package pkg ON pkg.id = e.package_id
		WHERE e.public_id = $1 AND er.revision_no = $2
	`, pid, rev).Scan(&bp.Status, &dt, &labelsJSON, &descJSON, &pkgCode, &iriLocal, &constraintsJSON)
	if err != nil {
		return nil, err
	}
	bp.Datatype = datatype.Type(dt)
	bp.Labels, _ = jsonToLabels(labelsJSON)
	bp.Descriptions, _ = jsonToLabels(descJSON)
	if pkgCode != nil {
		bp.PackageCode = *pkgCode
	}
	bp.IRILocal = iriLocal
	if len(constraintsJSON) > 0 {
		_ = json.Unmarshal(constraintsJSON, &bp.Constraints)
	}
	return &bp, nil
}

func (s *Store) exportClassAtRevision(ctx context.Context, cid string, rev int) (*domain.BundleClass, error) {
	var bc domain.BundleClass
	bc.PublicID = cid
	bc.RevisionNo = rev
	var labelsJSON, descJSON, docJSON []byte
	var pkgCode *string
	var iriLocal string
	err := s.pool.QueryRow(ctx, `
		SELECT er.status, er.labels, er.descriptions, pkg.code, cp.document, COALESCE(e.iri_local,'')
		FROM entity_revision er
		JOIN entity e ON e.id = er.entity_id
		JOIN class_profile cp ON cp.entity_id = e.id
		LEFT JOIN package pkg ON pkg.id = e.package_id
		WHERE e.public_id = $1 AND er.revision_no = $2
	`, cid, rev).Scan(&bc.Status, &labelsJSON, &descJSON, &pkgCode, &docJSON, &iriLocal)
	if err != nil {
		return nil, err
	}
	bc.Labels, _ = jsonToLabels(labelsJSON)
	bc.Descriptions, _ = jsonToLabels(descJSON)
	if pkgCode != nil {
		bc.PackageCode = *pkgCode
	}
	bc.IRILocal = iriLocal
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
	val, err = publicizeValue(ctx, s.pool, val)
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
		SELECT r.id, r.version, r.published_at,
			(SELECT COUNT(*) FROM release_object ro WHERE ro.release_id = r.id)
		FROM release r
		WHERE r.package_id = $1
		ORDER BY r.published_at DESC, r.version DESC
	`, packageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Release
	for rows.Next() {
		var rel domain.Release
		rel.PackageCode = packageCode
		if err := rows.Scan(&rel.ID, &rel.Version, &rel.PublishedAt, &rel.ObjectCount); err != nil {
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
		SELECT object_type, public_id, revision_no, entity_id, iri_local FROM (
			SELECT 'entity'::text AS object_type, e.public_id, e.current_revision_no AS revision_no, e.id AS entity_id, COALESCE(e.iri_local,'') AS iri_local
			FROM entity e
			WHERE e.package_id = $1
			  AND NOT EXISTS (SELECT 1 FROM property_profile pp WHERE pp.entity_id = e.id)
			  AND NOT EXISTS (SELECT 1 FROM class_profile cp WHERE cp.entity_id = e.id)
			UNION ALL
			SELECT 'property', e.public_id, e.current_revision_no, e.id, COALESCE(e.iri_local,'')
			FROM entity e
			JOIN property_profile pp ON pp.entity_id = e.id
			WHERE e.package_id = $1
			UNION ALL
			SELECT 'class', e.public_id, e.current_revision_no, e.id, COALESCE(e.iri_local,'')
			FROM entity e
			JOIN class_profile cp ON cp.entity_id = e.id
			WHERE e.package_id = $1
			UNION ALL
			SELECT 'shape', sp.code, 1, NULL::uuid, ''::text
			FROM shape_profile sp
			WHERE sp.package_id = $1
			UNION ALL
			SELECT 'statement', st.public_id, st.current_revision_no, NULL::uuid, ''::text
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
		var iriLocal string
		if err := rows.Scan(&o.ObjectType, &o.PublicID, &o.RevisionNo, &entityID, &iriLocal); err != nil {
			return nil, err
		}
		if entityID != nil {
			o.Labels, _ = s.loadLabels(ctx, `SELECT lang, text FROM entity_label WHERE entity_id = $1`, *entityID)
		}
		if o.Labels == nil {
			o.Labels = map[string]string{}
		}
		o.DisplayID = datatype.PackageDisplayID(packageCode, iriLocal, o.PublicID)
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
