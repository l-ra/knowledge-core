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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
)

var (
	ErrOpenChangeSetNotFound  = errors.New("open changeset not found")
	ErrOpenChangeSetClosed    = errors.New("changeset is not open")
	ErrOpenChangeSetForbidden = errors.New("changeset actor mismatch")
)

type activeChangeSetsCtxKey struct{}

// WithActiveChangeSets attaches open ChangeSet public IDs for read-merge.
func WithActiveChangeSets(ctx context.Context, ids []string) context.Context {
	cleaned := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		cleaned = append(cleaned, id)
	}
	if len(cleaned) == 0 {
		return ctx
	}
	return context.WithValue(ctx, activeChangeSetsCtxKey{}, cleaned)
}

func ActiveChangeSetsFrom(ctx context.Context) []string {
	v, _ := ctx.Value(activeChangeSetsCtxKey{}).([]string)
	return v
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

type openChangeSetRow struct {
	id       uuid.UUID
	publicID string
	actor    string
	opType   string
	comment  string
	openedAt time.Time
}

func (s *Store) OpenChangeSet(ctx context.Context, meta domain.WriteMeta, in domain.OpenChangeSetInput) (*domain.ChangeSet, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	id := datatype.NewUUID()
	publicID, err := s.nextPublicID(ctx, tx, "changeset", "C")
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	actor := meta.Actor
	if actor == "" {
		actor = "system"
	}
	opType := strings.TrimSpace(in.OperationType)
	if opType == "" {
		opType = strings.TrimSpace(meta.OperationType)
	}
	if opType == "" {
		opType = "open"
	}
	comment := strings.TrimSpace(in.Comment)
	_, err = tx.Exec(ctx, `
		INSERT INTO change_set (id, public_id, actor, committed_at, operation_type, comment, status, opened_at)
		VALUES ($1,$2,$3,NULL,$4,$5,'open',$6)
	`, id, publicID, actor, opType, nullIfEmpty(comment), now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.ChangeSet{
		ID: id, PublicID: publicID, Actor: actor, OperationType: opType, Comment: comment,
		Status: domain.ChangeSetOpen, OpenedAt: now,
	}, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (s *Store) loadOpenChangeSetTx(ctx context.Context, tx pgx.Tx, publicID string) (*openChangeSetRow, error) {
	var row openChangeSetRow
	var status string
	var comment *string
	err := tx.QueryRow(ctx, `
		SELECT id, public_id, COALESCE(actor,''), operation_type, comment, status, COALESCE(opened_at, now())
		FROM change_set WHERE public_id = $1
	`, publicID).Scan(&row.id, &row.publicID, &row.actor, &row.opType, &comment, &status, &row.openedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOpenChangeSetNotFound
	}
	if err != nil {
		return nil, err
	}
	if status != string(domain.ChangeSetOpen) {
		return nil, ErrOpenChangeSetClosed
	}
	if comment != nil {
		row.comment = *comment
	}
	return &row, nil
}

func (s *Store) assertOpenChangeSetActor(row *openChangeSetRow, actor string) error {
	if actor == "" || row.actor == "" {
		return nil
	}
	if row.actor != actor {
		return ErrOpenChangeSetForbidden
	}
	return nil
}

func (s *Store) claimObjectTx(
	ctx context.Context,
	tx pgx.Tx,
	csID uuid.UUID,
	objectType string,
	objectID uuid.UUID,
	canonicalIRI string,
	baseRevision int,
	opKind string,
) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO changeset_object_claim (id, changeset_id, object_type, object_id, canonical_iri, base_revision_no, op_kind)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (object_id) DO UPDATE
			SET op_kind = EXCLUDED.op_kind,
			    canonical_iri = EXCLUDED.canonical_iri
			WHERE changeset_object_claim.changeset_id = EXCLUDED.changeset_id
	`, datatype.NewUUID(), csID, objectType, objectID, canonicalIRI, baseRevision, opKind)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: object claimed by another open changeset", ErrConflict)
		}
		return err
	}
	// ON CONFLICT DO UPDATE with WHERE that doesn't match leaves 0 rows but no error —
	// detect foreign claim:
	var owner uuid.UUID
	err = tx.QueryRow(ctx, `SELECT changeset_id FROM changeset_object_claim WHERE object_id = $1`, objectID).Scan(&owner)
	if err != nil {
		return err
	}
	if owner != csID {
		return fmt.Errorf("%w: object claimed by another open changeset", ErrConflict)
	}
	if canonicalIRI != "" {
		var iriOwner uuid.UUID
		err = tx.QueryRow(ctx, `
			SELECT changeset_id FROM changeset_object_claim
			WHERE canonical_iri = $1 AND object_id <> $2
			LIMIT 1
		`, canonicalIRI, objectID).Scan(&iriOwner)
		if err == nil {
			return fmt.Errorf("%w: IRI claimed by another open changeset", ErrConflict)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	return nil
}

func (s *Store) upsertEntityOverlayTx(
	ctx context.Context,
	tx pgx.Tx,
	csID uuid.UUID,
	ent domain.Entity,
	datatypeStr string,
	constraintsJSON []byte,
	subclassOf string,
) error {
	labelsJSON, _ := labelsToJSON(ent.Labels)
	descJSON, _ := labelsToJSON(ent.Descriptions)
	kind := string(ent.Kind)
	if kind == "" {
		kind = "entity"
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO changeset_entity_overlay (
			changeset_id, object_id, public_id, package_code, iri_local, status,
			labels, descriptions, kind, datatype, constraints, subclass_of, revision_no, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$14)
		ON CONFLICT (changeset_id, object_id) DO UPDATE SET
			public_id = EXCLUDED.public_id,
			package_code = EXCLUDED.package_code,
			iri_local = EXCLUDED.iri_local,
			status = EXCLUDED.status,
			labels = EXCLUDED.labels,
			descriptions = EXCLUDED.descriptions,
			kind = EXCLUDED.kind,
			datatype = EXCLUDED.datatype,
			constraints = EXCLUDED.constraints,
			subclass_of = EXCLUDED.subclass_of,
			revision_no = EXCLUDED.revision_no,
			updated_at = EXCLUDED.updated_at
	`, csID, ent.ID, ent.PublicID, ent.PackageCode, ent.IRILocal, string(ent.Status),
		labelsJSON, descJSON, kind, nullIfEmpty(datatypeStr), constraintsJSON, nullIfEmpty(subclassOf),
		ent.RevisionNo, time.Now().UTC())
	return err
}

func (s *Store) upsertStatementOverlayTx(ctx context.Context, tx pgx.Tx, csID uuid.UUID, st domain.Statement) error {
	valJSON, err := json.Marshal(st.Value)
	if err != nil {
		return err
	}
	qualJSON, _ := json.Marshal(st.Qualifiers)
	if qualJSON == nil {
		qualJSON = []byte("[]")
	}
	refJSON, _ := json.Marshal(st.ReferenceIDs)
	if refJSON == nil {
		refJSON = []byte("[]")
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO changeset_statement_overlay (
			changeset_id, object_id, public_id, package_code, subject_public_id, property_public_id,
			status, value_json, qualifiers_json, references_json, valid_from, valid_to, revision_no, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$14)
		ON CONFLICT (changeset_id, object_id) DO UPDATE SET
			public_id = EXCLUDED.public_id,
			package_code = EXCLUDED.package_code,
			subject_public_id = EXCLUDED.subject_public_id,
			property_public_id = EXCLUDED.property_public_id,
			status = EXCLUDED.status,
			value_json = EXCLUDED.value_json,
			qualifiers_json = EXCLUDED.qualifiers_json,
			references_json = EXCLUDED.references_json,
			valid_from = EXCLUDED.valid_from,
			valid_to = EXCLUDED.valid_to,
			revision_no = EXCLUDED.revision_no,
			updated_at = EXCLUDED.updated_at
	`, csID, st.ID, st.PublicID, st.PackageCode, st.SubjectQID, st.PropertyPID,
		string(st.Status), valJSON, qualJSON, refJSON, st.ValidFrom, st.ValidTo, st.RevisionNo, time.Now().UTC())
	return err
}

func (s *Store) openChangeSetDomain(row *openChangeSetRow) *domain.ChangeSet {
	return &domain.ChangeSet{
		ID: row.id, PublicID: row.publicID, Actor: row.actor,
		OperationType: row.opType, Comment: row.comment,
		Status: domain.ChangeSetOpen, OpenedAt: row.openedAt,
	}
}

func (s *Store) CreateEntityInOpenChangeSet(ctx context.Context, meta domain.WriteMeta, in domain.CreateEntityInput) (*domain.WriteResult[domain.Entity], error) {
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

	row, err := s.loadOpenChangeSetTx(ctx, tx, meta.OpenChangeSetID)
	if err != nil {
		return nil, err
	}
	if err := s.assertOpenChangeSetActor(row, meta.Actor); err != nil {
		return nil, err
	}

	id := datatype.NewUUID()
	localID := generatedIRILocal("entity")
	pkgID, err := s.resolvePackageIDRequired(ctx, tx, in.PackageCode)
	if err != nil {
		return nil, err
	}
	iriLocal, err := normalizeOptionalIRILocal(in.IRILocal)
	if err != nil {
		return nil, err
	}
	if datatype.IsPackageRootIRILocal(iriLocal) {
		return nil, fmt.Errorf("iriLocal %q is reserved for package root", datatype.PackageRootIRILocal)
	}
	pkgCode, iriBase, err := s.packageIRIBaseByID(ctx, tx, pkgID)
	if err != nil {
		return nil, err
	}
	publicID := resolvePublicIRI(iriBase, iriLocal, localID, pkgCode)
	if iriLocal == "" {
		iriLocal = localID
	}
	var exists bool
	_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM entity WHERE public_id = $1)`, publicID).Scan(&exists)
	if exists {
		return nil, fmt.Errorf("%w: IRI already exists in committed data", ErrConflict)
	}

	now := time.Now().UTC()
	ent := domain.Entity{
		ID: id, PublicID: publicID, Status: domain.EntityActive,
		Kind: domain.EntityKindEntity, PackageCode: in.PackageCode,
		IRILocal: iriLocal, Labels: labels, Descriptions: descs, RevisionNo: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	ent.IRI = resolvePublicIRI(iriBase, iriLocal, localID, pkgCode)

	if err := s.claimObjectTx(ctx, tx, row.id, "entity", id, publicID, 0, "create"); err != nil {
		return nil, err
	}
	if err := s.upsertEntityOverlayTx(ctx, tx, row.id, ent, "", nil, ""); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	cs := s.openChangeSetDomain(row)
	return &domain.WriteResult[domain.Entity]{Value: ent, ChangeSet: cs}, nil
}

func (s *Store) UpdateEntityInOpenChangeSet(ctx context.Context, meta domain.WriteMeta, publicID string, in domain.UpdateEntityInput) (*domain.WriteResult[domain.Entity], error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	row, err := s.loadOpenChangeSetTx(ctx, tx, meta.OpenChangeSetID)
	if err != nil {
		return nil, err
	}
	if err := s.assertOpenChangeSetActor(row, meta.Actor); err != nil {
		return nil, err
	}

	ent, baseRev, fromOverlay, err := s.resolveEntityForOpenWrite(ctx, tx, row.id, publicID)
	if err != nil {
		return nil, err
	}
	if in.ExpectedRevision > 0 && !fromOverlay && ent.RevisionNo != in.ExpectedRevision {
		return nil, fmt.Errorf("%w: entity revision %d expected %d", ErrConflict, ent.RevisionNo, in.ExpectedRevision)
	}
	if in.Labels != nil {
		norm, err := datatype.NormalizeLabels(in.Labels)
		if err != nil {
			return nil, err
		}
		if err := datatype.RequireLabelEN(norm); err != nil {
			return nil, err
		}
		ent.Labels = norm
	}
	if in.Descriptions != nil {
		ent.Descriptions = in.Descriptions
	}
	if in.IRILocal != nil {
		iriLocal, err := normalizeOptionalIRILocal(*in.IRILocal)
		if err != nil {
			return nil, err
		}
		if datatype.IsPackageRootIRILocal(iriLocal) {
			return nil, fmt.Errorf("iriLocal %q is reserved for package root", datatype.PackageRootIRILocal)
		}
		ent.IRILocal = iriLocal
	}
	ent.UpdatedAt = time.Now().UTC()
	if !fromOverlay {
		ent.RevisionNo = baseRev + 1
	}
	opKind := "update"
	if fromOverlay && baseRev == 0 {
		opKind = "create"
	}
	if err := s.claimObjectTx(ctx, tx, row.id, "entity", ent.ID, ent.PublicID, baseRev, opKind); err != nil {
		return nil, err
	}
	dt, cons, sub := entityOverlayProfile(ent)
	if err := s.upsertEntityOverlayTx(ctx, tx, row.id, *ent, dt, cons, sub); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Entity]{Value: *ent, ChangeSet: s.openChangeSetDomain(row)}, nil
}

func entityOverlayProfile(ent *domain.Entity) (string, []byte, string) {
	if ent.PropertyProfile != nil {
		b, _ := json.Marshal(ent.PropertyProfile.Constraints)
		return string(ent.PropertyProfile.Datatype), b, ""
	}
	if ent.ClassProfile != nil {
		return "", nil, ent.ClassProfile.SubClassOf
	}
	return "", nil, ""
}

func (s *Store) resolveEntityForOpenWrite(ctx context.Context, tx pgx.Tx, csID uuid.UUID, publicID string) (*domain.Entity, int, bool, error) {
	var objectID uuid.UUID
	var ovPublicID, pkgCode, iriLocal, status, kind string
	var labelsJSON, descJSON, constraintsJSON []byte
	var datatypeStr, subclassOf *string
	var rev int
	err := tx.QueryRow(ctx, `
		SELECT object_id, public_id, package_code, iri_local, status, labels, descriptions, kind,
			datatype, constraints, subclass_of, revision_no
		FROM changeset_entity_overlay
		WHERE changeset_id = $1 AND public_id = $2
	`, csID, publicID).Scan(&objectID, &ovPublicID, &pkgCode, &iriLocal, &status, &labelsJSON, &descJSON, &kind,
		&datatypeStr, &constraintsJSON, &subclassOf, &rev)
	if err == nil {
		ent := &domain.Entity{
			ID: objectID, PublicID: ovPublicID, PackageCode: pkgCode, IRILocal: iriLocal,
			Status: domain.EntityStatus(status), Kind: domain.EntityKind(kind), RevisionNo: rev,
		}
		ent.Labels, _ = jsonToLabels(labelsJSON)
		ent.Descriptions, _ = jsonToLabels(descJSON)
		attachOverlayProfiles(ent, datatypeStr, constraintsJSON, subclassOf)
		var baseRev int
		_ = tx.QueryRow(ctx, `SELECT base_revision_no FROM changeset_object_claim WHERE changeset_id = $1 AND object_id = $2`, csID, objectID).Scan(&baseRev)
		return ent, baseRev, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, false, err
	}

	ent, err := s.loadEntityTx(ctx, tx, publicID)
	if err != nil {
		return nil, 0, false, err
	}
	return ent, ent.RevisionNo, false, nil
}

func attachOverlayProfiles(ent *domain.Entity, datatypeStr *string, constraintsJSON []byte, subclassOf *string) {
	if datatypeStr != nil && *datatypeStr != "" {
		ent.Kind = domain.EntityKindProperty
		info := &domain.PropertyProfileInfo{Datatype: datatype.Type(*datatypeStr)}
		if len(constraintsJSON) > 0 {
			_ = json.Unmarshal(constraintsJSON, &info.Constraints)
		}
		ent.PropertyProfile = info
		return
	}
	if subclassOf != nil {
		ent.Kind = domain.EntityKindClass
		ent.ClassProfile = &domain.ClassProfileInfo{SubClassOf: *subclassOf}
	}
}

func (s *Store) MutateEntityStatusInOpenChangeSet(ctx context.Context, meta domain.WriteMeta, publicID string, expectedRevision int, status domain.EntityStatus, opKind string) (*domain.WriteResult[domain.Entity], error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	row, err := s.loadOpenChangeSetTx(ctx, tx, meta.OpenChangeSetID)
	if err != nil {
		return nil, err
	}
	if err := s.assertOpenChangeSetActor(row, meta.Actor); err != nil {
		return nil, err
	}
	ent, baseRev, fromOverlay, err := s.resolveEntityForOpenWrite(ctx, tx, row.id, publicID)
	if err != nil {
		return nil, err
	}
	if expectedRevision > 0 && !fromOverlay && ent.RevisionNo != expectedRevision {
		return nil, fmt.Errorf("%w: entity revision %d expected %d", ErrConflict, ent.RevisionNo, expectedRevision)
	}
	ent.Status = status
	ent.UpdatedAt = time.Now().UTC()
	if !fromOverlay {
		ent.RevisionNo = baseRev + 1
	}
	claimOp := opKind
	if fromOverlay && baseRev == 0 {
		claimOp = "create"
	}
	if err := s.claimObjectTx(ctx, tx, row.id, "entity", ent.ID, ent.PublicID, baseRev, claimOp); err != nil {
		return nil, err
	}
	dt, cons, sub := entityOverlayProfile(ent)
	if err := s.upsertEntityOverlayTx(ctx, tx, row.id, *ent, dt, cons, sub); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Entity]{Value: *ent, ChangeSet: s.openChangeSetDomain(row)}, nil
}

func (s *Store) CreatePropertyInOpenChangeSet(ctx context.Context, meta domain.WriteMeta, in domain.CreatePropertyInput) (*domain.WriteResult[domain.Property], error) {
	if _, err := datatype.ParseType(string(in.Datatype)); err != nil {
		return nil, err
	}
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
	row, err := s.loadOpenChangeSetTx(ctx, tx, meta.OpenChangeSetID)
	if err != nil {
		return nil, err
	}
	if err := s.assertOpenChangeSetActor(row, meta.Actor); err != nil {
		return nil, err
	}
	id := datatype.NewUUID()
	localID := generatedIRILocal("property")
	pkgID, err := s.resolvePackageIDRequired(ctx, tx, in.PackageCode)
	if err != nil {
		return nil, err
	}
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
	var exists bool
	_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM entity WHERE public_id = $1)`, publicID).Scan(&exists)
	if exists {
		return nil, fmt.Errorf("%w: IRI already exists in committed data", ErrConflict)
	}
	now := time.Now().UTC()
	constraintsJSON, _ := json.Marshal(in.Constraints)
	ent := domain.Entity{
		ID: id, PublicID: publicID, Status: domain.EntityActive, Kind: domain.EntityKindProperty,
		PackageCode: in.PackageCode, IRILocal: iriLocal, Labels: labels, Descriptions: descs, RevisionNo: 1,
		CreatedAt: now, UpdatedAt: now,
		PropertyProfile: &domain.PropertyProfileInfo{Datatype: in.Datatype, Constraints: in.Constraints},
	}
	ent.IRI = publicID
	if err := s.claimObjectTx(ctx, tx, row.id, "entity", id, publicID, 0, "create"); err != nil {
		return nil, err
	}
	if err := s.upsertEntityOverlayTx(ctx, tx, row.id, ent, string(in.Datatype), constraintsJSON, ""); err != nil {
		return nil, err
	}
	if err := s.maybeAutoSetInstanceOfPropertyTx(ctx, tx, publicID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	p := domain.Property{
		ID: id, PublicID: publicID, Datatype: in.Datatype, Status: domain.PropertyActive,
		PackageCode: in.PackageCode, IRILocal: iriLocal, IRI: publicID,
		Labels: labels, Descriptions: descs, Constraints: in.Constraints, RevisionNo: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	return &domain.WriteResult[domain.Property]{Value: p, ChangeSet: s.openChangeSetDomain(row)}, nil
}

func (s *Store) CreateClassInOpenChangeSet(ctx context.Context, meta domain.WriteMeta, in domain.CreateClassInput) (*domain.WriteResult[domain.ClassDefinition], error) {
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
	row, err := s.loadOpenChangeSetTx(ctx, tx, meta.OpenChangeSetID)
	if err != nil {
		return nil, err
	}
	if err := s.assertOpenChangeSetActor(row, meta.Actor); err != nil {
		return nil, err
	}
	id := datatype.NewUUID()
	localID := generatedIRILocal("class")
	pkgID, err := s.resolvePackageIDRequired(ctx, tx, in.PackageCode)
	if err != nil {
		return nil, err
	}
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
	var exists bool
	_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM entity WHERE public_id = $1)`, publicID).Scan(&exists)
	if exists {
		return nil, fmt.Errorf("%w: IRI already exists in committed data", ErrConflict)
	}
	now := time.Now().UTC()
	ent := domain.Entity{
		ID: id, PublicID: publicID, Status: domain.EntityActive, Kind: domain.EntityKindClass,
		PackageCode: in.PackageCode, IRILocal: iriLocal, Labels: labels, Descriptions: descs, RevisionNo: 1,
		CreatedAt: now, UpdatedAt: now,
		ClassProfile: &domain.ClassProfileInfo{SubClassOf: in.SubClassOf},
	}
	ent.IRI = publicID
	if err := s.claimObjectTx(ctx, tx, row.id, "entity", id, publicID, 0, "create"); err != nil {
		return nil, err
	}
	if err := s.upsertEntityOverlayTx(ctx, tx, row.id, ent, "", nil, in.SubClassOf); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	c := domain.ClassDefinition{
		ID: publicID, PublicID: publicID, PackageCode: in.PackageCode, IRILocal: iriLocal, IRI: publicID,
		CanonicalEntityID: id.String(), CanonicalEntityQID: publicID,
		Status: domain.PropertyActive, Labels: labels, Descriptions: descs,
		Document:  domain.ClassDocument{SubClassOf: in.SubClassOf},
		CreatedAt: now.UTC().Format(time.RFC3339Nano),
		UpdatedAt: now.UTC().Format(time.RFC3339Nano),
	}
	return &domain.WriteResult[domain.ClassDefinition]{Value: c, ChangeSet: s.openChangeSetDomain(row)}, nil
}
