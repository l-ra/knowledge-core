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
	"github.com/shopspring/decimal"
)

func validateValidTime(from, to *time.Time) error {
	if from != nil && to != nil && from.After(*to) {
		return fmt.Errorf("validFrom must be before or equal to validTo")
	}
	return nil
}

func (s *Store) CreateReference(ctx context.Context, meta domain.WriteMeta, in domain.CreateReferenceInput) (*domain.WriteResult[domain.Reference], error) {
	fields := in.Fields
	if fields == nil {
		fields = map[string]any{}
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
		var ref domain.Reference
		if err := json.Unmarshal(hit.responseBody, &ref); err != nil {
			return nil, err
		}
		return &domain.WriteResult[domain.Reference]{Value: ref, Replay: true, ResponseRaw: hit.responseBody}, nil
	}

	id := datatype.NewUUID()
	publicID, err := s.nextPublicID(ctx, tx, "reference", "R")
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	fieldsJSON, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO reference (id, public_id, fields, created_at) VALUES ($1,$2,$3,$4)
	`, id, publicID, fieldsJSON, now)
	if err != nil {
		return nil, err
	}

	cs, err := s.beginChangeSetTx(ctx, tx, meta)
	if err != nil {
		return nil, err
	}
	if err := cs.addItem(ctx, tx, "reference", id, publicID, "create", nil); err != nil {
		return nil, err
	}

	ref := domain.Reference{ID: id, PublicID: publicID, Fields: fields, CreatedAt: now}
	if err := s.finalizeChangeSet(ctx, tx, cs, ref); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Reference]{Value: ref, ChangeSet: s.changeSetDomain(cs, meta)}, nil
}

func (s *Store) GetReferenceByPublicID(ctx context.Context, rid string) (*domain.Reference, error) {
	var ref domain.Reference
	var fieldsJSON []byte
	err := s.pool.QueryRow(ctx, `
		SELECT id, public_id, fields, created_at FROM reference WHERE public_id = $1
	`, rid).Scan(&ref.ID, &ref.PublicID, &fieldsJSON, &ref.CreatedAt)
	if err != nil {
		return nil, err
	}
	if len(fieldsJSON) > 0 {
		_ = json.Unmarshal(fieldsJSON, &ref.Fields)
	}
	if ref.Fields == nil {
		ref.Fields = map[string]any{}
	}
	return &ref, nil
}

func (s *Store) replaceStatementQualifiers(ctx context.Context, tx pgx.Tx, statementID uuid.UUID, inputs []domain.QualifierInput) error {
	_, err := tx.Exec(ctx, `DELETE FROM statement_qualifier WHERE statement_id = $1`, statementID)
	if err != nil {
		return err
	}
	for _, qin := range inputs {
		var propertyID uuid.UUID
		var dt string
		err := tx.QueryRow(ctx, `SELECT id, datatype FROM property_definition WHERE public_id = $1`, qin.Property).
			Scan(&propertyID, &dt)
		if err != nil {
			return fmt.Errorf("qualifier property %s: %w", qin.Property, err)
		}
		val := qin.Value
		_, sv, err := s.resolveAndEncodeValue(ctx, tx, datatype.Type(dt), val)
		if err != nil {
			return fmt.Errorf("qualifier value: %w", err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO statement_qualifier (
				id, statement_id, property_id, value_type,
				value_bool, value_int64, value_numeric, value_date, value_timestamptz,
				value_text, value_entity_id, value_json
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		`, datatype.NewUUID(), statementID, propertyID, sv.Type,
			sv.Bool, sv.Int64, sv.Numeric, sv.Date, sv.Timestamptz, sv.Text, sv.EntityID, sv.JSON)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) replaceStatementReferences(ctx context.Context, tx pgx.Tx, statementID uuid.UUID, referencePublicIDs []string) error {
	_, err := tx.Exec(ctx, `DELETE FROM statement_reference WHERE statement_id = $1`, statementID)
	if err != nil {
		return err
	}
	for _, rid := range referencePublicIDs {
		var refID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM reference WHERE public_id = $1`, rid).Scan(&refID)
		if err != nil {
			return fmt.Errorf("reference %s: %w", rid, err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO statement_reference (statement_id, reference_id) VALUES ($1,$2)
		`, statementID, refID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) snapshotStatementProvenance(ctx context.Context, tx pgx.Tx, statementID uuid.UUID, revisionNo int) error {
	_, err := tx.Exec(ctx, `
		DELETE FROM statement_revision_qualifier WHERE statement_id = $1 AND revision_no = $2
	`, statementID, revisionNo)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		DELETE FROM statement_revision_reference WHERE statement_id = $1 AND revision_no = $2
	`, statementID, revisionNo)
	if err != nil {
		return err
	}

	rows, err := tx.Query(ctx, `
		SELECT property_id, value_type,
			value_bool, value_int64, value_numeric, value_date, value_timestamptz,
			value_text, value_entity_id, value_json
		FROM statement_qualifier WHERE statement_id = $1
	`, statementID)
	if err != nil {
		return err
	}
	type qualSnap struct {
		propertyID uuid.UUID
		sv         storedValue
	}
	var snaps []qualSnap
	for rows.Next() {
		var snap qualSnap
		var numeric *decimal.Decimal
		if err := rows.Scan(
			&snap.propertyID, &snap.sv.Type,
			&snap.sv.Bool, &snap.sv.Int64, &numeric, &snap.sv.Date, &snap.sv.Timestamptz,
			&snap.sv.Text, &snap.sv.EntityID, &snap.sv.JSON,
		); err != nil {
			rows.Close()
			return err
		}
		snap.sv.Numeric = numeric
		snaps = append(snaps, snap)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, snap := range snaps {
		_, err = tx.Exec(ctx, `
			INSERT INTO statement_revision_qualifier (
				id, statement_id, revision_no, property_id, value_type,
				value_bool, value_int64, value_numeric, value_date, value_timestamptz,
				value_text, value_entity_id, value_json
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		`, datatype.NewUUID(), statementID, revisionNo, snap.propertyID, snap.sv.Type,
			snap.sv.Bool, snap.sv.Int64, snap.sv.Numeric, snap.sv.Date, snap.sv.Timestamptz,
			snap.sv.Text, snap.sv.EntityID, snap.sv.JSON)
		if err != nil {
			return err
		}
	}

	refRows, err := tx.Query(ctx, `SELECT reference_id FROM statement_reference WHERE statement_id = $1`, statementID)
	if err != nil {
		return err
	}
	var refIDs []uuid.UUID
	for refRows.Next() {
		var refID uuid.UUID
		if err := refRows.Scan(&refID); err != nil {
			refRows.Close()
			return err
		}
		refIDs = append(refIDs, refID)
	}
	if err := refRows.Err(); err != nil {
		refRows.Close()
		return err
	}
	refRows.Close()

	for _, refID := range refIDs {
		_, err = tx.Exec(ctx, `
			INSERT INTO statement_revision_reference (statement_id, revision_no, reference_id)
			VALUES ($1,$2,$3)
		`, statementID, revisionNo, refID)
		if err != nil {
			return err
		}
	}
	return nil
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (s *Store) loadStatementProvenance(ctx context.Context, q querier, statementID uuid.UUID) ([]domain.Qualifier, []string, error) {
	return s.loadStatementProvenanceAtRevision(ctx, q, statementID, 0)
}

func (s *Store) loadStatementProvenanceAtRevision(ctx context.Context, q querier, statementID uuid.UUID, revisionNo int) ([]domain.Qualifier, []string, error) {
	var qualQuery string
	var qualArgs []any
	if revisionNo > 0 {
		qualQuery = `
			SELECT p.public_id, sq.property_id, sq.value_type,
				sq.value_bool, sq.value_int64, sq.value_numeric, sq.value_date, sq.value_timestamptz,
				sq.value_text, sq.value_entity_id, sq.value_json
			FROM statement_revision_qualifier sq
			JOIN property_definition p ON p.id = sq.property_id
			WHERE sq.statement_id = $1 AND sq.revision_no = $2
		`
		qualArgs = []any{statementID, revisionNo}
	} else {
		qualQuery = `
			SELECT p.public_id, sq.property_id, sq.value_type,
				sq.value_bool, sq.value_int64, sq.value_numeric, sq.value_date, sq.value_timestamptz,
				sq.value_text, sq.value_entity_id, sq.value_json
		 FROM statement_qualifier sq
		 JOIN property_definition p ON p.id = sq.property_id
		 WHERE sq.statement_id = $1
		`
		qualArgs = []any{statementID}
	}

	rows, err := q.Query(ctx, qualQuery, qualArgs...)
	if err != nil {
		return nil, nil, err
	}
	var quals []domain.Qualifier
	for rows.Next() {
		var qual domain.Qualifier
		var sv storedValue
		var numeric *decimal.Decimal
		if err := rows.Scan(
			&qual.PropertyPID, &qual.PropertyID, &sv.Type,
			&sv.Bool, &sv.Int64, &numeric, &sv.Date, &sv.Timestamptz,
			&sv.Text, &sv.EntityID, &sv.JSON,
		); err != nil {
			return nil, nil, err
		}
		sv.Numeric = numeric
		val, err := decodeValue(sv)
		if err != nil {
			return nil, nil, err
		}
		qual.Value = val
		quals = append(quals, qual)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, nil, err
	}
	rows.Close()

	var refQuery string
	var refArgs []any
	if revisionNo > 0 {
		refQuery = `
			SELECT r.public_id FROM statement_revision_reference srr
			JOIN reference r ON r.id = srr.reference_id
			WHERE srr.statement_id = $1 AND srr.revision_no = $2 ORDER BY r.public_id
		`
		refArgs = []any{statementID, revisionNo}
	} else {
		refQuery = `
			SELECT r.public_id FROM statement_reference sr
			JOIN reference r ON r.id = sr.reference_id
			WHERE sr.statement_id = $1 ORDER BY r.public_id
		`
		refArgs = []any{statementID}
	}
	refRows, err := q.Query(ctx, refQuery, refArgs...)
	if err != nil {
		return nil, nil, err
	}
	var refs []string
	for refRows.Next() {
		var rid string
		if err := refRows.Scan(&rid); err != nil {
			refRows.Close()
			return nil, nil, err
		}
		refs = append(refs, rid)
	}
	refRows.Close()
	return quals, refs, refRows.Err()
}

func (s *Store) enrichStatement(ctx context.Context, q querier, st *domain.Statement) error {
	quals, refs, err := s.loadStatementProvenance(ctx, q, st.ID)
	if err != nil {
		return err
	}
	st.Qualifiers = quals
	st.ReferenceIDs = refs
	return nil
}
