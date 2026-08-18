package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
	"github.com/shopspring/decimal"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Pool() *pgxpool.Pool { return s.pool }

func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

func (s *Store) nextPublicID(ctx context.Context, tx pgx.Tx, kind, prefix string) (string, error) {
	var n int64
	err := tx.QueryRow(ctx, `
		UPDATE id_counter SET last_value = last_value + 1
		WHERE kind = $1
		RETURNING last_value
	`, kind).Scan(&n)
	if err != nil {
		return "", fmt.Errorf("allocate %s id: %w", kind, err)
	}
	return fmt.Sprintf("%s%d", prefix, n), nil
}

func (s *Store) CreateEntity(ctx context.Context, meta domain.WriteMeta, in domain.CreateEntityInput) (*domain.WriteResult[domain.Entity], error) {
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

	if hit, err := s.checkIdempotency(ctx, tx, meta); err != nil {
		return nil, err
	} else if hit != nil {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		var ent domain.Entity
		if err := json.Unmarshal(hit.responseBody, &ent); err != nil {
			return nil, err
		}
		return &domain.WriteResult[domain.Entity]{Value: ent, Replay: true, ResponseRaw: hit.responseBody}, nil
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
		VALUES ($1, $2, 'active', 1, $3, $4, $5, $5)
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

	cs, err := s.beginChangeSetTx(ctx, tx, meta)
	if err != nil {
		return nil, err
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

	ent := domain.Entity{
		ID: id, PublicID: publicID, Status: domain.EntityActive,
		Kind: domain.EntityKindEntity, PackageCode: in.PackageCode,
		IRILocal: iriLocal,
		Labels:   labels, Descriptions: descs, RevisionNo: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.fillEntityIRI(ctx, &ent); err != nil {
		return nil, err
	}
	if err := s.finalizeChangeSet(ctx, tx, cs, ent); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Entity]{Value: ent, ChangeSet: s.changeSetDomain(cs, meta)}, nil
}

func (s *Store) GetEntityByPublicID(ctx context.Context, qid string) (*domain.Entity, error) {
	var e domain.Entity
	var pkgCode *string
	err := s.pool.QueryRow(ctx, `
		SELECT e.id, e.public_id, e.status, e.current_revision_no, e.created_at, e.updated_at, p.code, COALESCE(e.iri_local,'')
		FROM entity e
		LEFT JOIN package p ON p.id = e.package_id
		WHERE e.public_id = $1
	`, qid).Scan(&e.ID, &e.PublicID, &e.Status, &e.RevisionNo, &e.CreatedAt, &e.UpdatedAt, &pkgCode, &e.IRILocal)
	if err != nil {
		return nil, err
	}
	if pkgCode != nil {
		e.PackageCode = *pkgCode
	}
	e.Labels, err = s.loadLabels(ctx, `SELECT lang, text FROM entity_label WHERE entity_id = $1`, e.ID)
	if err != nil {
		return nil, err
	}
	e.Descriptions, err = s.loadLabels(ctx, `SELECT lang, text FROM entity_description WHERE entity_id = $1`, e.ID)
	if err != nil {
		return nil, err
	}
	if err := s.attachEntityProfiles(ctx, &e); err != nil {
		return nil, err
	}
	if err := s.fillEntityIRI(ctx, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *Store) attachEntityProfiles(ctx context.Context, e *domain.Entity) error {
	e.Kind = domain.EntityKindEntity
	var dt string
	var constraintsJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT datatype, constraints FROM property_profile WHERE entity_id = $1
	`, e.ID).Scan(&dt, &constraintsJSON)
	if err == nil {
		e.Kind = domain.EntityKindProperty
		info := &domain.PropertyProfileInfo{Datatype: datatype.Type(dt)}
		if len(constraintsJSON) > 0 {
			_ = json.Unmarshal(constraintsJSON, &info.Constraints)
		}
		e.PropertyProfile = info
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var docJSON []byte
	err = s.pool.QueryRow(ctx, `SELECT document FROM class_profile WHERE entity_id = $1`, e.ID).Scan(&docJSON)
	if err == nil {
		e.Kind = domain.EntityKindClass
		info := &domain.ClassProfileInfo{}
		var doc domain.ClassDocument
		_ = json.Unmarshal(docJSON, &doc)
		info.SubClassOf = doc.SubClassOf
		e.ClassProfile = info
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return nil
}

func (s *Store) CreateProperty(ctx context.Context, meta domain.WriteMeta, in domain.CreatePropertyInput) (*domain.WriteResult[domain.Property], error) {
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

	if hit, err := s.checkIdempotency(ctx, tx, meta); err != nil {
		return nil, err
	} else if hit != nil {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		var p domain.Property
		if err := json.Unmarshal(hit.responseBody, &p); err != nil {
			return nil, err
		}
		return &domain.WriteResult[domain.Property]{Value: p, Replay: true, ResponseRaw: hit.responseBody}, nil
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
		INSERT INTO property_profile (entity_id, datatype, constraints)
		VALUES ($1,$2,$3)
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

	cs, err := s.beginChangeSetTx(ctx, tx, meta)
	if err != nil {
		return nil, err
	}
	labelsJSON, _ := labelsToJSON(labels)
	descJSON, _ := labelsToJSON(descs)
	payload, _ := json.Marshal(map[string]any{"datatype": in.Datatype, "constraints": in.Constraints})
	_, err = tx.Exec(ctx, `
		INSERT INTO entity_revision (id, entity_id, revision_no, status, labels, descriptions, change_set_id, actor, created_at)
		VALUES ($1,$2,1,'active',$3,$4,$5,$6,$7)
	`, datatype.NewUUID(), id, labelsJSON, descJSON, cs.id, meta.Actor, now)
	if err != nil {
		return nil, err
	}
	if err := cs.addItem(ctx, tx, "property", id, publicID, "create", payload); err != nil {
		return nil, err
	}

	p := domain.Property{
		ID: id, PublicID: publicID, Datatype: in.Datatype, Status: domain.PropertyActive,
		PackageCode: in.PackageCode, IRILocal: iriLocal,
		Labels: labels, Descriptions: descs, Constraints: in.Constraints, RevisionNo: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	if err := s.fillPropertyIRI(ctx, &p); err != nil {
		return nil, err
	}
	if err := s.finalizeChangeSet(ctx, tx, cs, p); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Property]{Value: p, ChangeSet: s.changeSetDomain(cs, meta)}, nil
}

func (s *Store) GetPropertyByPublicID(ctx context.Context, pid string) (*domain.Property, error) {
	var p domain.Property
	var dt string
	var constraintsJSON []byte
	var pkgCode *string
	var iriBase string
	err := s.pool.QueryRow(ctx, `
		SELECT e.id, e.public_id, pp.datatype, e.status, e.current_revision_no, pp.constraints,
			e.created_at, e.updated_at, pkg.code, COALESCE(e.iri_local,''), COALESCE(pkg.iri_base,'')
		FROM property_profile pp
		JOIN entity e ON e.id = pp.entity_id
		LEFT JOIN package pkg ON pkg.id = e.package_id
		WHERE e.public_id = $1 AND e.status <> 'deleted'
	`, pid).Scan(&p.ID, &p.PublicID, &dt, &p.Status, &p.RevisionNo, &constraintsJSON, &p.CreatedAt, &p.UpdatedAt, &pkgCode, &p.IRILocal, &iriBase)
	if err != nil {
		return nil, err
	}
	if pkgCode != nil {
		p.PackageCode = *pkgCode
	}
	p.Datatype = datatype.Type(dt)
	if len(constraintsJSON) > 0 {
		_ = json.Unmarshal(constraintsJSON, &p.Constraints)
	}
	p.Labels, err = s.loadLabels(ctx, `SELECT lang, text FROM entity_label WHERE entity_id = $1`, p.ID)
	if err != nil {
		return nil, err
	}
	p.Descriptions, err = s.loadLabels(ctx, `SELECT lang, text FROM entity_description WHERE entity_id = $1`, p.ID)
	if err != nil {
		return nil, err
	}
	p.IRI = datatype.ResolveIRI(iriBase, p.IRILocal, p.PublicID, rdfNSProperty)
	return &p, nil
}

func (s *Store) fillPropertyIRI(ctx context.Context, p *domain.Property) error {
	if p == nil {
		return nil
	}
	var base string
	if p.PackageCode != "" {
		_ = s.pool.QueryRow(ctx, `SELECT COALESCE(iri_base,'') FROM package WHERE code = $1`, p.PackageCode).Scan(&base)
	}
	p.IRI = datatype.ResolveIRI(base, p.IRILocal, p.PublicID, rdfNSProperty)
	return nil
}

type storedValue struct {
	Type        string
	Bool        *bool
	Int64       *int64
	Numeric     *decimal.Decimal
	Date        *time.Time
	Timestamptz *time.Time
	Text        *string
	EntityID    *uuid.UUID
	JSON        []byte
}

func encodeValue(dt datatype.Type, v datatype.Value) (storedValue, error) {
	if err := datatype.Validate(dt, v); err != nil {
		return storedValue{}, err
	}
	concrete := dt
	if dt == datatype.Any {
		concrete = v.Type
	}
	out := storedValue{Type: string(concrete)}
	switch concrete {
	case datatype.EntityReference:
		id, err := uuid.Parse(*v.EntityID)
		if err != nil {
			// may be public Q id resolved by caller — store layer expects UUID string in EntityID after resolve
			return storedValue{}, fmt.Errorf("entityId must be UUID at store layer: %w", err)
		}
		out.EntityID = &id
	case datatype.String, datatype.URI:
		var t string
		if dt == datatype.String {
			t = *v.String
		} else {
			t = *v.URI
		}
		out.Text = &t
	case datatype.Boolean:
		out.Bool = v.Bool
	case datatype.Integer:
		out.Int64 = v.Int64
	case datatype.Decimal:
		d, err := decimal.NewFromString(*v.Decimal)
		if err != nil {
			return storedValue{}, err
		}
		out.Numeric = &d
	case datatype.Date:
		t, err := time.Parse("2006-01-02", *v.Date)
		if err != nil {
			return storedValue{}, err
		}
		out.Date = &t
	case datatype.DateTime:
		t, err := time.Parse(time.RFC3339, *v.DateTime)
		if err != nil {
			return storedValue{}, err
		}
		t = t.UTC()
		out.Timestamptz = &t
	case datatype.LocalizedString:
		b, err := json.Marshal(v.LangMap)
		if err != nil {
			return storedValue{}, err
		}
		out.JSON = b
	case datatype.ExternalIdentifier:
		b, err := json.Marshal(map[string]string{"scheme": *v.Scheme, "value": *v.ExtID})
		if err != nil {
			return storedValue{}, err
		}
		out.JSON = b
	case datatype.Quantity:
		b, err := json.Marshal(map[string]string{
			"quantityValue": *v.QuantityValue,
			"unitEntityId":  *v.UnitEntityID,
		})
		if err != nil {
			return storedValue{}, err
		}
		out.JSON = b
	case datatype.Interval:
		b, err := json.Marshal(map[string]json.RawMessage{
			"from": rawOrNull(v.IntervalFrom),
			"to":   rawOrNull(v.IntervalTo),
		})
		if err != nil {
			return storedValue{}, err
		}
		out.JSON = b
	}
	return out, nil
}

func rawOrNull(r *json.RawMessage) json.RawMessage {
	if r == nil {
		return json.RawMessage("null")
	}
	return *r
}

func decodeValue(sv storedValue) (datatype.Value, error) {
	v := datatype.Value{Type: datatype.Type(sv.Type)}
	switch v.Type {
	case datatype.EntityReference:
		if sv.EntityID == nil {
			return v, fmt.Errorf("missing entity id")
		}
		s := sv.EntityID.String()
		v.EntityID = &s
	case datatype.String:
		v.String = sv.Text
	case datatype.URI:
		v.URI = sv.Text
	case datatype.Boolean:
		v.Bool = sv.Bool
	case datatype.Integer:
		v.Int64 = sv.Int64
	case datatype.Decimal:
		if sv.Numeric == nil {
			return v, fmt.Errorf("missing numeric")
		}
		s := sv.Numeric.String()
		v.Decimal = &s
	case datatype.Date:
		if sv.Date == nil {
			return v, fmt.Errorf("missing date")
		}
		s := sv.Date.Format("2006-01-02")
		v.Date = &s
	case datatype.DateTime:
		if sv.Timestamptz == nil {
			return v, fmt.Errorf("missing timestamptz")
		}
		s := sv.Timestamptz.UTC().Format(time.RFC3339)
		v.DateTime = &s
	case datatype.LocalizedString:
		if err := json.Unmarshal(sv.JSON, &v.LangMap); err != nil {
			return v, err
		}
	case datatype.ExternalIdentifier:
		var m map[string]string
		if err := json.Unmarshal(sv.JSON, &m); err != nil {
			return v, err
		}
		scheme, val := m["scheme"], m["value"]
		v.Scheme, v.ExtID = &scheme, &val
	case datatype.Quantity:
		var m map[string]string
		if err := json.Unmarshal(sv.JSON, &m); err != nil {
			return v, err
		}
		qv, u := m["quantityValue"], m["unitEntityId"]
		v.QuantityValue, v.UnitEntityID = &qv, &u
	case datatype.Interval:
		var m map[string]json.RawMessage
		if err := json.Unmarshal(sv.JSON, &m); err != nil {
			return v, err
		}
		if f, ok := m["from"]; ok && string(f) != "null" {
			v.IntervalFrom = &f
		}
		if t, ok := m["to"]; ok && string(t) != "null" {
			v.IntervalTo = &t
		}
	}
	return v, nil
}

func (s *Store) CreateStatement(ctx context.Context, meta domain.WriteMeta, in domain.CreateStatementInput) (*domain.WriteResult[domain.Statement], error) {
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
		var st domain.Statement
		if err := json.Unmarshal(hit.responseBody, &st); err != nil {
			return nil, err
		}
		return &domain.WriteResult[domain.Statement]{Value: st, Replay: true, ResponseRaw: hit.responseBody}, nil
	}

	var subjectID uuid.UUID
	var subjectQID string
	err = tx.QueryRow(ctx, `SELECT id, public_id FROM entity WHERE public_id = $1`, in.SubjectPublicID).
		Scan(&subjectID, &subjectQID)
	if err != nil {
		return nil, fmt.Errorf("subject: %w", err)
	}

	var propertyID uuid.UUID
	var propertyPID, dt string
	err = tx.QueryRow(ctx, `
		SELECT e.id, e.public_id, pp.datatype
		FROM property_profile pp
		JOIN entity e ON e.id = pp.entity_id
		WHERE e.public_id = $1 AND e.status <> 'deleted'
	`, in.PropertyPublicID).
		Scan(&propertyID, &propertyPID, &dt)
	if err != nil {
		return nil, fmt.Errorf("property: %w", err)
	}
	dtype := datatype.Type(dt)

	val := in.Value
	val, sv, err := s.resolveAndEncodeValue(ctx, tx, dtype, val)
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
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			return &domain.WriteResult[domain.Statement]{Value: *st, Replay: true}, nil
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

	cs, err := s.beginChangeSetTx(ctx, tx, meta)
	if err != nil {
		return nil, err
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
			$9,$10,$11,$12,$13,$14,$15,$16
		)
	`, datatype.NewUUID(), id, sv.Type,
		sv.Bool, sv.Int64, sv.Numeric, sv.Date, sv.Timestamptz,
		sv.Text, sv.EntityID, sv.JSON, in.ValidFrom, in.ValidTo, cs.id, meta.Actor, now)
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
	if err := s.finalizeChangeSet(ctx, tx, cs, st); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Statement]{Value: *st, ChangeSet: s.changeSetDomain(cs, meta)}, nil
}

func (s *Store) GetStatementByPublicID(ctx context.Context, sid string) (*domain.Statement, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT st.id, st.public_id, st.subject_id, e.public_id, st.property_id, p.public_id, st.status,
			st.value_type, st.value_bool, st.value_int64, st.value_numeric, st.value_date, st.value_timestamptz,
			st.value_text, st.value_entity_id, st.value_json, st.valid_from, st.valid_to,
			st.current_revision_no, st.created_at, st.updated_at
		FROM statement st
		JOIN entity e ON e.id = st.subject_id
		JOIN property_profile pp ON pp.entity_id = st.property_id
		JOIN entity p ON p.id = pp.entity_id
		WHERE st.public_id = $1
	`, sid)
	st, err := scanStatement(row)
	if err != nil {
		return nil, err
	}
	if err := s.enrichStatement(ctx, s.pool, st); err != nil {
		return nil, err
	}
	return st, nil
}

type StatementListOptions struct {
	Limit       int
	Cursor      string
	SubjectQID  string
	PropertyPID string
	ObjectQID   string
}

func (s *Store) ListStatements(ctx context.Context, opt StatementListOptions) ([]domain.Statement, string, error) {
	if opt.Limit <= 0 {
		opt.Limit = 50
	}
	if opt.Limit > 200 {
		opt.Limit = 200
	}
	where := []string{`st.status = 'active'`}
	args := []any{}
	if qid := strings.TrimSpace(opt.SubjectQID); qid != "" {
		args = append(args, qid)
		where = append(where, `sub.public_id = $`+strconv.Itoa(len(args)))
	}
	if pid := strings.TrimSpace(opt.PropertyPID); pid != "" {
		args = append(args, pid)
		where = append(where, `p.public_id = $`+strconv.Itoa(len(args)))
	}
	if qid := strings.TrimSpace(opt.ObjectQID); qid != "" {
		args = append(args, qid)
		where = append(where, `obj.public_id = $`+strconv.Itoa(len(args)))
	}
	if cursor := strings.TrimSpace(opt.Cursor); cursor != "" {
		args = append(args, cursor)
		where = append(where, `st.public_id > $`+strconv.Itoa(len(args)))
	}
	args = append(args, opt.Limit+1)
	limitArg := `$` + strconv.Itoa(len(args))
	rows, err := s.pool.Query(ctx, `
		SELECT st.id, st.public_id, st.subject_id, sub.public_id, st.property_id, p.public_id, st.status,
			st.value_type, st.value_bool, st.value_int64, st.value_numeric, st.value_date, st.value_timestamptz,
			st.value_text, st.value_entity_id, st.value_json, st.valid_from, st.valid_to,
			st.current_revision_no, st.created_at, st.updated_at
		FROM statement_current sc
		JOIN statement st ON st.id = sc.statement_id
		JOIN entity sub ON sub.id = st.subject_id
		JOIN property_profile pp ON pp.entity_id = st.property_id
		JOIN entity p ON p.id = pp.entity_id
		LEFT JOIN entity obj ON obj.id = sc.value_entity_id
		WHERE `+strings.Join(where, ` AND `)+`
		ORDER BY st.public_id
		LIMIT `+limitArg, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := make([]domain.Statement, 0, opt.Limit)
	next := ""
	for rows.Next() {
		st, err := scanStatement(rows)
		if err != nil {
			return nil, "", err
		}
		if err := s.enrichStatement(ctx, s.pool, st); err != nil {
			return nil, "", err
		}
		if len(out) == opt.Limit {
			next = st.PublicID
			break
		}
		out = append(out, *st)
	}
	return out, next, rows.Err()
}

func (s *Store) ListStatementsBySubject(ctx context.Context, qid string, propertyPID string) ([]domain.Statement, error) {
	out, _, err := s.ListStatements(ctx, StatementListOptions{Limit: 200, SubjectQID: qid, PropertyPID: propertyPID})
	return out, err
}

type scannable interface {
	Scan(dest ...any) error
}

func scanStatement(row scannable) (*domain.Statement, error) {
	var st domain.Statement
	var sv storedValue
	var numeric *decimal.Decimal
	err := row.Scan(
		&st.ID, &st.PublicID, &st.SubjectID, &st.SubjectQID, &st.PropertyID, &st.PropertyPID, &st.Status,
		&sv.Type, &sv.Bool, &sv.Int64, &numeric, &sv.Date, &sv.Timestamptz,
		&sv.Text, &sv.EntityID, &sv.JSON, &st.ValidFrom, &st.ValidTo,
		&st.RevisionNo, &st.CreatedAt, &st.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	sv.Numeric = numeric
	val, err := decodeValue(sv)
	if err != nil {
		return nil, err
	}
	st.Value = val
	return &st, nil
}

func (s *Store) loadLabels(ctx context.Context, q string, id uuid.UUID) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, q, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var lang, text string
		if err := rows.Scan(&lang, &text); err != nil {
			return nil, err
		}
		out[lang] = text
	}
	return out, rows.Err()
}
