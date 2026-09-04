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
)

func (s *Store) CreateStatementInOpenChangeSet(ctx context.Context, meta domain.WriteMeta, in domain.CreateStatementInput) (*domain.WriteResult[domain.Statement], error) {
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

	subjectQID, propertyPID, err := s.resolveStatementEndsForOpen(ctx, tx, row.id, in.SubjectPublicID, in.PropertyPublicID)
	if err != nil {
		return nil, err
	}

	// Prefer committed property datatype when available; overlay property defaults to String if unknown.
	dtype := datatype.String
	var dt string
	err = tx.QueryRow(ctx, `
		SELECT pp.datatype FROM property_profile pp
		JOIN entity e ON e.id = pp.entity_id WHERE e.public_id = $1
	`, propertyPID).Scan(&dt)
	if err == nil {
		dtype = datatype.Type(dt)
	} else {
		var ovDT *string
		_ = tx.QueryRow(ctx, `
			SELECT datatype FROM changeset_entity_overlay
			WHERE changeset_id = $1 AND public_id = $2 AND kind = 'property'
		`, row.id, propertyPID).Scan(&ovDT)
		if ovDT != nil && *ovDT != "" {
			dtype = datatype.Type(*ovDT)
		}
	}

	val := in.Value
	// Soft validation: try resolve against committed graph; if EntityReference target only in overlay, keep as-is.
	if val.Type == datatype.EntityReference || dtype == datatype.EntityReference {
		if _, _, err := s.resolveAndEncodeValue(ctx, tx, dtype, val); err != nil {
			// allow overlay-only targets
			if val.EntityID == nil || *val.EntityID == "" {
				return nil, err
			}
		} else {
			val, _, err = s.resolveAndEncodeValue(ctx, tx, dtype, val)
			if err != nil {
				return nil, err
			}
		}
	} else {
		val, _, err = s.resolveAndEncodeValue(ctx, tx, dtype, val)
		if err != nil {
			return nil, err
		}
	}

	id := datatype.NewUUID()
	localID := generatedIRILocal("statement")
	pkgID, err := s.resolvePackageIDRequired(ctx, tx, in.PackageCode)
	if err != nil {
		return nil, err
	}
	pkgCode, iriBase, err := s.packageIRIBaseByID(ctx, tx, pkgID)
	if err != nil {
		return nil, err
	}
	publicID := resolvePublicIRI(iriBase, "statement/"+localID, "statement/"+localID, pkgCode)
	now := time.Now().UTC()

	quals := make([]domain.Qualifier, 0, len(in.Qualifiers))
	for _, q := range in.Qualifiers {
		quals = append(quals, domain.Qualifier{PropertyPID: q.Property, Value: q.Value})
	}

	st := domain.Statement{
		ID: id, PublicID: publicID, PackageCode: pkgCode,
		SubjectQID: subjectQID, PropertyPID: propertyPID,
		Status: domain.StatementActive, Value: val, RevisionNo: 1,
		Qualifiers: quals, ReferenceIDs: in.ReferenceIDs,
		ValidFrom: in.ValidFrom, ValidTo: in.ValidTo,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.claimObjectTx(ctx, tx, row.id, "statement", id, publicID, 0, "create"); err != nil {
		return nil, err
	}
	if err := s.upsertStatementOverlayTx(ctx, tx, row.id, st); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Statement]{Value: st, ChangeSet: s.openChangeSetDomain(row)}, nil
}

func (s *Store) resolveStatementEndsForOpen(ctx context.Context, tx pgx.Tx, csID uuid.UUID, subject, property string) (string, string, error) {
	var subj string
	err := tx.QueryRow(ctx, `SELECT public_id FROM entity WHERE public_id = $1`, subject).Scan(&subj)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			SELECT public_id FROM changeset_entity_overlay WHERE changeset_id = $1 AND public_id = $2
		`, csID, subject).Scan(&subj)
	}
	if err != nil {
		return "", "", fmt.Errorf("subject: %w", err)
	}
	var prop string
	err = tx.QueryRow(ctx, `
		SELECT e.public_id FROM entity e
		JOIN property_profile pp ON pp.entity_id = e.id
		WHERE e.public_id = $1 AND e.status <> 'deleted'
	`, property).Scan(&prop)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `
			SELECT public_id FROM changeset_entity_overlay
			WHERE changeset_id = $1 AND public_id = $2 AND kind = 'property'
		`, csID, property).Scan(&prop)
	}
	if err != nil {
		return "", "", fmt.Errorf("property: %w", err)
	}
	return subj, prop, nil
}

func (s *Store) ReviseStatementInOpenChangeSet(ctx context.Context, meta domain.WriteMeta, publicID string, in domain.ReviseStatementInput) (*domain.WriteResult[domain.Statement], error) {
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
	st, baseRev, fromOverlay, err := s.resolveStatementForOpenWrite(ctx, tx, row.id, publicID)
	if err != nil {
		return nil, err
	}
	if in.ExpectedRevision > 0 && !fromOverlay && st.RevisionNo != in.ExpectedRevision {
		return nil, fmt.Errorf("%w: statement revision %d expected %d", ErrConflict, st.RevisionNo, in.ExpectedRevision)
	}
	if in.Value != nil {
		st.Value = *in.Value
	}
	if in.ReplaceQualifiers {
		quals := make([]domain.Qualifier, 0, len(in.Qualifiers))
		for _, q := range in.Qualifiers {
			quals = append(quals, domain.Qualifier{PropertyPID: q.Property, Value: q.Value})
		}
		st.Qualifiers = quals
	}
	if in.ReplaceReferences {
		st.ReferenceIDs = in.ReferenceIDs
	}
	if in.ReplaceValidTime {
		st.ValidFrom = in.ValidFrom
		st.ValidTo = in.ValidTo
	}
	st.UpdatedAt = time.Now().UTC()
	if !fromOverlay {
		st.RevisionNo = baseRev + 1
	}
	opKind := "update"
	if fromOverlay && baseRev == 0 {
		opKind = "create"
	}
	if err := s.claimObjectTx(ctx, tx, row.id, "statement", st.ID, st.PublicID, baseRev, opKind); err != nil {
		return nil, err
	}
	if err := s.upsertStatementOverlayTx(ctx, tx, row.id, *st); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Statement]{Value: *st, ChangeSet: s.openChangeSetDomain(row)}, nil
}

func (s *Store) DeprecateStatementInOpenChangeSet(ctx context.Context, meta domain.WriteMeta, publicID string, expectedRevision int) (*domain.WriteResult[domain.Statement], error) {
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
	st, baseRev, fromOverlay, err := s.resolveStatementForOpenWrite(ctx, tx, row.id, publicID)
	if err != nil {
		return nil, err
	}
	if expectedRevision > 0 && !fromOverlay && st.RevisionNo != expectedRevision {
		return nil, fmt.Errorf("%w: statement revision %d expected %d", ErrConflict, st.RevisionNo, expectedRevision)
	}
	st.Status = domain.StatementDeprecated
	st.UpdatedAt = time.Now().UTC()
	if !fromOverlay {
		st.RevisionNo = baseRev + 1
	}
	opKind := "deprecate"
	if fromOverlay && baseRev == 0 {
		opKind = "create"
	}
	if err := s.claimObjectTx(ctx, tx, row.id, "statement", st.ID, st.PublicID, baseRev, opKind); err != nil {
		return nil, err
	}
	if err := s.upsertStatementOverlayTx(ctx, tx, row.id, *st); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Statement]{Value: *st, ChangeSet: s.openChangeSetDomain(row)}, nil
}

func (s *Store) resolveStatementForOpenWrite(ctx context.Context, tx pgx.Tx, csID uuid.UUID, publicID string) (*domain.Statement, int, bool, error) {
	var objectID uuid.UUID
	var ovPublicID, pkgCode, subject, property, status string
	var valueJSON, qualJSON, refJSON []byte
	var rev int
	var vf, vt *time.Time
	var created, updated time.Time
	err := tx.QueryRow(ctx, `
		SELECT object_id, public_id, package_code, subject_public_id, property_public_id, status,
			value_json, qualifiers_json, references_json, valid_from, valid_to, revision_no, created_at, updated_at
		FROM changeset_statement_overlay WHERE changeset_id = $1 AND public_id = $2
	`, csID, publicID).Scan(&objectID, &ovPublicID, &pkgCode, &subject, &property, &status,
		&valueJSON, &qualJSON, &refJSON, &vf, &vt, &rev, &created, &updated)
	if err == nil {
		st := &domain.Statement{
			ID: objectID, PublicID: ovPublicID, PackageCode: pkgCode,
			SubjectQID: subject, PropertyPID: property, Status: domain.StatementStatus(status),
			RevisionNo: rev, ValidFrom: vf, ValidTo: vt, CreatedAt: created, UpdatedAt: updated,
		}
		_ = json.Unmarshal(valueJSON, &st.Value)
		_ = json.Unmarshal(qualJSON, &st.Qualifiers)
		_ = json.Unmarshal(refJSON, &st.ReferenceIDs)
		var baseRev int
		_ = tx.QueryRow(ctx, `SELECT base_revision_no FROM changeset_object_claim WHERE changeset_id = $1 AND object_id = $2`, csID, objectID).Scan(&baseRev)
		return st, baseRev, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, false, err
	}
	st, err := s.getStatementTx(ctx, tx, publicID)
	if err != nil {
		return nil, 0, false, err
	}
	if err := s.enrichStatement(ctx, tx, st); err != nil {
		return nil, 0, false, err
	}
	return st, st.RevisionNo, false, nil
}

func (s *Store) CancelOpenChangeSet(ctx context.Context, meta domain.WriteMeta, publicID string) (*domain.ChangeSet, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var cs domain.ChangeSet
	var status string
	var committedAt *time.Time
	var comment *string
	err = tx.QueryRow(ctx, `
		SELECT id, public_id, COALESCE(actor,''), operation_type, comment, status, COALESCE(opened_at, now()), committed_at
		FROM change_set WHERE public_id = $1 FOR UPDATE
	`, publicID).Scan(&cs.ID, &cs.PublicID, &cs.Actor, &cs.OperationType, &comment, &status, &cs.OpenedAt, &committedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOpenChangeSetNotFound
	}
	if err != nil {
		return nil, err
	}
	if comment != nil {
		cs.Comment = *comment
	}
	if status == string(domain.ChangeSetCancelled) {
		cs.Status = domain.ChangeSetCancelled
		if committedAt != nil {
			cs.CommittedAt = *committedAt
		}
		_ = tx.Commit(ctx)
		return &cs, nil
	}
	if status != string(domain.ChangeSetOpen) {
		return nil, ErrOpenChangeSetClosed
	}
	if err := s.assertOpenChangeSetActor(&openChangeSetRow{actor: cs.Actor}, meta.Actor); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM changeset_entity_overlay WHERE changeset_id = $1`, cs.ID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM changeset_statement_overlay WHERE changeset_id = $1`, cs.ID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM changeset_object_claim WHERE changeset_id = $1`, cs.ID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE change_set SET status = 'cancelled' WHERE id = $1`, cs.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	cs.Status = domain.ChangeSetCancelled
	return &cs, nil
}

func (s *Store) CommitOpenChangeSet(ctx context.Context, meta domain.WriteMeta, publicID string) (*domain.WriteResult[domain.ChangeSet], error) {
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
		var cs domain.ChangeSet
		if err := json.Unmarshal(hit.responseBody, &cs); err != nil {
			return nil, err
		}
		return &domain.WriteResult[domain.ChangeSet]{Value: cs, Replay: true, ResponseRaw: hit.responseBody}, nil
	}

	row, err := s.loadOpenChangeSetTx(ctx, tx, publicID)
	if err != nil {
		return nil, err
	}
	if err := s.assertOpenChangeSetActor(row, meta.Actor); err != nil {
		return nil, err
	}

	claims, err := s.lockClaimsTx(ctx, tx, row.id)
	if err != nil {
		return nil, err
	}
	for _, c := range claims {
		if err := s.assertClaimAgainstCommitted(ctx, tx, c); err != nil {
			return nil, err
		}
	}

	now := time.Now().UTC()
	actor := meta.Actor
	if actor == "" {
		actor = row.actor
	}
	if meta.IdempotencyKey != "" {
		_, err = tx.Exec(ctx, `UPDATE change_set SET idempotency_key = $2, request_hash = $3, correlation_id = COALESCE($4, correlation_id) WHERE id = $1`,
			row.id, meta.IdempotencyKey, nullIfEmpty(meta.RequestHash), nullIfEmpty(meta.CorrelationID))
		if err != nil {
			return nil, err
		}
	}

	csTx := &changeSetTx{id: row.id, publicID: row.publicID, committed: now}

	for _, c := range claims {
		switch c.objectType {
		case "entity":
			if err := s.applyEntityOverlayTx(ctx, tx, csTx, actor, c); err != nil {
				return nil, err
			}
		case "statement":
			if err := s.applyStatementOverlayTx(ctx, tx, csTx, actor, c); err != nil {
				return nil, err
			}
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE change_set SET status = 'committed', committed_at = $2,
			operation_type = CASE WHEN operation_type = 'open' THEN 'openCommit' ELSE operation_type END
		WHERE id = $1
	`, row.id, now); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM changeset_entity_overlay WHERE changeset_id = $1`, row.id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM changeset_statement_overlay WHERE changeset_id = $1`, row.id); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM changeset_object_claim WHERE changeset_id = $1`, row.id); err != nil {
		return nil, err
	}

	out := &domain.ChangeSet{
		ID: row.id, PublicID: row.publicID, Actor: actor,
		OperationType: row.opType, Comment: row.comment,
		Status: domain.ChangeSetCommitted, OpenedAt: row.openedAt, CommittedAt: now,
		Items: csTx.items, ItemCount: len(csTx.items),
		IdempotencyKey: meta.IdempotencyKey, CorrelationID: meta.CorrelationID,
	}
	if out.OperationType == "open" {
		out.OperationType = "openCommit"
	}
	if err := s.finalizeChangeSet(ctx, tx, csTx, out); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.ChangeSet]{Value: *out, ChangeSet: out}, nil
}

type claimRow struct {
	objectType  string
	objectID    uuid.UUID
	canonicalIRI string
	baseRevision int
	opKind      string
}

func (s *Store) lockClaimsTx(ctx context.Context, tx pgx.Tx, csID uuid.UUID) ([]claimRow, error) {
	rows, err := tx.Query(ctx, `
		SELECT object_type, object_id, canonical_iri, base_revision_no, op_kind
		FROM changeset_object_claim WHERE changeset_id = $1 FOR UPDATE
	`, csID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []claimRow
	for rows.Next() {
		var c claimRow
		if err := rows.Scan(&c.objectType, &c.objectID, &c.canonicalIRI, &c.baseRevision, &c.opKind); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) assertClaimAgainstCommitted(ctx context.Context, tx pgx.Tx, c claimRow) error {
	switch c.objectType {
	case "entity":
		if c.baseRevision == 0 {
			var exists bool
			_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM entity WHERE id = $1 OR public_id = $2)`, c.objectID, c.canonicalIRI).Scan(&exists)
			if exists {
				return fmt.Errorf("%w: create conflicts with committed entity", ErrConflict)
			}
			return nil
		}
		var current int
		err := tx.QueryRow(ctx, `SELECT current_revision_no FROM entity WHERE id = $1 FOR UPDATE`, c.objectID).Scan(&current)
		if err != nil {
			return fmt.Errorf("%w: claimed entity missing: %v", ErrConflict, err)
		}
		if current != c.baseRevision {
			return fmt.Errorf("%w: entity revision %d expected %d", ErrConflict, current, c.baseRevision)
		}
	case "statement":
		if c.baseRevision == 0 {
			var exists bool
			_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM statement WHERE id = $1 OR public_id = $2)`, c.objectID, c.canonicalIRI).Scan(&exists)
			if exists {
				return fmt.Errorf("%w: create conflicts with committed statement", ErrConflict)
			}
			return nil
		}
		var current int
		err := tx.QueryRow(ctx, `SELECT current_revision_no FROM statement WHERE id = $1 FOR UPDATE`, c.objectID).Scan(&current)
		if err != nil {
			return fmt.Errorf("%w: claimed statement missing: %v", ErrConflict, err)
		}
		if current != c.baseRevision {
			return fmt.Errorf("%w: statement revision %d expected %d", ErrConflict, current, c.baseRevision)
		}
	}
	return nil
}

func (s *Store) applyEntityOverlayTx(ctx context.Context, tx pgx.Tx, cs *changeSetTx, actor string, c claimRow) error {
	var publicID, pkgCode, iriLocal, status, kind string
	var labelsJSON, descJSON, constraintsJSON []byte
	var datatypeStr, subclassOf *string
	var rev int
	var created, updated time.Time
	err := tx.QueryRow(ctx, `
		SELECT public_id, package_code, iri_local, status, labels, descriptions, kind,
			datatype, constraints, subclass_of, revision_no, created_at, updated_at
		FROM changeset_entity_overlay WHERE changeset_id = $1 AND object_id = $2
	`, cs.id, c.objectID).Scan(&publicID, &pkgCode, &iriLocal, &status, &labelsJSON, &descJSON, &kind,
		&datatypeStr, &constraintsJSON, &subclassOf, &rev, &created, &updated)
	if err != nil {
		return err
	}
	labels, _ := jsonToLabels(labelsJSON)
	descs, _ := jsonToLabels(descJSON)
	now := time.Now().UTC()

	if c.baseRevision == 0 {
		pkgID, err := s.resolvePackageIDRequired(ctx, tx, pkgCode)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO entity (id, public_id, status, current_revision_no, package_id, iri_local, created_at, updated_at)
			VALUES ($1,$2,$3,1,$4,$5,$6,$7)
		`, c.objectID, publicID, status, pkgID, iriLocal, created, now)
		if err != nil {
			return err
		}
		for lang, text := range labels {
			if _, err := tx.Exec(ctx, `INSERT INTO entity_label (entity_id, lang, text) VALUES ($1,$2,$3)`, c.objectID, lang, text); err != nil {
				return err
			}
		}
		for lang, text := range descs {
			if _, err := tx.Exec(ctx, `INSERT INTO entity_description (entity_id, lang, text) VALUES ($1,$2,$3)`, c.objectID, lang, text); err != nil {
				return err
			}
		}
		objectType := "entity"
		var payload any
		if kind == "property" && datatypeStr != nil {
			objectType = "property"
			var cons domain.PropertyConstraints
			_ = json.Unmarshal(constraintsJSON, &cons)
			_, err = tx.Exec(ctx, `INSERT INTO property_profile (entity_id, datatype, constraints) VALUES ($1,$2,$3)`,
				c.objectID, *datatypeStr, constraintsJSON)
			if err != nil {
				return err
			}
			payload = map[string]any{"datatype": *datatypeStr, "constraints": cons}
		}
		if kind == "class" {
			objectType = "class"
			sub := ""
			if subclassOf != nil {
				sub = *subclassOf
			}
			doc := domain.ClassDocument{SubClassOf: sub}
			docJSON, _ := json.Marshal(doc)
			_, err = tx.Exec(ctx, `INSERT INTO class_profile (entity_id, document) VALUES ($1,$2)`, c.objectID, docJSON)
			if err != nil {
				return err
			}
			payload = doc
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO entity_revision (id, entity_id, revision_no, status, labels, descriptions, change_set_id, actor, created_at)
			VALUES ($1,$2,1,$3,$4,$5,$6,$7,$8)
		`, datatype.NewUUID(), c.objectID, status, labelsJSON, descJSON, cs.id, actor, now)
		if err != nil {
			return err
		}
		return cs.addItem(ctx, tx, objectType, c.objectID, publicID, "create", payload)
	}

	nextRev := c.baseRevision + 1
	_, err = tx.Exec(ctx, `
		UPDATE entity SET status = $2, iri_local = $3, current_revision_no = $4, updated_at = $5 WHERE id = $1
	`, c.objectID, status, iriLocal, nextRev, now)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM entity_label WHERE entity_id = $1`, c.objectID); err != nil {
		return err
	}
	for lang, text := range labels {
		if _, err := tx.Exec(ctx, `INSERT INTO entity_label (entity_id, lang, text) VALUES ($1,$2,$3)`, c.objectID, lang, text); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM entity_description WHERE entity_id = $1`, c.objectID); err != nil {
		return err
	}
	for lang, text := range descs {
		if _, err := tx.Exec(ctx, `INSERT INTO entity_description (entity_id, lang, text) VALUES ($1,$2,$3)`, c.objectID, lang, text); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO entity_revision (id, entity_id, revision_no, status, labels, descriptions, change_set_id, actor, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, datatype.NewUUID(), c.objectID, nextRev, status, labelsJSON, descJSON, cs.id, actor, now)
	if err != nil {
		return err
	}
	op := c.opKind
	if op == "" {
		op = "update"
	}
	return cs.addItem(ctx, tx, "entity", c.objectID, publicID, op, nil)
}

func (s *Store) applyStatementOverlayTx(ctx context.Context, tx pgx.Tx, cs *changeSetTx, actor string, c claimRow) error {
	var publicID, pkgCode, subjectPID, propertyPID, status string
	var valueJSON, qualJSON, refJSON []byte
	var vf, vt *time.Time
	var created time.Time
	err := tx.QueryRow(ctx, `
		SELECT public_id, package_code, subject_public_id, property_public_id, status,
			value_json, qualifiers_json, references_json, valid_from, valid_to, created_at
		FROM changeset_statement_overlay WHERE changeset_id = $1 AND object_id = $2
	`, cs.id, c.objectID).Scan(&publicID, &pkgCode, &subjectPID, &propertyPID, &status,
		&valueJSON, &qualJSON, &refJSON, &vf, &vt, &created)
	if err != nil {
		return err
	}
	var val datatype.Value
	_ = json.Unmarshal(valueJSON, &val)

	var subjectID, propertyID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM entity WHERE public_id = $1`, subjectPID).Scan(&subjectID); err != nil {
		return fmt.Errorf("subject: %w", err)
	}
	var dt string
	if err := tx.QueryRow(ctx, `
		SELECT e.id, pp.datatype FROM entity e JOIN property_profile pp ON pp.entity_id = e.id WHERE e.public_id = $1
	`, propertyPID).Scan(&propertyID, &dt); err != nil {
		return fmt.Errorf("property: %w", err)
	}
	val, sv, err := s.resolveAndEncodeValue(ctx, tx, datatype.Type(dt), val)
	if err != nil {
		return err
	}
	pkgID, err := s.resolvePackageIDRequired(ctx, tx, pkgCode)
	if err != nil {
		return err
	}
	now := time.Now().UTC()

	if c.baseRevision == 0 {
		_, err = tx.Exec(ctx, `
			INSERT INTO statement (
				id, public_id, subject_id, property_id, status, value_type,
				value_bool, value_int64, value_numeric, value_date, value_timestamptz,
				value_text, value_entity_id, value_json, valid_from, valid_to,
				current_revision_no, package_id, created_at, updated_at
			) VALUES (
				$1,$2,$3,$4,$5,$6,
				$7,$8,$9,$10,$11,
				$12,$13,$14,$15,$16,1,$17,$18,$19
			)
		`, c.objectID, publicID, subjectID, propertyID, status, sv.Type,
			sv.Bool, sv.Int64, sv.Numeric, sv.Date, sv.Timestamptz,
			sv.Text, sv.EntityID, sv.JSON, vf, vt, pkgID, created, now)
		if err != nil {
			return err
		}
		if status == string(domain.StatementActive) {
			if err := s.refreshStatementCurrentTx(ctx, tx, c.objectID); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO statement_revision (
				id, statement_id, revision_no, status, value_type,
				value_bool, value_int64, value_numeric, value_date, value_timestamptz,
				value_text, value_entity_id, value_json, valid_from, valid_to,
				change_set_id, actor, created_at
			) VALUES ($1,$2,1,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		`, datatype.NewUUID(), c.objectID, status, sv.Type,
			sv.Bool, sv.Int64, sv.Numeric, sv.Date, sv.Timestamptz,
			sv.Text, sv.EntityID, sv.JSON, vf, vt, cs.id, actor, now)
		if err != nil {
			return err
		}
		return cs.addItem(ctx, tx, "statement", c.objectID, publicID, "create", nil)
	}

	nextRev := c.baseRevision + 1
	_, err = tx.Exec(ctx, `
		UPDATE statement SET
			status = $2, value_type = $3,
			value_bool = $4, value_int64 = $5, value_numeric = $6, value_date = $7, value_timestamptz = $8,
			value_text = $9, value_entity_id = $10, value_json = $11,
			valid_from = $12, valid_to = $13, current_revision_no = $14, updated_at = $15
		WHERE id = $1
	`, c.objectID, status, sv.Type,
		sv.Bool, sv.Int64, sv.Numeric, sv.Date, sv.Timestamptz,
		sv.Text, sv.EntityID, sv.JSON, vf, vt, nextRev, now)
	if err != nil {
		return err
	}
	if status == string(domain.StatementActive) {
		if err := s.refreshStatementCurrentTx(ctx, tx, c.objectID); err != nil {
			return err
		}
	} else {
		_, _ = tx.Exec(ctx, `DELETE FROM statement_current WHERE statement_id = $1`, c.objectID)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO statement_revision (
			id, statement_id, revision_no, status, value_type,
			value_bool, value_int64, value_numeric, value_date, value_timestamptz,
			value_text, value_entity_id, value_json, valid_from, valid_to,
			change_set_id, actor, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)
	`, datatype.NewUUID(), c.objectID, nextRev, status, sv.Type,
		sv.Bool, sv.Int64, sv.Numeric, sv.Date, sv.Timestamptz,
		sv.Text, sv.EntityID, sv.JSON, vf, vt, cs.id, actor, now)
	if err != nil {
		return err
	}
	op := c.opKind
	if op == "" {
		op = "update"
	}
	return cs.addItem(ctx, tx, "statement", c.objectID, publicID, op, nil)
}

func (s *Store) refreshStatementCurrentTx(ctx context.Context, tx pgx.Tx, statementID uuid.UUID) error {
	_, err := tx.Exec(ctx, `DELETE FROM statement_current WHERE statement_id = $1`, statementID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO statement_current (
			statement_id, public_id, subject_id, property_id, value_type,
			value_bool, value_int64, value_numeric, value_date, value_timestamptz,
			value_text, value_entity_id, value_json, valid_from, valid_to, updated_at
		)
		SELECT id, public_id, subject_id, property_id, value_type,
			value_bool, value_int64, value_numeric, value_date, value_timestamptz,
			value_text, value_entity_id, value_json, valid_from, valid_to, updated_at
		FROM statement WHERE id = $1 AND status = 'active'
	`, statementID)
	return err
}
