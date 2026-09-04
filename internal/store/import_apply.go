package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
	"github.com/l-ra/knowledge-core/internal/pkgcompat"
	"github.com/l-ra/knowledge-core/internal/pkgversion"
)

var ErrImportDowngrade = errors.New("import revision downgrade")

func (s *Store) importEntity(ctx context.Context, tx pgx.Tx, be domain.BundleEntity, cs *changeSetTx) error {
	var entityID uuid.UUID
	var currentRev int
	err := tx.QueryRow(ctx, `SELECT id, current_revision_no FROM entity WHERE public_id = $1`, be.PublicID).Scan(&entityID, &currentRev)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.insertImportedEntity(ctx, tx, be, cs)
	}
	if err != nil {
		return err
	}
	if be.RevisionNo < currentRev {
		return fmt.Errorf("%w: entity %s bundle rev %d < current %d", ErrImportDowngrade, be.PublicID, be.RevisionNo, currentRev)
	}
	if be.RevisionNo == currentRev {
		ok, err := s.entityRevisionMatches(ctx, tx, be.PublicID, be.RevisionNo, be)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: entity %s revision %d", ErrImportCollision, be.PublicID, be.RevisionNo)
		}
		return nil
	}
	return s.applyImportedEntity(ctx, tx, entityID, be, cs)
}

func (s *Store) applyImportedEntity(ctx context.Context, tx pgx.Tx, entityID uuid.UUID, be domain.BundleEntity, cs *changeSetTx) error {
	labels := be.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	descs := be.Descriptions
	if descs == nil {
		descs = map[string]string{}
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `DELETE FROM entity_label WHERE entity_id = $1`, entityID); err != nil {
		return err
	}
	for lang, text := range labels {
		if _, err := tx.Exec(ctx, `INSERT INTO entity_label (entity_id, lang, text) VALUES ($1,$2,$3)`, entityID, lang, text); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM entity_description WHERE entity_id = $1`, entityID); err != nil {
		return err
	}
	for lang, text := range descs {
		if _, err := tx.Exec(ctx, `INSERT INTO entity_description (entity_id, lang, text) VALUES ($1,$2,$3)`, entityID, lang, text); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE entity SET status = $2, current_revision_no = $3, iri_local = $4, updated_at = $5 WHERE id = $1
	`, entityID, string(be.Status), be.RevisionNo, be.IRILocal, now); err != nil {
		return err
	}
	labelsJSON, _ := labelsToJSON(labels)
	descJSON, _ := labelsToJSON(descs)
	if _, err := tx.Exec(ctx, `
		INSERT INTO entity_revision (id, entity_id, revision_no, status, labels, descriptions, change_set_id, actor, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, datatype.NewUUID(), entityID, be.RevisionNo, string(be.Status), labelsJSON, descJSON, cs.id, csActor(cs), now); err != nil {
		return err
	}
	return cs.addItem(ctx, tx, "entity", entityID, be.PublicID, "import_upgrade", map[string]any{"revisionNo": be.RevisionNo})
}

func (s *Store) importProperty(ctx context.Context, tx pgx.Tx, bp domain.BundleProperty, cs *changeSetTx) error {
	var propertyID uuid.UUID
	var currentRev int
	err := tx.QueryRow(ctx, `
		SELECT e.id, e.current_revision_no FROM property_profile pp
		JOIN entity e ON e.id = pp.entity_id WHERE e.public_id = $1
	`, bp.PublicID).Scan(&propertyID, &currentRev)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := s.insertImportedProperty(ctx, tx, bp, cs); err != nil {
			return err
		}
		return s.maybeAutoSetInstanceOfPropertyTx(ctx, tx, bp.PublicID)
	}
	if err != nil {
		return err
	}
	if bp.RevisionNo < currentRev {
		return fmt.Errorf("%w: property %s bundle rev %d < current %d", ErrImportDowngrade, bp.PublicID, bp.RevisionNo, currentRev)
	}
	if bp.RevisionNo == currentRev {
		ok, err := s.propertyRevisionMatches(ctx, tx, bp.PublicID, bp.RevisionNo, bp)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: property %s revision %d", ErrImportCollision, bp.PublicID, bp.RevisionNo)
		}
		// Property already present (e.g. re-import): still fill empty schema-config.
		return s.maybeAutoSetInstanceOfPropertyTx(ctx, tx, bp.PublicID)
	}
	if err := s.applyImportedProperty(ctx, tx, propertyID, bp, cs); err != nil {
		return err
	}
	return s.maybeAutoSetInstanceOfPropertyTx(ctx, tx, bp.PublicID)
}

func (s *Store) applyImportedProperty(ctx context.Context, tx pgx.Tx, propertyID uuid.UUID, bp domain.BundleProperty, cs *changeSetTx) error {
	labels := bp.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	descs := bp.Descriptions
	if descs == nil {
		descs = map[string]string{}
	}
	constraintsJSON, _ := json.Marshal(bp.Constraints)
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `
		UPDATE property_profile SET datatype = $2, constraints = $3 WHERE entity_id = $1
	`, propertyID, string(bp.Datatype), constraintsJSON); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM entity_label WHERE entity_id = $1`, propertyID); err != nil {
		return err
	}
	for lang, text := range labels {
		if _, err := tx.Exec(ctx, `INSERT INTO entity_label (entity_id, lang, text) VALUES ($1,$2,$3)`, propertyID, lang, text); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM entity_description WHERE entity_id = $1`, propertyID); err != nil {
		return err
	}
	for lang, text := range descs {
		if _, err := tx.Exec(ctx, `INSERT INTO entity_description (entity_id, lang, text) VALUES ($1,$2,$3)`, propertyID, lang, text); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE entity SET status = $2, current_revision_no = $3, iri_local = $4, updated_at = $5 WHERE id = $1
	`, propertyID, string(bp.Status), bp.RevisionNo, bp.IRILocal, now); err != nil {
		return err
	}
	labelsJSON, _ := labelsToJSON(labels)
	descJSON, _ := labelsToJSON(descs)
	if _, err := tx.Exec(ctx, `
		INSERT INTO entity_revision (id, entity_id, revision_no, status, labels, descriptions, change_set_id, actor, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, datatype.NewUUID(), propertyID, bp.RevisionNo, string(bp.Status), labelsJSON, descJSON, cs.id, csActor(cs), now); err != nil {
		return err
	}
	return cs.addItem(ctx, tx, "property", propertyID, bp.PublicID, "import_upgrade", map[string]any{"revisionNo": bp.RevisionNo})
}

func (s *Store) importClass(ctx context.Context, tx pgx.Tx, bc domain.BundleClass, cs *changeSetTx) error {
	var classID uuid.UUID
	var currentRev int
	err := tx.QueryRow(ctx, `
		SELECT e.id, e.current_revision_no FROM class_profile cp
		JOIN entity e ON e.id = cp.entity_id WHERE e.public_id = $1
	`, bc.PublicID).Scan(&classID, &currentRev)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.insertImportedClass(ctx, tx, bc, cs)
	}
	if err != nil {
		return err
	}
	if bc.RevisionNo < currentRev {
		return fmt.Errorf("%w: class %s bundle rev %d < current %d", ErrImportDowngrade, bc.PublicID, bc.RevisionNo, currentRev)
	}
	if bc.RevisionNo == currentRev {
		ok, err := s.classRevisionMatches(ctx, tx, bc.PublicID, bc.RevisionNo, bc)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: class %s revision %d", ErrImportCollision, bc.PublicID, bc.RevisionNo)
		}
		return nil
	}
	return s.applyImportedClass(ctx, tx, classID, bc, cs)
}

func (s *Store) applyImportedClass(ctx context.Context, tx pgx.Tx, classID uuid.UUID, bc domain.BundleClass, cs *changeSetTx) error {
	labels := bc.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	descs := bc.Descriptions
	if descs == nil {
		descs = map[string]string{}
	}
	doc := domain.ClassDocument{SubClassOf: bc.SubClassOf}
	docJSON, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `UPDATE class_profile SET document = $2 WHERE entity_id = $1`, classID, docJSON); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM entity_label WHERE entity_id = $1`, classID); err != nil {
		return err
	}
	for lang, text := range labels {
		if _, err := tx.Exec(ctx, `INSERT INTO entity_label (entity_id, lang, text) VALUES ($1,$2,$3)`, classID, lang, text); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM entity_description WHERE entity_id = $1`, classID); err != nil {
		return err
	}
	for lang, text := range descs {
		if _, err := tx.Exec(ctx, `INSERT INTO entity_description (entity_id, lang, text) VALUES ($1,$2,$3)`, classID, lang, text); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE entity SET status = $2, current_revision_no = $3, iri_local = $4, updated_at = $5 WHERE id = $1
	`, classID, string(bc.Status), bc.RevisionNo, bc.IRILocal, now); err != nil {
		return err
	}
	labelsJSON, _ := labelsToJSON(labels)
	descJSON, _ := labelsToJSON(descs)
	if _, err := tx.Exec(ctx, `
		INSERT INTO entity_revision (id, entity_id, revision_no, status, labels, descriptions, change_set_id, actor, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, datatype.NewUUID(), classID, bc.RevisionNo, string(bc.Status), labelsJSON, descJSON, cs.id, csActor(cs), now); err != nil {
		return err
	}
	return cs.addItem(ctx, tx, "class", classID, bc.PublicID, "import_upgrade", docJSON)
}

func (s *Store) importStatement(ctx context.Context, tx pgx.Tx, bs domain.BundleStatement, cs *changeSetTx) error {
	var statementID uuid.UUID
	var currentRev int
	err := tx.QueryRow(ctx, `SELECT id, current_revision_no FROM statement WHERE public_id = $1`, bs.PublicID).Scan(&statementID, &currentRev)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.insertImportedStatement(ctx, tx, bs, cs)
	}
	if err != nil {
		return err
	}
	if bs.RevisionNo < currentRev {
		return fmt.Errorf("%w: statement %s bundle rev %d < current %d", ErrImportDowngrade, bs.PublicID, bs.RevisionNo, currentRev)
	}
	if bs.RevisionNo == currentRev {
		ok, err := s.statementRevisionMatches(ctx, tx, bs.PublicID, bs.RevisionNo, bs)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: statement %s revision %d", ErrImportCollision, bs.PublicID, bs.RevisionNo)
		}
		return nil
	}
	return s.applyImportedStatement(ctx, tx, statementID, bs, cs)
}

func (s *Store) applyImportedStatement(ctx context.Context, tx pgx.Tx, statementID uuid.UUID, bs domain.BundleStatement, cs *changeSetTx) error {
	var propertyID uuid.UUID
	var dt string
	err := tx.QueryRow(ctx, `
		SELECT e.id, pp.datatype FROM property_profile pp
		JOIN entity e ON e.id = pp.entity_id WHERE e.public_id = $1
	`, bs.Property).Scan(&propertyID, &dt)
	if err != nil {
		return fmt.Errorf("property %q: %w", bs.Property, err)
	}
	_, sv, err := s.resolveAndEncodeValue(ctx, tx, datatype.Type(dt), bs.Value)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `
		UPDATE statement SET
			status = $2, value_type = $3, value_bool = $4, value_int64 = $5, value_numeric = $6,
			value_date = $7, value_timestamptz = $8, value_text = $9, value_entity_id = $10,
			value_json = $11, current_revision_no = $12, updated_at = $13
		WHERE id = $1
	`, statementID, string(bs.Status), sv.Type, sv.Bool, sv.Int64, sv.Numeric, sv.Date, sv.Timestamptz,
		sv.Text, sv.EntityID, sv.JSON, bs.RevisionNo, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE statement_current SET
			value_type = $2, value_bool = $3, value_int64 = $4, value_numeric = $5,
			value_date = $6, value_timestamptz = $7, value_text = $8, value_entity_id = $9,
			value_json = $10, updated_at = $11
		WHERE statement_id = $1
	`, statementID, sv.Type, sv.Bool, sv.Int64, sv.Numeric, sv.Date, sv.Timestamptz,
		sv.Text, sv.EntityID, sv.JSON, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO statement_revision (
			id, statement_id, revision_no, status, value_type,
			value_bool, value_int64, value_numeric, value_date, value_timestamptz,
			value_text, value_entity_id, value_json,
			change_set_id, actor, created_at
		) VALUES (
			$1,$2,$3,$4,$5,
			$6,$7,$8,$9,$10,
			$11,$12,$13,$14,$15,$16
		)
	`, datatype.NewUUID(), statementID, bs.RevisionNo, string(bs.Status), sv.Type,
		sv.Bool, sv.Int64, sv.Numeric, sv.Date, sv.Timestamptz,
		sv.Text, sv.EntityID, sv.JSON, cs.id, csActor(cs), now); err != nil {
		return err
	}
	if err := s.snapshotStatementProvenance(ctx, tx, statementID, bs.RevisionNo); err != nil {
		return err
	}
	return cs.addItem(ctx, tx, "statement", statementID, bs.PublicID, "import_upgrade", map[string]any{"revisionNo": bs.RevisionNo})
}

func (s *Store) importShape(ctx context.Context, tx pgx.Tx, bs domain.BundleShape) error {
	if bs.Code == "" || bs.ClassID == "" {
		return fmt.Errorf("shape code and classId required")
	}
	var shapeID uuid.UUID
	var docJSON []byte
	var classPID string
	err := tx.QueryRow(ctx, `
		SELECT sp.id, sp.document, e.public_id
		FROM shape_profile sp
		JOIN entity e ON e.id = sp.class_id
		WHERE sp.code = $1
	`, bs.Code).Scan(&shapeID, &docJSON, &classPID)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.insertImportedShape(ctx, tx, bs)
	}
	if err != nil {
		return err
	}
	var existing domain.ShapeDocument
	_ = json.Unmarshal(docJSON, &existing)
	want, _ := json.Marshal(bs.Document)
	have, _ := json.Marshal(existing)
	if classPID == bs.ClassID && string(want) == string(have) {
		return nil
	}
	// Content change: apply (compat gate runs before import loop).
	var classUUID string
	err = tx.QueryRow(ctx, `
		SELECT e.id FROM class_profile cp JOIN entity e ON e.id = cp.entity_id
		WHERE e.public_id = $1 AND e.status <> 'deleted'
	`, bs.ClassID).Scan(&classUUID)
	if err != nil {
		return fmt.Errorf("shape class %s: %w", bs.ClassID, err)
	}
	_, err = tx.Exec(ctx, `
		UPDATE shape_profile SET class_id = $2, document = $3, updated_at = now() WHERE id = $1
	`, shapeID, classUUID, want)
	return err
}

func (s *Store) insertImportedShape(ctx context.Context, tx pgx.Tx, bs domain.BundleShape) error {
	var classUUID string
	err := tx.QueryRow(ctx, `
		SELECT e.id FROM class_profile cp JOIN entity e ON e.id = cp.entity_id
		WHERE e.public_id = $1 AND e.status <> 'deleted'
	`, bs.ClassID).Scan(&classUUID)
	if err != nil {
		return fmt.Errorf("shape class %s: %w", bs.ClassID, err)
	}
	var pkgID any
	if bs.PackageCode != "" {
		id, err := s.resolvePackageIDRequired(ctx, tx, bs.PackageCode)
		if err != nil {
			return err
		}
		pkgID = id
	}
	docJSON, err := json.Marshal(bs.Document)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO shape_profile (id, code, class_id, document, package_id, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,now(),now())
	`, datatype.NewUUID(), bs.Code, classUUID, docJSON, pkgID)
	return err
}

func latestSemVer(versions []string) (string, error) {
	if len(versions) == 0 {
		return "", fmt.Errorf("no versions")
	}
	best := versions[0]
	bm, bn, bp, err := pkgversion.ParseVersion(best)
	if err != nil {
		return "", err
	}
	for _, v := range versions[1:] {
		m, n, p, err := pkgversion.ParseVersion(v)
		if err != nil {
			return "", err
		}
		if m > bm || (m == bm && n > bn) || (m == bm && n == bn && p > bp) {
			best, bm, bn, bp = v, m, n, p
		}
	}
	return best, nil
}

func (s *Store) packageHasLiveObjects(ctx context.Context, packageCode string) (bool, error) {
	var packageID uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT id FROM package WHERE code = $1`, packageCode).Scan(&packageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var exists bool
	err = s.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM entity WHERE package_id = $1)
		    OR EXISTS(SELECT 1 FROM statement WHERE package_id = $1)
		    OR EXISTS(SELECT 1 FROM shape_profile WHERE package_id = $1)
	`, packageID).Scan(&exists)
	return exists, err
}

func (s *Store) livePackageSnapshot(ctx context.Context, packageCode string) (pkgcompat.Snapshot, error) {
	bundle, err := s.exportLivePackageBundle(ctx, packageCode)
	if err != nil {
		return pkgcompat.Snapshot{}, err
	}
	return pkgcompat.SnapshotFromBundle(*bundle), nil
}

func (s *Store) exportLivePackageBundle(ctx context.Context, packageCode string) (*domain.Bundle, error) {
	var packageID uuid.UUID
	var iriBase, lifecycle string
	var labelsJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, COALESCE(iri_base,''), lifecycle, labels FROM package WHERE code = $1
	`, packageCode).Scan(&packageID, &iriBase, &lifecycle, &labelsJSON)
	if err != nil {
		return nil, err
	}
	labels, _ := jsonToLabels(labelsJSON)
	b := &domain.Bundle{
		Manifest: domain.BundleManifest{
			FormatVersion: 1,
			Package:       packageCode,
			Version:       "0.0.0-live",
			IRIBase:       iriBase,
			Lifecycle:     domain.PackageLifecycle(lifecycle),
			Labels:        labels,
		},
	}

	propRows, err := s.pool.Query(ctx, `
		SELECT e.public_id, e.current_revision_no FROM entity e
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
		bp, err := s.exportPropertyAtRevision(ctx, pid, rev)
		if err != nil {
			propRows.Close()
			return nil, err
		}
		b.Properties = append(b.Properties, *bp)
	}
	propRows.Close()
	if err := propRows.Err(); err != nil {
		return nil, err
	}

	classRows, err := s.pool.Query(ctx, `
		SELECT e.public_id, e.current_revision_no FROM entity e
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
		bc, err := s.exportClassAtRevision(ctx, cid, rev)
		if err != nil {
			classRows.Close()
			return nil, err
		}
		b.Classes = append(b.Classes, *bc)
	}
	classRows.Close()
	if err := classRows.Err(); err != nil {
		return nil, err
	}

	entRows, err := s.pool.Query(ctx, `
		SELECT e.public_id, e.current_revision_no FROM entity e
		WHERE e.package_id = $1
		  AND NOT EXISTS (SELECT 1 FROM property_profile pp WHERE pp.entity_id = e.id)
		  AND NOT EXISTS (SELECT 1 FROM class_profile cp WHERE cp.entity_id = e.id)
	`, packageID)
	if err != nil {
		return nil, err
	}
	for entRows.Next() {
		var qid string
		var rev int
		if err := entRows.Scan(&qid, &rev); err != nil {
			entRows.Close()
			return nil, err
		}
		be, err := s.exportEntityAtRevision(ctx, qid, rev)
		if err != nil {
			entRows.Close()
			return nil, err
		}
		b.Entities = append(b.Entities, *be)
	}
	entRows.Close()
	if err := entRows.Err(); err != nil {
		return nil, err
	}

	stmtRows, err := s.pool.Query(ctx, `
		SELECT public_id, current_revision_no FROM statement WHERE package_id = $1
	`, packageID)
	if err != nil {
		return nil, err
	}
	for stmtRows.Next() {
		var sid string
		var rev int
		if err := stmtRows.Scan(&sid, &rev); err != nil {
			stmtRows.Close()
			return nil, err
		}
		st, err := s.exportStatementAtRevision(ctx, sid, rev)
		if err != nil {
			stmtRows.Close()
			return nil, err
		}
		b.Statements = append(b.Statements, *st)
	}
	stmtRows.Close()
	if err := stmtRows.Err(); err != nil {
		return nil, err
	}

	shapeRows, err := s.pool.Query(ctx, `
		SELECT sp.code, e.public_id, sp.document
		FROM shape_profile sp
		JOIN entity e ON e.id = sp.class_id
		WHERE sp.package_id = $1
	`, packageID)
	if err != nil {
		return nil, err
	}
	for shapeRows.Next() {
		var code, classID string
		var docJSON []byte
		if err := shapeRows.Scan(&code, &classID, &docJSON); err != nil {
			shapeRows.Close()
			return nil, err
		}
		var doc domain.ShapeDocument
		_ = json.Unmarshal(docJSON, &doc)
		b.Shapes = append(b.Shapes, domain.BundleShape{
			Code: code, PackageCode: packageCode, ClassID: classID, Document: doc, RevisionNo: 1,
		})
	}
	shapeRows.Close()
	return b, shapeRows.Err()
}

func (s *Store) baselineSnapshotForUpgrade(ctx context.Context, packageCode string) (pkgcompat.Snapshot, bool, error) {
	ver, ok, err := s.latestPackageReleaseVersion(ctx, packageCode)
	if err != nil {
		return pkgcompat.Snapshot{}, false, err
	}
	if ok {
		bundle, err := s.ExportReleaseBundle(ctx, packageCode, ver)
		if err != nil {
			return pkgcompat.Snapshot{}, false, err
		}
		snap := pkgcompat.FilterByPackage(pkgcompat.SnapshotFromBundle(*bundle), packageCode)
		return snap, true, nil
	}
	has, err := s.packageHasLiveObjects(ctx, packageCode)
	if err != nil || !has {
		return pkgcompat.Snapshot{}, false, err
	}
	snap, err := s.livePackageSnapshot(ctx, packageCode)
	return snap, err == nil, err
}

func (s *Store) latestPackageReleaseVersion(ctx context.Context, packageCode string) (string, bool, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.version FROM release r
		JOIN package p ON p.id = r.package_id
		WHERE p.code = $1
	`, packageCode)
	if err != nil {
		return "", false, err
	}
	defer rows.Close()
	var versions []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return "", false, err
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		return "", false, err
	}
	if len(versions) == 0 {
		return "", false, nil
	}
	best, err := latestSemVer(versions)
	return best, true, err
}

func (s *Store) latestReleaseVersionsByPackageID(ctx context.Context, packageIDs []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(packageIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT package_id, version FROM release WHERE package_id = ANY($1)
	`, packageIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byPkg := map[uuid.UUID][]string{}
	for rows.Next() {
		var id uuid.UUID
		var v string
		if err := rows.Scan(&id, &v); err != nil {
			return nil, err
		}
		byPkg[id] = append(byPkg[id], v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for id, versions := range byPkg {
		best, err := latestSemVer(versions)
		if err != nil {
			return nil, err
		}
		out[id] = best
	}
	return out, nil
}

// packageModifiedAfterRelease reports whether live package objects differ from the pinned release
// (new/missing objects or revision mismatch). Shapes are compared by presence only (rev is always 1).
func (s *Store) packageModifiedAfterRelease(ctx context.Context, packageID uuid.UUID, version string) (bool, error) {
	var releaseID uuid.UUID
	err := s.pool.QueryRow(ctx, `
		SELECT id FROM release WHERE package_id = $1 AND version = $2
	`, packageID, version).Scan(&releaseID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	relRows, err := s.pool.Query(ctx, `
		SELECT object_type, object_public_id, revision_no FROM release_object WHERE release_id = $1
	`, releaseID)
	if err != nil {
		return false, err
	}
	pinned := map[string]int{}
	for relRows.Next() {
		var typ, pid string
		var rev int
		if err := relRows.Scan(&typ, &pid, &rev); err != nil {
			relRows.Close()
			return false, err
		}
		pinned[typ+":"+pid] = rev
	}
	relRows.Close()
	if err := relRows.Err(); err != nil {
		return false, err
	}

	live, err := s.listLivePackageObjects(ctx, packageID)
	if err != nil {
		return false, err
	}
	seen := map[string]struct{}{}
	for _, obj := range live {
		key := obj.ObjectType + ":" + obj.ObjectPublicID
		seen[key] = struct{}{}
		pinnedRev, ok := pinned[key]
		if !ok {
			return true, nil // new object since release
		}
		if obj.ObjectType != "shape" && obj.RevisionNo != pinnedRev {
			return true, nil
		}
	}
	for key := range pinned {
		if _, ok := seen[key]; !ok {
			return true, nil // removed / moved out of package
		}
	}
	return false, nil
}

func (s *Store) listLivePackageObjects(ctx context.Context, packageID uuid.UUID) ([]domain.ReleaseObject, error) {
	var objects []domain.ReleaseObject

	entRows, err := s.pool.Query(ctx, `
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
	if err := entRows.Err(); err != nil {
		return nil, err
	}

	propRows, err := s.pool.Query(ctx, `
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
	if err := propRows.Err(); err != nil {
		return nil, err
	}

	classRows, err := s.pool.Query(ctx, `
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
	if err := classRows.Err(); err != nil {
		return nil, err
	}

	stmtRows, err := s.pool.Query(ctx, `
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
	if err := stmtRows.Err(); err != nil {
		return nil, err
	}

	shapeRows, err := s.pool.Query(ctx, `
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
	return objects, shapeRows.Err()
}

func (s *Store) ensureBackwardCompatible(ctx string, old, neu pkgcompat.Snapshot) error {
	findings := pkgcompat.Diff(old, neu)
	return pkgcompat.NewBreakingError(ctx, findings)
}
