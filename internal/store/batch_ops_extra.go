package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
)

func (s *Store) setEntityIRIAliasesInTx(
	ctx context.Context,
	tx pgx.Tx,
	cs *changeSetTx,
	meta domain.WriteMeta,
	publicID string,
	aliases []domain.EntityIRIAlias,
) (*domain.Entity, error) {
	norm, err := normalizeEntityIRIAliases(aliases)
	if err != nil {
		return nil, err
	}
	var entityID uuid.UUID
	var status string
	var rev int
	if err := tx.QueryRow(ctx, `SELECT id, status, current_revision_no FROM entity WHERE public_id = $1`, publicID).
		Scan(&entityID, &status, &rev); err != nil {
		return nil, err
	}
	if err := errIfEntityDeleted(status); err != nil {
		return nil, err
	}
	if err := s.replaceEntityIRIAliasesTx(ctx, tx, entityID, norm); err != nil {
		return nil, err
	}
	nextRev := rev + 1
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `UPDATE entity SET current_revision_no = $2, updated_at = $3 WHERE id = $1`, entityID, nextRev, now); err != nil {
		return nil, err
	}
	ent, err := s.loadEntityTx(ctx, tx, publicID)
	if err != nil {
		return nil, err
	}
	labelsJSON, _ := labelsToJSON(ent.Labels)
	descJSON, _ := labelsToJSON(ent.Descriptions)
	if _, err := tx.Exec(ctx, `
		INSERT INTO entity_revision (id, entity_id, revision_no, status, labels, descriptions, change_set_id, actor, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, datatype.NewUUID(), entityID, nextRev, ent.Status, labelsJSON, descJSON, cs.id, meta.Actor, now); err != nil {
		return nil, err
	}
	if err := cs.addItem(ctx, tx, "entity", entityID, publicID, "setIRIAliases", map[string]any{
		"aliasCount": len(norm), "revisionNo": nextRev,
	}); err != nil {
		return nil, err
	}
	ent.IRIAliases = norm
	ent.RevisionNo = nextRev
	ent.UpdatedAt = now
	return ent, nil
}

func (s *Store) updatePropertyInTx(
	ctx context.Context,
	tx pgx.Tx,
	cs *changeSetTx,
	meta domain.WriteMeta,
	pid string,
	in domain.UpdatePropertyInput,
) (*domain.Property, error) {
	var entityID uuid.UUID
	var rev int
	err := tx.QueryRow(ctx, `
		SELECT e.id, e.current_revision_no FROM entity e
		JOIN property_profile pp ON pp.entity_id = e.id
		WHERE e.public_id = $1 AND e.status <> 'deleted'
	`, pid).Scan(&entityID, &rev)
	if err != nil {
		return nil, err
	}
	if in.ExpectedRevision > 0 && in.ExpectedRevision != rev {
		return nil, fmt.Errorf("%w: expected revision %d have %d", ErrConflict, in.ExpectedRevision, rev)
	}
	if in.Constraints != nil {
		constraintsJSON, _ := json.Marshal(*in.Constraints)
		if _, err := tx.Exec(ctx, `UPDATE property_profile SET constraints = $2 WHERE entity_id = $1`, entityID, constraintsJSON); err != nil {
			return nil, err
		}
	}
	nextRev := rev + 1
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `UPDATE entity SET current_revision_no = $2, updated_at = $3 WHERE id = $1`, entityID, nextRev, now); err != nil {
		return nil, err
	}
	ent, err := s.loadEntityTx(ctx, tx, pid)
	if err != nil {
		return nil, err
	}
	labelsJSON, _ := labelsToJSON(ent.Labels)
	descJSON, _ := labelsToJSON(ent.Descriptions)
	if _, err := tx.Exec(ctx, `
		INSERT INTO entity_revision (id, entity_id, revision_no, status, labels, descriptions, change_set_id, actor, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, datatype.NewUUID(), entityID, nextRev, ent.Status, labelsJSON, descJSON, cs.id, meta.Actor, now); err != nil {
		return nil, err
	}
	if err := cs.addItem(ctx, tx, "property", entityID, pid, "update", map[string]any{"revisionNo": nextRev}); err != nil {
		return nil, err
	}
	var dt string
	var consJSON []byte
	_ = tx.QueryRow(ctx, `SELECT datatype, constraints FROM property_profile WHERE entity_id = $1`, entityID).Scan(&dt, &consJSON)
	var cons domain.PropertyConstraints
	_ = json.Unmarshal(consJSON, &cons)
	return &domain.Property{
		ID: entityID, PublicID: pid, Datatype: datatype.Type(dt), Status: domain.PropertyActive,
		PackageCode: ent.PackageCode, IRILocal: ent.IRILocal, Labels: ent.Labels, Descriptions: ent.Descriptions,
		Constraints: cons, RevisionNo: nextRev, UpdatedAt: now,
	}, nil
}

func (s *Store) moveEntityInTx(
	ctx context.Context,
	tx pgx.Tx,
	cs *changeSetTx,
	meta domain.WriteMeta,
	publicID string,
	in domain.MoveEntityInput,
) (*domain.Entity, error) {
	var entityID uuid.UUID
	var oldPkg *uuid.UUID
	var status string
	var rev int
	err := tx.QueryRow(ctx, `SELECT id, package_id, status, current_revision_no FROM entity WHERE public_id = $1`, publicID).
		Scan(&entityID, &oldPkg, &status, &rev)
	if err != nil {
		return nil, err
	}
	if err := errIfEntityDeleted(status); err != nil {
		return nil, err
	}
	if in.ExpectedRevision > 0 && in.ExpectedRevision != rev {
		return nil, fmt.Errorf("%w: expected revision %d have %d", ErrConflict, in.ExpectedRevision, rev)
	}
	newPkg, err := s.resolvePackageIDRequired(ctx, tx, in.PackageCode)
	if err != nil {
		return nil, err
	}
	nextRev := rev + 1
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `UPDATE entity SET package_id = $2, current_revision_no = $3, updated_at = $4 WHERE id = $1`, entityID, newPkg, nextRev, now); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConflict, err)
	}
	if oldPkg != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE statement SET package_id = $2, updated_at = $3
			WHERE subject_id = $1 AND package_id = $4
		`, entityID, newPkg, now, *oldPkg); err != nil {
			return nil, err
		}
	}
	entSnap, err := s.loadEntityTx(ctx, tx, publicID)
	if err != nil {
		return nil, err
	}
	labelsJSON, _ := labelsToJSON(entSnap.Labels)
	descJSON, _ := labelsToJSON(entSnap.Descriptions)
	if _, err := tx.Exec(ctx, `
		INSERT INTO entity_revision (id, entity_id, revision_no, status, labels, descriptions, change_set_id, actor, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, datatype.NewUUID(), entityID, nextRev, entSnap.Status, labelsJSON, descJSON, cs.id, meta.Actor, now); err != nil {
		return nil, err
	}
	if err := cs.addItem(ctx, tx, "entity", entityID, publicID, "move", map[string]any{"packageCode": in.PackageCode, "revisionNo": nextRev}); err != nil {
		return nil, err
	}
	entSnap.PackageCode = in.PackageCode
	entSnap.RevisionNo = nextRev
	entSnap.UpdatedAt = now
	return entSnap, nil
}

func (s *Store) createStatementOverlayInTx(
	ctx context.Context,
	tx pgx.Tx,
	row *openChangeSetRow,
	in domain.CreateStatementInput,
) (*domain.Statement, error) {
	subjectQID, propertyPID, err := s.resolveStatementEndsForOpen(ctx, tx, row.id, in.SubjectPublicID, in.PropertyPublicID)
	if err != nil {
		return nil, err
	}
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
	if val.Type == datatype.EntityReference || dtype == datatype.EntityReference {
		if _, _, err := s.resolveAndEncodeValue(ctx, tx, dtype, val); err != nil {
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

	// Upsert against overlay statements for same subject+property+value (best-effort).
	if in.Upsert {
		rows, err := tx.Query(ctx, `
			SELECT object_id, public_id, revision_no, value_json FROM changeset_statement_overlay
			WHERE changeset_id = $1 AND subject_public_id = $2 AND property_public_id = $3 AND status = 'active'
		`, row.id, subjectQID, propertyPID)
		if err == nil {
			defer rows.Close()
			want, _ := json.Marshal(val)
			for rows.Next() {
				var oid uuid.UUID
				var pid string
				var rev int
				var vj []byte
				if err := rows.Scan(&oid, &pid, &rev, &vj); err != nil {
					return nil, err
				}
				if string(vj) == string(want) {
					return &domain.Statement{
						ID: oid, PublicID: pid, PackageCode: in.PackageCode,
						SubjectQID: subjectQID, PropertyPID: propertyPID,
						Status: domain.StatementActive, Value: val, RevisionNo: rev,
					}, nil
				}
			}
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
	return &st, nil
}
