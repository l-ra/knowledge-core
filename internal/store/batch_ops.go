package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
)

func (s *Store) ApplyChangeSet(ctx context.Context, meta domain.WriteMeta, in domain.ApplyChangeSetInput) (*domain.WriteResult[domain.ChangeSet], error) {
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
		return &domain.WriteResult[domain.ChangeSet]{Value: *cs, Replay: true, ResponseRaw: hit.responseBody}, nil
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
	for _, op := range in.Operations {
		res, err := s.applyOneOp(ctx, tx, cs, meta, op, keys)
		if err != nil {
			return nil, err
		}
		results = append(results, res)
	}

	response := map[string]any{
		"changeSet": cs.publicID,
		"results":   results,
	}
	responseBody, _ := json.Marshal(response)
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
	return &domain.WriteResult[domain.ChangeSet]{
		Value: *domainCS, ChangeSet: domainCS, ResponseRaw: responseBody,
	}, nil
}

func resolveKey(keys map[string]string, ref string) string {
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "$") {
		if v, ok := keys[ref]; ok {
			return v
		}
	}
	if v, ok := keys[ref]; ok {
		return v
	}
	return ref
}

func (s *Store) applyOneOp(ctx context.Context, tx pgx.Tx, cs *changeSetTx, meta domain.WriteMeta, op domain.ChangeOperation, keys map[string]string) (map[string]any, error) {
	switch op.Op {
	case "createEntity":
		ent, err := s.createEntityInTx(ctx, tx, cs, meta, domain.CreateEntityInput{
			PackageCode: op.PackageCode, Labels: op.Labels, Descriptions: op.Descriptions, IRILocal: op.IRILocal,
		})
		if err != nil {
			return nil, err
		}
		if op.ClientKey != "" {
			keys[op.ClientKey] = ent.PublicID
		}
		return map[string]any{"op": op.Op, "entity": ent.PublicID, "revisionNo": ent.RevisionNo, "clientKey": op.ClientKey}, nil

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
		p, err := s.createPropertyInTx(ctx, tx, cs, meta, in)
		if err != nil {
			return nil, err
		}
		if op.ClientKey != "" {
			keys[op.ClientKey] = p.PublicID
		}
		return map[string]any{"op": op.Op, "property": p.PublicID, "revisionNo": p.RevisionNo, "clientKey": op.ClientKey}, nil

	case "createClass":
		c, err := s.createClassInTx(ctx, tx, cs, meta, domain.CreateClassInput{
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

	case "createStatement":
		subject := resolveKey(keys, op.Subject)
		property := resolveKey(keys, op.Property)
		val := op.Value
		if val.EntityID != nil {
			resolved := resolveKey(keys, *val.EntityID)
			val.EntityID = &resolved
		}
		st, err := s.createStatementInTx(ctx, tx, cs, meta, domain.CreateStatementInput{
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
		stRes, err := s.reviseStatementInTx(ctx, tx, cs, meta, sid, domain.ReviseStatementInput{
			Value: &val, ExpectedRevision: op.ExpectedRevision,
			Qualifiers: op.Qualifiers, ReplaceQualifiers: len(op.Qualifiers) > 0,
			ReferenceIDs: op.ReferenceIDs, ReplaceReferences: len(op.ReferenceIDs) > 0,
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"op": op.Op, "statement": stRes.PublicID, "revisionNo": stRes.RevisionNo}, nil

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
		entRes, err := s.updateEntityInTx(ctx, tx, cs, meta, eid, in)
		if err != nil {
			return nil, err
		}
		return map[string]any{"op": op.Op, "entity": entRes.PublicID, "revisionNo": entRes.RevisionNo}, nil

	case "deprecateStatement":
		sid := resolveKey(keys, op.Statement)
		if sid == "" {
			return nil, fmt.Errorf("deprecateStatement requires statement")
		}
		st, err := s.deprecateStatementInTx(ctx, tx, cs, meta, sid, op.ExpectedRevision)
		if err != nil {
			return nil, err
		}
		return map[string]any{"op": op.Op, "statement": st.PublicID, "revisionNo": st.RevisionNo}, nil

	case "deprecateEntity":
		eid := resolveKey(keys, op.Entity)
		if eid == "" {
			return nil, fmt.Errorf("deprecateEntity requires entity")
		}
		ent, err := s.deprecateEntityInTx(ctx, tx, cs, meta, eid, op.ExpectedRevision)
		if err != nil {
			return nil, err
		}
		return map[string]any{"op": op.Op, "entity": ent.PublicID, "revisionNo": ent.RevisionNo, "status": ent.Status}, nil

	case "deleteEntity":
		eid := resolveKey(keys, op.Entity)
		if eid == "" {
			return nil, fmt.Errorf("deleteEntity requires entity")
		}
		ent, err := s.deleteEntityInTx(ctx, tx, cs, meta, eid, op.ExpectedRevision)
		if err != nil {
			return nil, err
		}
		return map[string]any{"op": op.Op, "entity": ent.PublicID, "revisionNo": ent.RevisionNo, "status": ent.Status}, nil

	default:
		return nil, fmt.Errorf("unsupported operation %q", op.Op)
	}
}

func (s *Store) createEntityInTx(ctx context.Context, tx pgx.Tx, cs *changeSetTx, meta domain.WriteMeta, in domain.CreateEntityInput) (*domain.Entity, error) {
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
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		INSERT INTO entity (id, public_id, status, current_revision_no, package_id, iri_local, created_at, updated_at)
		VALUES ($1,$2,'active',1,$3,$4,$5,$5)
	`, id, publicID, pkgID, iriLocal, now)
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
	labelsJSON, _ := labelsToJSON(labels)
	descJSON, _ := labelsToJSON(descs)
	_, err = tx.Exec(ctx, `
		INSERT INTO entity_revision (id, entity_id, revision_no, status, labels, descriptions, change_set_id, actor, created_at)
		VALUES ($1,$2,1,'active',$3,$4,$5,$6,$7)
	`, datatype.NewUUID(), id, labelsJSON, descJSON, cs.id, meta.Actor, now)
	if err != nil {
		return nil, err
	}
	if err := cs.addItem(ctx, tx, "entity", id, publicID, "create", nil); err != nil {
		return nil, err
	}
	return &domain.Entity{
		ID: id, PublicID: publicID, Status: domain.EntityActive,
		Kind: domain.EntityKindEntity, PackageCode: in.PackageCode, IRILocal: iriLocal,
		Labels: labels, Descriptions: descs, RevisionNo: 1,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (s *Store) createPropertyInTx(ctx context.Context, tx pgx.Tx, cs *changeSetTx, meta domain.WriteMeta, in domain.CreatePropertyInput) (*domain.Property, error) {
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
	now := time.Now().UTC()
	constraintsJSON, _ := json.Marshal(in.Constraints)
	_, err = tx.Exec(ctx, `
		INSERT INTO entity (id, public_id, status, current_revision_no, package_id, iri_local, created_at, updated_at)
		VALUES ($1,$2,'active',1,$3,$4,$5,$5)
	`, id, publicID, pkgID, iriLocal, now)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO property_profile (entity_id, datatype, constraints) VALUES ($1,$2,$3)
	`, id, string(in.Datatype), constraintsJSON)
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
	labelsJSON, _ := labelsToJSON(labels)
	descJSON, _ := labelsToJSON(descs)
	_, err = tx.Exec(ctx, `
		INSERT INTO entity_revision (id, entity_id, revision_no, status, labels, descriptions, change_set_id, actor, created_at)
		VALUES ($1,$2,1,'active',$3,$4,$5,$6,$7)
	`, datatype.NewUUID(), id, labelsJSON, descJSON, cs.id, meta.Actor, now)
	if err != nil {
		return nil, err
	}
	if err := cs.addItem(ctx, tx, "property", id, publicID, "create", nil); err != nil {
		return nil, err
	}
	return &domain.Property{
		ID: id, PublicID: publicID, Datatype: in.Datatype, Status: domain.PropertyActive,
		Labels: labels, Descriptions: descs, Constraints: in.Constraints, RevisionNo: 1,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (s *Store) createClassInTx(ctx context.Context, tx pgx.Tx, cs *changeSetTx, meta domain.WriteMeta, in domain.CreateClassInput) (*domain.ClassDefinition, error) {
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
	if in.SubClassOf != "" {
		if _, err := datatype.ParsePublicClassID(in.SubClassOf); err != nil {
			return nil, fmt.Errorf("subClassOf: %w", err)
		}
		var exists bool
		err = tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM class_profile cp JOIN entity e ON e.id = cp.entity_id
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
	return &domain.ClassDefinition{
		ID: id.String(), PublicID: publicID, Status: domain.PropertyActive,
		PackageCode: in.PackageCode, Document: doc, Labels: labels, Descriptions: descs,
		CreatedAt: now.Format(time.RFC3339Nano), UpdatedAt: now.Format(time.RFC3339Nano),
	}, nil
}

func (s *Store) createStatementInTx(ctx context.Context, tx pgx.Tx, cs *changeSetTx, meta domain.WriteMeta, in domain.CreateStatementInput) (*domain.Statement, error) {
	var subjectID uuid.UUID
	var subjectQID string
	err := tx.QueryRow(ctx, `SELECT id, public_id FROM entity WHERE public_id = $1`, in.SubjectPublicID).
		Scan(&subjectID, &subjectQID)
	if err != nil {
		return nil, fmt.Errorf("subject: %w", err)
	}
	var propertyID uuid.UUID
	var propertyPID, dt string
	err = tx.QueryRow(ctx, `
		SELECT e.id, e.public_id, pp.datatype
		FROM property_profile pp JOIN entity e ON e.id = pp.entity_id
		WHERE e.public_id = $1 AND e.status <> 'deleted'
	`, in.PropertyPublicID).Scan(&propertyID, &propertyPID, &dt)
	if err != nil {
		return nil, fmt.Errorf("property: %w", err)
	}
	dtype := datatype.Type(dt)
	_, sv, err := s.resolveAndEncodeValue(ctx, tx, dtype, in.Value)
	if err != nil {
		return nil, err
	}
	if err := validateValidTime(in.ValidFrom, in.ValidTo); err != nil {
		return nil, err
	}
	if in.Upsert {
		dup, err := s.findDuplicateStatementTx(ctx, tx, subjectID, propertyID, sv)
		if err == nil && dup != "" {
			st, err := s.getStatementTx(ctx, tx, dup)
			if err != nil {
				return nil, err
			}
			if err := s.enrichStatement(ctx, tx, st); err != nil {
				return nil, err
			}
			return st, nil
		}
		if err != nil && err != pgx.ErrNoRows {
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
	_, err = tx.Exec(ctx, `
		INSERT INTO statement (
			id, public_id, subject_id, property_id, status, value_type,
			value_bool, value_int64, value_numeric, value_date, value_timestamptz,
			value_text, value_entity_id, value_json, valid_from, valid_to,
			current_revision_no, package_id, created_at, updated_at
		) VALUES (
			$1,$2,$3,$4,'active',$5,
			$6,$7,$8,$9,$10,
			$11,$12,$13,$14,$15,1,$16,$17,$17
		)
	`, id, publicID, subjectID, propertyID, sv.Type,
		sv.Bool, sv.Int64, sv.Numeric, sv.Date, sv.Timestamptz,
		sv.Text, sv.EntityID, sv.JSON, in.ValidFrom, in.ValidTo, pkgID, now)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO statement_current (
			statement_id, public_id, subject_id, property_id, value_type,
			value_bool, value_int64, value_numeric, value_date, value_timestamptz,
			value_text, value_entity_id, value_json, valid_from, valid_to, updated_at
		) VALUES (
			$1,$2,$3,$4,$5,
			$6,$7,$8,$9,$10,
			$11,$12,$13,$14,$15,$16
		)
	`, id, publicID, subjectID, propertyID, sv.Type,
		sv.Bool, sv.Int64, sv.Numeric, sv.Date, sv.Timestamptz,
		sv.Text, sv.EntityID, sv.JSON, in.ValidFrom, in.ValidTo, now)
	if err != nil {
		return nil, err
	}
	if len(in.Qualifiers) > 0 {
		if err := s.replaceStatementQualifiers(ctx, tx, id, in.Qualifiers); err != nil {
			return nil, err
		}
	}
	if len(in.ReferenceIDs) > 0 {
		if err := s.replaceStatementReferences(ctx, tx, id, in.ReferenceIDs); err != nil {
			return nil, err
		}
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO statement_revision (
			id, statement_id, revision_no, status, value_type,
			value_bool, value_int64, value_numeric, value_date, value_timestamptz,
			value_text, value_entity_id, value_json, valid_from, valid_to,
			change_set_id, actor, created_at
		) VALUES (
			$1,$2,1,'active',$3,
			$4,$5,$6,$7,$8,
			$9,$10,$11,$12,$13,
			$14,$15,$16
		)
	`, datatype.NewUUID(), id, sv.Type,
		sv.Bool, sv.Int64, sv.Numeric, sv.Date, sv.Timestamptz,
		sv.Text, sv.EntityID, sv.JSON, in.ValidFrom, in.ValidTo,
		cs.id, meta.Actor, now)
	if err != nil {
		return nil, err
	}
	if err := s.snapshotStatementProvenance(ctx, tx, id, 1); err != nil {
		return nil, err
	}
	if err := cs.addItem(ctx, tx, "statement", id, publicID, "create", nil); err != nil {
		return nil, err
	}
	st, err := s.getStatementTx(ctx, tx, publicID)
	if err != nil {
		return nil, err
	}
	st.RevisionNo = 1
	if err := s.enrichStatement(ctx, tx, st); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Store) deprecateStatementInTx(ctx context.Context, tx pgx.Tx, cs *changeSetTx, meta domain.WriteMeta, publicID string, expectedRevision int) (*domain.Statement, error) {
	if err := s.assertNotManagedPackageCodeStatementTx(ctx, tx, publicID); err != nil {
		return nil, err
	}
	var statementID uuid.UUID
	var status string
	err := tx.QueryRow(ctx, `SELECT id, status FROM statement WHERE public_id = $1`, publicID).Scan(&statementID, &status)
	if err != nil {
		return nil, fmt.Errorf("statement: %w", err)
	}
	if status != string(domain.StatementActive) {
		return nil, fmt.Errorf("%w: statement is not active", ErrNotActive)
	}
	currentRev, err := s.assertStatementRevision(ctx, tx, statementID, expectedRevision)
	if err != nil {
		return nil, err
	}
	nextRev := currentRev + 1
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		UPDATE statement SET status = $2, current_revision_no = $3, updated_at = $4 WHERE id = $1
	`, statementID, domain.StatementDeprecated, nextRev, now)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `DELETE FROM statement_current WHERE statement_id = $1`, statementID)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO statement_revision (
			id, statement_id, revision_no, status, value_type,
			value_bool, value_int64, value_numeric, value_date, value_timestamptz,
			value_text, value_entity_id, value_json, valid_from, valid_to,
			change_set_id, actor, created_at
		)
		SELECT $1, st.id, $2, $3, st.value_type,
			st.value_bool, st.value_int64, st.value_numeric, st.value_date, st.value_timestamptz,
			st.value_text, st.value_entity_id, st.value_json, st.valid_from, st.valid_to,
			$4, $5, $6
		FROM statement st WHERE st.id = $7
	`, datatype.NewUUID(), nextRev, domain.StatementDeprecated, cs.id, meta.Actor, now, statementID)
	if err != nil {
		return nil, err
	}
	if err := cs.addItem(ctx, tx, "statement", statementID, publicID, "deprecate", nil); err != nil {
		return nil, err
	}
	st, err := s.getStatementTx(ctx, tx, publicID)
	if err != nil {
		return nil, err
	}
	st.RevisionNo = nextRev
	st.Status = domain.StatementDeprecated
	return st, nil
}
