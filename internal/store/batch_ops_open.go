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
)

const (
	MaxBatchOperations = 500
	MaxBatchBodyBytes  = 4 << 20 // 4 MiB
)

var ErrBatchLimitExceeded = errors.New("batch limit exceeded")

func validateBatchSize(ops []domain.ChangeOperation) error {
	n := len(ops)
	if n == 0 {
		return fmt.Errorf("%w: operations must not be empty", ErrBatchLimitExceeded)
	}
	if n > MaxBatchOperations {
		return fmt.Errorf("%w: at most %d operations per request (got %d)", ErrBatchLimitExceeded, MaxBatchOperations, n)
	}
	return nil
}

func (s *Store) ApplyChangeSet(ctx context.Context, meta domain.WriteMeta, in domain.ApplyChangeSetInput) (*domain.WriteResult[domain.ChangeSet], error) {
	if err := validateBatchSize(in.Operations); err != nil {
		return nil, err
	}
	if meta.OpenChangeSetID != "" {
		return s.applyChangeSetOpen(ctx, meta, in)
	}
	return s.applyChangeSetCommitted(ctx, meta, in)
}

func (s *Store) applyChangeSetCommitted(ctx context.Context, meta domain.WriteMeta, in domain.ApplyChangeSetInput) (*domain.WriteResult[domain.ChangeSet], error) {
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
		cs, err := s.GetChangeSetByPublicID(ctx, hit.publicID)
		if err != nil {
			return nil, err
		}
		return &domain.WriteResult[domain.ChangeSet]{Value: *cs, ChangeSet: cs, Replay: true, ResponseRaw: hit.responseBody}, nil
	}

	meta.OperationType = in.OperationType
	if meta.OperationType == "" {
		meta.OperationType = "batch"
	}
	cs, err := s.beginChangeSetTx(ctx, tx, meta)
	if err != nil {
		return nil, err
	}
	if comment := strings.TrimSpace(in.Comment); comment != "" {
		if _, err := tx.Exec(ctx, `UPDATE change_set SET comment = $2 WHERE id = $1`, cs.id, comment); err != nil {
			return nil, err
		}
	}

	keys := map[string]string{}
	results := make([]map[string]any, 0, len(in.Operations))
	for i, op := range in.Operations {
		res, err := s.applyOneOp(ctx, tx, cs, meta, op, keys)
		if err != nil {
			return nil, fmt.Errorf("op[%d] %s: %w", i, op.Op, err)
		}
		results = append(results, res)
	}

	// Pure upsert hits (no CS items) — match REST createStatement Replay semantics; no empty CS.
	if len(cs.items) == 0 {
		allHit := true
		for _, r := range results {
			hit, _ := r["upsertHit"].(bool)
			if !hit {
				allHit = false
				break
			}
		}
		if allHit {
			_ = tx.Rollback(ctx)
			raw, _ := json.Marshal(map[string]any{"results": results})
			return &domain.WriteResult[domain.ChangeSet]{
				Replay: true, ResponseRaw: raw,
			}, nil
		}
	}

	response := map[string]any{
		"results": results,
	}
	domainCS := s.changeSetDomain(cs, meta)
	domainCS.Comment = strings.TrimSpace(in.Comment)
	domainCS.Items = cs.items
	domainCS.ItemCount = len(cs.items)
	if err := s.finalizeChangeSet(ctx, tx, cs, response); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	out := map[string]any{"results": results, "changeSet": domainCS.PublicID}
	raw, _ := json.Marshal(out)
	return &domain.WriteResult[domain.ChangeSet]{
		Value: *domainCS, ChangeSet: domainCS, ResponseRaw: raw,
	}, nil
}

func (s *Store) applyChangeSetOpen(ctx context.Context, meta domain.WriteMeta, in domain.ApplyChangeSetInput) (*domain.WriteResult[domain.ChangeSet], error) {
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

	if hit, err := s.checkOpenIdempotency(ctx, tx, row.id, meta); err != nil {
		return nil, err
	} else if hit != nil {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		cs := s.openChangeSetDomain(row)
		return &domain.WriteResult[domain.ChangeSet]{Value: *cs, ChangeSet: cs, Replay: true, ResponseRaw: hit}, nil
	}

	keys := map[string]string{}
	results := make([]map[string]any, 0, len(in.Operations))
	for i, op := range in.Operations {
		res, err := s.applyOneOpOpen(ctx, tx, row, meta, op, keys)
		if err != nil {
			return nil, fmt.Errorf("op[%d] %s: %w", i, op.Op, err)
		}
		results = append(results, res)
	}

	cs := s.openChangeSetDomain(row)
	if c := strings.TrimSpace(in.Comment); c != "" && cs.Comment == "" {
		cs.Comment = c
	}
	response := map[string]any{"results": results}
	responseBody, _ := json.Marshal(response)
	if meta.IdempotencyKey != "" {
		if err := s.saveOpenIdempotency(ctx, tx, row.id, meta, responseBody); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.ChangeSet]{
		Value: *cs, ChangeSet: cs, ResponseRaw: responseBody,
	}, nil
}

func (s *Store) checkOpenIdempotency(ctx context.Context, tx pgx.Tx, csID uuid.UUID, meta domain.WriteMeta) ([]byte, error) {
	if meta.IdempotencyKey == "" {
		return nil, nil
	}
	var hash string
	var body []byte
	err := tx.QueryRow(ctx, `
		SELECT request_hash, response_body FROM open_changeset_idempotency
		WHERE changeset_id = $1 AND idempotency_key = $2
	`, csID, meta.IdempotencyKey).Scan(&hash, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if hash != meta.RequestHash {
		return nil, ErrIdempotencyConflict
	}
	return body, nil
}

func (s *Store) saveOpenIdempotency(ctx context.Context, tx pgx.Tx, csID uuid.UUID, meta domain.WriteMeta, body []byte) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO open_changeset_idempotency (changeset_id, idempotency_key, request_hash, response_body)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (changeset_id, idempotency_key) DO NOTHING
	`, csID, meta.IdempotencyKey, meta.RequestHash, body)
	return err
}

func (s *Store) applyOneOpOpen(
	ctx context.Context,
	tx pgx.Tx,
	row *openChangeSetRow,
	meta domain.WriteMeta,
	op domain.ChangeOperation,
	keys map[string]string,
) (map[string]any, error) {
	switch op.Op {
	case "createEntity":
		ent, err := s.createEntityOverlayInTx(ctx, tx, row, domain.CreateEntityInput{
			PackageCode: op.PackageCode, Labels: op.Labels, Descriptions: op.Descriptions, IRILocal: op.IRILocal,
		})
		if err != nil {
			return nil, err
		}
		if op.ClientKey != "" {
			keys[op.ClientKey] = ent.PublicID
		}
		return map[string]any{"op": op.Op, "entity": ent.PublicID, "revisionNo": ent.RevisionNo, "clientKey": op.ClientKey}, nil

	case "updateEntity":
		eid := resolveKey(keys, op.Entity)
		if eid == "" {
			return nil, fmt.Errorf("updateEntity requires entity")
		}
		in := domain.UpdateEntityInput{
			Labels: op.Labels, Descriptions: op.Descriptions, ExpectedRevision: op.ExpectedRevision,
		}
		if op.IRILocal != "" {
			local := op.IRILocal
			in.IRILocal = &local
		}
		ent, err := s.updateEntityOverlayInTx(ctx, tx, row, eid, in)
		if err != nil {
			return nil, err
		}
		return map[string]any{"op": op.Op, "entity": ent.PublicID, "revisionNo": ent.RevisionNo}, nil

	case "createStatement":
		subject := resolveKey(keys, op.Subject)
		property := resolveKey(keys, op.Property)
		val := op.Value
		if val.EntityID != nil {
			resolved := resolveKey(keys, *val.EntityID)
			val.EntityID = &resolved
		}
		st, err := s.createStatementOverlayInTx(ctx, tx, row, domain.CreateStatementInput{
			PackageCode: op.PackageCode, SubjectPublicID: subject, PropertyPublicID: property,
			Value: val, Qualifiers: op.Qualifiers, ReferenceIDs: op.ReferenceIDs,
			ValidFrom: op.ValidFrom, ValidTo: op.ValidTo, Upsert: op.Upsert,
		})
		if err != nil {
			return nil, err
		}
		if op.ClientKey != "" {
			keys[op.ClientKey] = st.PublicID
		}
		return map[string]any{"op": op.Op, "statement": st.PublicID, "revisionNo": st.RevisionNo, "clientKey": op.ClientKey}, nil

	case "setEntityIRIAliases":
		eid := resolveKey(keys, op.Entity)
		if eid == "" {
			return nil, fmt.Errorf("setEntityIRIAliases requires entity")
		}
		ent, err := s.setEntityIRIAliasesOverlayInTx(ctx, tx, row, eid, op.Aliases)
		if err != nil {
			return nil, err
		}
		return map[string]any{"op": op.Op, "entity": ent.PublicID, "aliasCount": len(ent.IRIAliases)}, nil

	case "createProperty":
		dt, err := datatype.ParseType(op.Datatype)
		if err != nil {
			return nil, err
		}
		in := domain.CreatePropertyInput{
			PackageCode: op.PackageCode, Datatype: dt, Labels: op.Labels, Descriptions: op.Descriptions, IRILocal: op.IRILocal,
		}
		if op.Constraints != nil {
			in.Constraints = *op.Constraints
		}
		res, err := s.createPropertyOverlayInTx(ctx, tx, row, in)
		if err != nil {
			return nil, err
		}
		if op.ClientKey != "" {
			keys[op.ClientKey] = res.PublicID
		}
		return map[string]any{"op": op.Op, "property": res.PublicID, "revisionNo": res.RevisionNo, "clientKey": op.ClientKey}, nil

	case "createClass":
		c, err := s.createClassOverlayInTx(ctx, tx, row, domain.CreateClassInput{
			PackageCode: op.PackageCode, Labels: op.Labels, Descriptions: op.Descriptions,
			SubClassOf: resolveKey(keys, op.SubClassOf), IRILocal: op.IRILocal,
		})
		if err != nil {
			return nil, err
		}
		if op.ClientKey != "" {
			keys[op.ClientKey] = c.PublicID
		}
		return map[string]any{"op": op.Op, "class": c.PublicID, "clientKey": op.ClientKey}, nil

	case "reviseStatement":
		sid := resolveKey(keys, op.Statement)
		if sid == "" {
			return nil, fmt.Errorf("reviseStatement requires statement")
		}
		val := op.Value
		if val.EntityID != nil {
			resolved := resolveKey(keys, *val.EntityID)
			val.EntityID = &resolved
		}
		in := domain.ReviseStatementInput{
			ExpectedRevision:  op.ExpectedRevision,
			Qualifiers:        op.Qualifiers,
			ReplaceQualifiers: op.ReplaceQualifiers || len(op.Qualifiers) > 0,
			ReferenceIDs:      op.ReferenceIDs,
			ReplaceReferences: op.ReplaceReferences || len(op.ReferenceIDs) > 0,
			ValidFrom:         op.ValidFrom,
			ValidTo:           op.ValidTo,
			ReplaceValidTime:  op.ReplaceValidTime || op.ValidFrom != nil || op.ValidTo != nil,
		}
		if op.Value.Type != "" {
			v := val
			in.Value = &v
		}
		st, err := s.reviseStatementOverlayInTx(ctx, tx, row, sid, in)
		if err != nil {
			return nil, err
		}
		return map[string]any{"op": op.Op, "statement": st.PublicID, "revisionNo": st.RevisionNo}, nil

	case "deprecateStatement":
		sid := resolveKey(keys, op.Statement)
		if sid == "" {
			return nil, fmt.Errorf("deprecateStatement requires statement")
		}
		st, err := s.deprecateStatementOverlayInTx(ctx, tx, row, sid, op.ExpectedRevision)
		if err != nil {
			return nil, err
		}
		return map[string]any{"op": op.Op, "statement": st.PublicID, "revisionNo": st.RevisionNo}, nil

	case "deprecateEntity":
		eid := resolveKey(keys, op.Entity)
		if eid == "" {
			return nil, fmt.Errorf("deprecateEntity requires entity")
		}
		ent, err := s.mutateEntityStatusOverlayInTx(ctx, tx, row, eid, op.ExpectedRevision, domain.EntityDeprecated, "deprecate")
		if err != nil {
			return nil, err
		}
		return map[string]any{"op": op.Op, "entity": ent.PublicID, "revisionNo": ent.RevisionNo, "status": ent.Status}, nil

	case "deleteEntity":
		eid := resolveKey(keys, op.Entity)
		if eid == "" {
			return nil, fmt.Errorf("deleteEntity requires entity")
		}
		ent, err := s.mutateEntityStatusOverlayInTx(ctx, tx, row, eid, op.ExpectedRevision, domain.EntityDeleted, "delete")
		if err != nil {
			return nil, err
		}
		return map[string]any{"op": op.Op, "entity": ent.PublicID, "revisionNo": ent.RevisionNo, "status": ent.Status}, nil

	case "updateProperty":
		pid := resolveKey(keys, op.Entity)
		if pid == "" {
			pid = resolveKey(keys, op.Property)
		}
		if pid == "" {
			return nil, fmt.Errorf("updateProperty requires entity")
		}
		in := domain.UpdatePropertyInput{ExpectedRevision: op.ExpectedRevision}
		if op.Constraints != nil {
			in.Constraints = op.Constraints
		}
		p, err := s.updatePropertyOverlayInTx(ctx, tx, row, pid, in)
		if err != nil {
			return nil, err
		}
		return map[string]any{"op": op.Op, "property": p.PublicID, "revisionNo": p.RevisionNo}, nil

	case "moveEntity":
		eid := resolveKey(keys, op.Entity)
		if eid == "" {
			return nil, fmt.Errorf("moveEntity requires entity")
		}
		if op.PackageCode == "" {
			return nil, fmt.Errorf("moveEntity requires packageCode")
		}
		ent, err := s.moveEntityOverlayInTx(ctx, tx, row, eid, domain.MoveEntityInput{
			PackageCode: op.PackageCode, ExpectedRevision: op.ExpectedRevision,
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"op": op.Op, "entity": ent.PublicID, "packageCode": ent.PackageCode, "revisionNo": ent.RevisionNo}, nil

	default:
		return nil, fmt.Errorf("unsupported operation %q in open changeset batch", op.Op)
	}
}

func (s *Store) createEntityOverlayInTx(ctx context.Context, tx pgx.Tx, row *openChangeSetRow, in domain.CreateEntityInput) (*domain.Entity, error) {
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
		CreatedAt: now, UpdatedAt: now, IRI: publicID,
	}
	if err := s.claimObjectTx(ctx, tx, row.id, "entity", id, publicID, 0, "create"); err != nil {
		return nil, err
	}
	if err := s.upsertEntityOverlayTx(ctx, tx, row.id, ent, "", nil, ""); err != nil {
		return nil, err
	}
	return &ent, nil
}

func (s *Store) updateEntityOverlayInTx(ctx context.Context, tx pgx.Tx, row *openChangeSetRow, publicID string, in domain.UpdateEntityInput) (*domain.Entity, error) {
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
	return ent, nil
}

func (s *Store) setEntityIRIAliasesOverlayInTx(ctx context.Context, tx pgx.Tx, row *openChangeSetRow, publicID string, aliases []domain.EntityIRIAlias) (*domain.Entity, error) {
	norm, err := normalizeEntityIRIAliases(aliases)
	if err != nil {
		return nil, err
	}
	ent, baseRev, fromOverlay, err := s.resolveEntityForOpenWrite(ctx, tx, row.id, publicID)
	if err != nil {
		return nil, err
	}
	if err := errIfEntityDeleted(string(ent.Status)); err != nil {
		return nil, err
	}
	if err := s.assertAliasIRIsAvailableTx(ctx, tx, row.id, ent.ID, norm); err != nil {
		return nil, err
	}
	ent.IRIAliases = norm
	ent.UpdatedAt = time.Now().UTC()
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
	return ent, nil
}

func (s *Store) createPropertyOverlayInTx(ctx context.Context, tx pgx.Tx, row *openChangeSetRow, in domain.CreatePropertyInput) (*domain.Property, error) {
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
		CreatedAt: now, UpdatedAt: now, IRI: publicID,
		PropertyProfile: &domain.PropertyProfileInfo{Datatype: in.Datatype, Constraints: in.Constraints},
	}
	if err := s.claimObjectTx(ctx, tx, row.id, "entity", id, publicID, 0, "create"); err != nil {
		return nil, err
	}
	if err := s.upsertEntityOverlayTx(ctx, tx, row.id, ent, string(in.Datatype), constraintsJSON, ""); err != nil {
		return nil, err
	}
	return &domain.Property{
		ID: id, PublicID: publicID, Datatype: in.Datatype, Status: domain.PropertyActive,
		PackageCode: in.PackageCode, IRILocal: iriLocal, IRI: publicID,
		Labels: labels, Descriptions: descs, Constraints: in.Constraints, RevisionNo: 1,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (s *Store) createClassOverlayInTx(ctx context.Context, tx pgx.Tx, row *openChangeSetRow, in domain.CreateClassInput) (*domain.ClassDefinition, error) {
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
		CreatedAt: now, UpdatedAt: now, IRI: publicID,
		ClassProfile: &domain.ClassProfileInfo{SubClassOf: in.SubClassOf},
	}
	if err := s.claimObjectTx(ctx, tx, row.id, "entity", id, publicID, 0, "create"); err != nil {
		return nil, err
	}
	if err := s.upsertEntityOverlayTx(ctx, tx, row.id, ent, "", nil, in.SubClassOf); err != nil {
		return nil, err
	}
	return &domain.ClassDefinition{
		ID: publicID, PublicID: publicID, PackageCode: in.PackageCode, IRILocal: iriLocal, IRI: publicID,
		CanonicalEntityID: id.String(), CanonicalEntityQID: publicID,
		Status: domain.PropertyActive, Labels: labels, Descriptions: descs,
		Document:  domain.ClassDocument{SubClassOf: in.SubClassOf},
		CreatedAt: now.UTC().Format(time.RFC3339Nano),
		UpdatedAt: now.UTC().Format(time.RFC3339Nano),
	}, nil
}

func (s *Store) reviseStatementOverlayInTx(ctx context.Context, tx pgx.Tx, row *openChangeSetRow, publicID string, in domain.ReviseStatementInput) (*domain.Statement, error) {
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
	return st, nil
}

func (s *Store) deprecateStatementOverlayInTx(ctx context.Context, tx pgx.Tx, row *openChangeSetRow, publicID string, expectedRevision int) (*domain.Statement, error) {
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
	return st, nil
}

func (s *Store) mutateEntityStatusOverlayInTx(ctx context.Context, tx pgx.Tx, row *openChangeSetRow, publicID string, expectedRevision int, status domain.EntityStatus, opKind string) (*domain.Entity, error) {
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
	return ent, nil
}

func (s *Store) updatePropertyOverlayInTx(ctx context.Context, tx pgx.Tx, row *openChangeSetRow, pid string, in domain.UpdatePropertyInput) (*domain.Property, error) {
	ent, baseRev, fromOverlay, err := s.resolveEntityForOpenWrite(ctx, tx, row.id, pid)
	if err != nil {
		return nil, err
	}
	if ent.Kind != domain.EntityKindProperty || ent.PropertyProfile == nil {
		return nil, fmt.Errorf("not a property")
	}
	if in.ExpectedRevision > 0 && !fromOverlay && ent.RevisionNo != in.ExpectedRevision {
		return nil, fmt.Errorf("%w: entity revision %d expected %d", ErrConflict, ent.RevisionNo, in.ExpectedRevision)
	}
	if in.Constraints != nil {
		ent.PropertyProfile.Constraints = *in.Constraints
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
	return &domain.Property{
		ID: ent.ID, PublicID: ent.PublicID, Datatype: ent.PropertyProfile.Datatype,
		Status: domain.PropertyActive, PackageCode: ent.PackageCode, IRILocal: ent.IRILocal, IRI: ent.PublicID,
		Labels: ent.Labels, Descriptions: ent.Descriptions, Constraints: ent.PropertyProfile.Constraints,
		RevisionNo: ent.RevisionNo, CreatedAt: ent.CreatedAt, UpdatedAt: ent.UpdatedAt,
	}, nil
}

func (s *Store) moveEntityOverlayInTx(ctx context.Context, tx pgx.Tx, row *openChangeSetRow, publicID string, in domain.MoveEntityInput) (*domain.Entity, error) {
	if in.PackageCode == "" {
		return nil, fmt.Errorf("packageCode required")
	}
	if _, err := s.resolvePackageIDRequired(ctx, tx, in.PackageCode); err != nil {
		return nil, err
	}
	ent, baseRev, fromOverlay, err := s.resolveEntityForOpenWrite(ctx, tx, row.id, publicID)
	if err != nil {
		return nil, err
	}
	if err := errIfEntityDeleted(string(ent.Status)); err != nil {
		return nil, err
	}
	if in.ExpectedRevision > 0 && !fromOverlay && ent.RevisionNo != in.ExpectedRevision {
		return nil, fmt.Errorf("%w: entity revision %d expected %d", ErrConflict, ent.RevisionNo, in.ExpectedRevision)
	}
	ent.PackageCode = in.PackageCode
	ent.UpdatedAt = time.Now().UTC()
	if !fromOverlay {
		ent.RevisionNo = baseRev + 1
	}
	opKind := "move"
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
	if _, err := tx.Exec(ctx, `
		UPDATE changeset_statement_overlay
		SET package_code = $3, updated_at = $4
		WHERE changeset_id = $1 AND subject_public_id = $2
	`, row.id, ent.PublicID, in.PackageCode, time.Now().UTC()); err != nil {
		return nil, err
	}
	return ent, nil
}
