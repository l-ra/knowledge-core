package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
	"github.com/l-ra/knowledge-core/internal/pkgversion"
	"github.com/shopspring/decimal"
)

var ErrImportCollision = errors.New("import identity collision")

func (s *Store) ImportReleaseBundle(ctx context.Context, meta domain.WriteMeta, bundle domain.Bundle) (*domain.WriteResult[domain.Release], error) {
	if err := validateImportBundle(&bundle); err != nil {
		return nil, err
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
		var rel domain.Release
		if err := json.Unmarshal(hit.responseBody, &rel); err != nil {
			return nil, err
		}
		return &domain.WriteResult[domain.Release]{Value: rel, Replay: true, ResponseRaw: hit.responseBody}, nil
	}

	cs, err := s.beginChangeSetTx(ctx, tx, meta)
	if err != nil {
		return nil, err
	}

	releaseManifests := orderReleaseManifests(collectReleaseManifests(&bundle))
	for _, rm := range releaseManifests {
		if err := s.ensureImportPackage(ctx, tx, rm.Package); err != nil {
			return nil, err
		}
	}

	for _, ref := range bundle.References {
		if err := s.importReference(ctx, tx, ref, cs); err != nil {
			return nil, err
		}
	}
	for _, p := range bundle.Properties {
		if err := s.importProperty(ctx, tx, p, cs); err != nil {
			return nil, err
		}
	}
	for _, e := range bundle.Entities {
		if err := s.importEntity(ctx, tx, e, cs); err != nil {
			return nil, err
		}
	}
	for _, st := range bundle.Statements {
		if err := s.importStatement(ctx, tx, st, cs); err != nil {
			return nil, err
		}
	}

	var mainRelease domain.Release
	for _, rm := range releaseManifests {
		rel, err := s.importReleaseRecord(ctx, tx, rm, cs)
		if err != nil {
			return nil, err
		}
		if rm.Package == bundle.Manifest.Package && rm.Version == bundle.Manifest.Version {
			mainRelease = *rel
		}
	}

	if err := s.finalizeChangeSet(ctx, tx, cs, mainRelease); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Release]{Value: mainRelease, ChangeSet: s.changeSetDomain(cs, meta)}, nil
}

func validateImportBundle(bundle *domain.Bundle) error {
	if bundle.Manifest.FormatVersion != 1 {
		return fmt.Errorf("unsupported bundle format version %d", bundle.Manifest.FormatVersion)
	}
	if bundle.Manifest.Package == "" || bundle.Manifest.Version == "" {
		return fmt.Errorf("manifest package and version required")
	}
	if _, _, _, err := pkgversion.ParseVersion(bundle.Manifest.Version); err != nil {
		return fmt.Errorf("invalid version: %w", err)
	}
	for _, rm := range collectReleaseManifests(bundle) {
		if _, _, _, err := pkgversion.ParseVersion(rm.Version); err != nil {
			return fmt.Errorf("invalid release version %s@%s: %w", rm.Package, rm.Version, err)
		}
	}
	return nil
}

func collectReleaseManifests(bundle *domain.Bundle) []domain.BundleManifest {
	if len(bundle.Releases) > 0 {
		return bundle.Releases
	}
	return []domain.BundleManifest{bundle.Manifest}
}

func orderReleaseManifests(manifests []domain.BundleManifest) []domain.BundleManifest {
	byKey := make(map[string]domain.BundleManifest, len(manifests))
	for _, m := range manifests {
		byKey[m.Package+"@"+m.Version] = m
	}
	seen := map[string]struct{}{}
	var ordered []domain.BundleManifest
	var visit func(domain.BundleManifest)
	visit = func(m domain.BundleManifest) {
		key := m.Package + "@" + m.Version
		if _, ok := seen[key]; ok {
			return
		}
		for _, d := range m.Dependencies {
			depKey := d.DependencyCode + "@" + d.DependencyVersion
			if dep, ok := byKey[depKey]; ok {
				visit(dep)
			}
		}
		seen[key] = struct{}{}
		ordered = append(ordered, m)
	}
	for _, m := range manifests {
		visit(m)
	}
	return ordered
}

func (s *Store) ensureImportPackage(ctx context.Context, tx pgx.Tx, code string) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM package WHERE code = $1)`, code).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	id := datatype.NewUUID()
	now := time.Now().UTC()
	labelsJSON, _ := json.Marshal(map[string]string{"en": code})
	_, err := tx.Exec(ctx, `
		INSERT INTO package (id, code, lifecycle, labels, created_at, updated_at)
		VALUES ($1,$2,'released',$3,$4,$4)
	`, id, code, labelsJSON, now)
	return err
}

func (s *Store) reservePublicID(ctx context.Context, tx pgx.Tx, kind, prefix, publicID string) error {
	n, err := parsePublicIDNumber(prefix, publicID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE id_counter SET last_value = GREATEST(last_value, $2) WHERE kind = $1
	`, kind, n)
	return err
}

func parsePublicIDNumber(prefix, publicID string) (int64, error) {
	switch prefix {
	case "Q":
		return datatype.ParsePublicEntityID(publicID)
	case "P":
		return datatype.ParsePublicPropertyID(publicID)
	case "S":
		return datatype.ParsePublicStatementID(publicID)
	case "R":
		if len(publicID) < 2 || publicID[0] != 'R' {
			return 0, fmt.Errorf("invalid reference id %q", publicID)
		}
		var n int64
		if _, err := fmt.Sscanf(publicID[1:], "%d", &n); err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid reference id %q", publicID)
		}
		return n, nil
	default:
		return 0, fmt.Errorf("unknown public id prefix %q", prefix)
	}
}

func (s *Store) resolvePackageIDRequired(ctx context.Context, tx pgx.Tx, code string) (uuid.UUID, error) {
	if code == "" {
		return uuid.Nil, fmt.Errorf("package code required for import")
	}
	id, err := s.resolvePackageID(ctx, tx, code)
	if err != nil {
		return uuid.Nil, err
	}
	if id == nil {
		return uuid.Nil, fmt.Errorf("package %q not found", code)
	}
	return *id, nil
}

func (s *Store) importEntity(ctx context.Context, tx pgx.Tx, be domain.BundleEntity, cs *changeSetTx) error {
	var entityID uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM entity WHERE public_id = $1`, be.PublicID).Scan(&entityID)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.insertImportedEntity(ctx, tx, be, cs)
	}
	if err != nil {
		return err
	}
	ok, err := s.entityRevisionMatches(ctx, tx, be.PublicID, be.RevisionNo, be)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: entity %s revision %d", ErrImportCollision, be.PublicID, be.RevisionNo)
	}
	return nil
}

func (s *Store) insertImportedEntity(ctx context.Context, tx pgx.Tx, be domain.BundleEntity, cs *changeSetTx) error {
	if err := s.reservePublicID(ctx, tx, "entity", "Q", be.PublicID); err != nil {
		return err
	}
	pkgID, err := s.resolvePackageIDRequired(ctx, tx, be.PackageCode)
	if err != nil {
		return err
	}
	labels := be.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	descs := be.Descriptions
	if descs == nil {
		descs = map[string]string{}
	}
	id := datatype.NewUUID()
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		INSERT INTO entity (id, public_id, status, current_revision_no, package_id, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$6)
	`, id, be.PublicID, string(be.Status), be.RevisionNo, pkgID, now)
	if err != nil {
		return err
	}
	for lang, text := range labels {
		if _, err := tx.Exec(ctx, `INSERT INTO entity_label (entity_id, lang, text) VALUES ($1,$2,$3)`, id, lang, text); err != nil {
			return err
		}
	}
	for lang, text := range descs {
		if _, err := tx.Exec(ctx, `INSERT INTO entity_description (entity_id, lang, text) VALUES ($1,$2,$3)`, id, lang, text); err != nil {
			return err
		}
	}
	labelsJSON, _ := labelsToJSON(labels)
	descJSON, _ := labelsToJSON(descs)
	_, err = tx.Exec(ctx, `
		INSERT INTO entity_revision (id, entity_id, revision_no, status, labels, descriptions, change_set_id, actor, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, datatype.NewUUID(), id, be.RevisionNo, string(be.Status), labelsJSON, descJSON, cs.id, csActor(cs), now)
	if err != nil {
		return err
	}
	return cs.addItem(ctx, tx, "entity", id, be.PublicID, "import", nil)
}

func (s *Store) entityRevisionMatches(ctx context.Context, tx pgx.Tx, publicID string, rev int, be domain.BundleEntity) (bool, error) {
	var status string
	var labelsJSON, descJSON []byte
	err := tx.QueryRow(ctx, `
		SELECT er.status, er.labels, er.descriptions
		FROM entity_revision er
		JOIN entity e ON e.id = er.entity_id
		WHERE e.public_id = $1 AND er.revision_no = $2
	`, publicID, rev).Scan(&status, &labelsJSON, &descJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	labels, _ := jsonToLabels(labelsJSON)
	descs, _ := jsonToLabels(descJSON)
	return status == string(be.Status) && mapsEqual(labels, be.Labels) && mapsEqual(descs, be.Descriptions), nil
}

func (s *Store) importProperty(ctx context.Context, tx pgx.Tx, bp domain.BundleProperty, cs *changeSetTx) error {
	var propertyID uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM property_definition WHERE public_id = $1`, bp.PublicID).Scan(&propertyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.insertImportedProperty(ctx, tx, bp, cs)
	}
	if err != nil {
		return err
	}
	ok, err := s.propertyRevisionMatches(ctx, tx, bp.PublicID, bp.RevisionNo, bp)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: property %s revision %d", ErrImportCollision, bp.PublicID, bp.RevisionNo)
	}
	return nil
}

func (s *Store) insertImportedProperty(ctx context.Context, tx pgx.Tx, bp domain.BundleProperty, cs *changeSetTx) error {
	if err := s.reservePublicID(ctx, tx, "property", "P", bp.PublicID); err != nil {
		return err
	}
	pkgID, err := s.resolvePackageIDRequired(ctx, tx, bp.PackageCode)
	if err != nil {
		return err
	}
	labels := bp.Labels
	if labels == nil {
		labels = map[string]string{}
	}
	descs := bp.Descriptions
	if descs == nil {
		descs = map[string]string{}
	}
	id := datatype.NewUUID()
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		INSERT INTO property_definition (id, public_id, datatype, status, current_revision_no, package_id, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$7)
	`, id, bp.PublicID, string(bp.Datatype), string(bp.Status), bp.RevisionNo, pkgID, now)
	if err != nil {
		return err
	}
	for lang, text := range labels {
		if _, err := tx.Exec(ctx, `INSERT INTO property_label (property_id, lang, text) VALUES ($1,$2,$3)`, id, lang, text); err != nil {
			return err
		}
	}
	for lang, text := range descs {
		if _, err := tx.Exec(ctx, `INSERT INTO property_description (property_id, lang, text) VALUES ($1,$2,$3)`, id, lang, text); err != nil {
			return err
		}
	}
	labelsJSON, _ := labelsToJSON(labels)
	descJSON, _ := labelsToJSON(descs)
	_, err = tx.Exec(ctx, `
		INSERT INTO property_revision (id, property_id, revision_no, status, datatype, labels, descriptions, change_set_id, actor, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
	`, datatype.NewUUID(), id, bp.RevisionNo, string(bp.Status), string(bp.Datatype), labelsJSON, descJSON, cs.id, csActor(cs), now)
	if err != nil {
		return err
	}
	return cs.addItem(ctx, tx, "property", id, bp.PublicID, "import", nil)
}

func (s *Store) propertyRevisionMatches(ctx context.Context, tx pgx.Tx, publicID string, rev int, bp domain.BundleProperty) (bool, error) {
	var status, dt string
	var labelsJSON, descJSON []byte
	err := tx.QueryRow(ctx, `
		SELECT pr.status, pr.datatype, pr.labels, pr.descriptions
		FROM property_revision pr
		JOIN property_definition p ON p.id = pr.property_id
		WHERE p.public_id = $1 AND pr.revision_no = $2
	`, publicID, rev).Scan(&status, &dt, &labelsJSON, &descJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	labels, _ := jsonToLabels(labelsJSON)
	descs, _ := jsonToLabels(descJSON)
	return status == string(bp.Status) && dt == string(bp.Datatype) &&
		mapsEqual(labels, bp.Labels) && mapsEqual(descs, bp.Descriptions), nil
}

func (s *Store) importReference(ctx context.Context, tx pgx.Tx, br domain.BundleReference, cs *changeSetTx) error {
	var refID uuid.UUID
	var fieldsJSON []byte
	err := tx.QueryRow(ctx, `SELECT id, fields FROM reference WHERE public_id = $1`, br.PublicID).Scan(&refID, &fieldsJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.insertImportedReference(ctx, tx, br, cs)
	}
	if err != nil {
		return err
	}
	var existing map[string]any
	_ = json.Unmarshal(fieldsJSON, &existing)
	if !reflect.DeepEqual(normalizeMap(existing), normalizeMap(br.Fields)) {
		return fmt.Errorf("%w: reference %s", ErrImportCollision, br.PublicID)
	}
	return nil
}

func (s *Store) insertImportedReference(ctx context.Context, tx pgx.Tx, br domain.BundleReference, cs *changeSetTx) error {
	if err := s.reservePublicID(ctx, tx, "reference", "R", br.PublicID); err != nil {
		return err
	}
	fields := br.Fields
	if fields == nil {
		fields = map[string]any{}
	}
	fieldsJSON, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	id := datatype.NewUUID()
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `INSERT INTO reference (id, public_id, fields, created_at) VALUES ($1,$2,$3,$4)`, id, br.PublicID, fieldsJSON, now)
	if err != nil {
		return err
	}
	return cs.addItem(ctx, tx, "reference", id, br.PublicID, "import", nil)
}

func (s *Store) importStatement(ctx context.Context, tx pgx.Tx, bs domain.BundleStatement, cs *changeSetTx) error {
	var statementID uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM statement WHERE public_id = $1`, bs.PublicID).Scan(&statementID)
	if errors.Is(err, pgx.ErrNoRows) {
		return s.insertImportedStatement(ctx, tx, bs, cs)
	}
	if err != nil {
		return err
	}
	ok, err := s.statementRevisionMatches(ctx, tx, bs.PublicID, bs.RevisionNo, bs)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: statement %s revision %d", ErrImportCollision, bs.PublicID, bs.RevisionNo)
	}
	return nil
}

func (s *Store) insertImportedStatement(ctx context.Context, tx pgx.Tx, bs domain.BundleStatement, cs *changeSetTx) error {
	if err := s.reservePublicID(ctx, tx, "statement", "S", bs.PublicID); err != nil {
		return err
	}
	pkgID, err := s.resolvePackageIDRequired(ctx, tx, bs.PackageCode)
	if err != nil {
		return err
	}

	var subjectID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM entity WHERE public_id = $1`, bs.Subject).Scan(&subjectID)
	if err != nil {
		return fmt.Errorf("subject %q: %w", bs.Subject, err)
	}
	var propertyID uuid.UUID
	var dt string
	err = tx.QueryRow(ctx, `SELECT id, datatype FROM property_definition WHERE public_id = $1`, bs.Property).Scan(&propertyID, &dt)
	if err != nil {
		return fmt.Errorf("property %q: %w", bs.Property, err)
	}

	val, sv, err := s.resolveAndEncodeValue(ctx, tx, datatype.Type(dt), bs.Value)
	if err != nil {
		return err
	}

	id := datatype.NewUUID()
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		INSERT INTO statement (
			id, public_id, subject_id, property_id, status, value_type,
			value_bool, value_int64, value_numeric, value_date, value_timestamptz,
			value_text, value_entity_id, value_json,
			current_revision_no, package_id, created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,
			$7,$8,$9,$10,$11,
			$12,$13,$14,$15,$16,$17,$17
		)
	`, id, bs.PublicID, subjectID, propertyID, string(bs.Status), sv.Type,
		sv.Bool, sv.Int64, sv.Numeric, sv.Date, sv.Timestamptz,
		sv.Text, sv.EntityID, sv.JSON, bs.RevisionNo, pkgID, now)
	if err != nil {
		return err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO statement_current (
			statement_id, public_id, subject_id, property_id, value_type,
			value_bool, value_int64, value_numeric, value_date, value_timestamptz,
			value_text, value_entity_id, value_json, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,
			$6,$7,$8,$9,$10,
			$11,$12,$13,$14
		)
	`, id, bs.PublicID, subjectID, propertyID, sv.Type,
		sv.Bool, sv.Int64, sv.Numeric, sv.Date, sv.Timestamptz,
		sv.Text, sv.EntityID, sv.JSON, now)
	if err != nil {
		return err
	}

	if len(bs.Qualifiers) > 0 {
		if err := s.replaceStatementQualifiers(ctx, tx, id, bs.Qualifiers); err != nil {
			return err
		}
	}
	if len(bs.ReferenceIDs) > 0 {
		if err := s.replaceStatementReferences(ctx, tx, id, bs.ReferenceIDs); err != nil {
			return err
		}
	}

	_, err = tx.Exec(ctx, `
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
	`, datatype.NewUUID(), id, bs.RevisionNo, string(bs.Status), sv.Type,
		sv.Bool, sv.Int64, sv.Numeric, sv.Date, sv.Timestamptz,
		sv.Text, sv.EntityID, sv.JSON, cs.id, csActor(cs), now)
	if err != nil {
		return err
	}
	if err := s.snapshotStatementProvenance(ctx, tx, id, bs.RevisionNo); err != nil {
		return err
	}
	if err := cs.addItem(ctx, tx, "statement", id, bs.PublicID, "import", nil); err != nil {
		return err
	}
	_ = val
	return nil
}

func (s *Store) statementRevisionMatches(ctx context.Context, tx pgx.Tx, publicID string, rev int, bs domain.BundleStatement) (bool, error) {
	var statementID uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM statement WHERE public_id = $1`, publicID).Scan(&statementID)
	if err != nil {
		return false, err
	}
	row := tx.QueryRow(ctx, `
		SELECT sr.status, e.public_id, p.public_id,
			sr.value_type, sr.value_bool, sr.value_int64, sr.value_numeric, sr.value_date, sr.value_timestamptz,
			sr.value_text, sr.value_entity_id, sr.value_json
		FROM statement_revision sr
		JOIN statement st ON st.id = sr.statement_id
		JOIN entity e ON e.id = st.subject_id
		JOIN property_definition p ON p.id = st.property_id
		WHERE st.public_id = $1 AND sr.revision_no = $2
	`, publicID, rev)
	var status, subject, property string
	var sv storedValue
	var numeric *decimal.Decimal
	err = row.Scan(&status, &subject, &property,
		&sv.Type, &sv.Bool, &sv.Int64, &numeric, &sv.Date, &sv.Timestamptz,
		&sv.Text, &sv.EntityID, &sv.JSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	sv.Numeric = numeric
	val, err := decodeValue(sv)
	if err != nil {
		return false, err
	}
	if status != string(bs.Status) || subject != bs.Subject || property != bs.Property {
		return false, nil
	}
	if !valuesEqual(val, bs.Value) {
		return false, nil
	}
	quals, refs, err := s.loadStatementProvenanceAtRevision(ctx, tx, statementID, rev)
	if err != nil {
		return false, err
	}
	if len(refs) != len(bs.ReferenceIDs) {
		return false, nil
	}
	refSet := map[string]struct{}{}
	for _, r := range refs {
		refSet[r] = struct{}{}
	}
	for _, r := range bs.ReferenceIDs {
		if _, ok := refSet[r]; !ok {
			return false, nil
		}
	}
	if len(quals) != len(bs.Qualifiers) {
		return false, nil
	}
	for i, q := range quals {
		if q.PropertyPID != bs.Qualifiers[i].Property || !valuesEqual(q.Value, bs.Qualifiers[i].Value) {
			return false, nil
		}
	}
	return true, nil
}

func (s *Store) importReleaseRecord(ctx context.Context, tx pgx.Tx, m domain.BundleManifest, cs *changeSetTx) (*domain.Release, error) {
	var packageID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM package WHERE code = $1`, m.Package).Scan(&packageID); err != nil {
		return nil, err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM release WHERE package_id = $1 AND version = $2)`, packageID, m.Version).Scan(&exists); err != nil {
		return nil, err
	}
	if exists {
		return nil, fmt.Errorf("%w: release %s@%s already exists", ErrReleaseImmutable, m.Package, m.Version)
	}

	publishedAt, err := time.Parse(time.RFC3339Nano, m.PublishedAt)
	if err != nil {
		publishedAt, err = time.Parse(time.RFC3339, m.PublishedAt)
		if err != nil {
			return nil, fmt.Errorf("invalid publishedAt: %w", err)
		}
	}
	publishedAt = publishedAt.UTC()

	releaseID := datatype.NewUUID()
	manifestJSON, _ := json.Marshal(m)
	_, err = tx.Exec(ctx, `
		INSERT INTO release (id, package_id, version, published_at, manifest)
		VALUES ($1,$2,$3,$4,$5)
	`, releaseID, packageID, m.Version, publishedAt, manifestJSON)
	if err != nil {
		return nil, err
	}

	for _, d := range m.Dependencies {
		_, err = tx.Exec(ctx, `
			INSERT INTO release_dependency (release_id, dependency_code, dependency_version)
			VALUES ($1,$2,$3)
		`, releaseID, d.DependencyCode, d.DependencyVersion)
		if err != nil {
			return nil, err
		}
	}
	for _, obj := range m.ObjectIndex {
		_, err = tx.Exec(ctx, `
			INSERT INTO release_object (release_id, object_type, object_public_id, revision_no)
			VALUES ($1,$2,$3,$4)
		`, releaseID, obj.ObjectType, obj.ObjectPublicID, obj.RevisionNo)
		if err != nil {
			return nil, err
		}
	}
	if err := cs.addItem(ctx, tx, "release", releaseID, m.Package+"@"+m.Version, "import", nil); err != nil {
		return nil, err
	}

	return &domain.Release{
		ID:           releaseID,
		PackageCode:  m.Package,
		Version:      m.Version,
		PublishedAt:  publishedAt,
		Dependencies: m.Dependencies,
		Objects:      m.ObjectIndex,
	}, nil
}

func csActor(cs *changeSetTx) string {
	return "import"
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func normalizeMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func valuesEqual(a, b datatype.Value) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}
