package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/rasekl/knowledge-core/internal/datatype"
	"github.com/rasekl/knowledge-core/internal/domain"
	"github.com/shopspring/decimal"
)

func (s *Store) writeStatementRevision(
	ctx context.Context,
	tx pgx.Tx,
	cs *changeSetTx,
	meta domain.WriteMeta,
	statementID uuid.UUID,
	publicID string,
	propertyDtype datatype.Type,
	currentRev int,
	in domain.ReviseStatementInput,
) (int, error) {
	nextRev := currentRev + 1
	now := time.Now().UTC()

	var sv storedValue
	var validFrom, validTo *time.Time

	row := tx.QueryRow(ctx, `
		SELECT value_type, value_bool, value_int64, value_numeric, value_date, value_timestamptz,
			value_text, value_entity_id, value_json, valid_from, valid_to
		FROM statement WHERE id = $1
	`, statementID)
	var curType string
	var numeric *decimal.Decimal
	err := row.Scan(
		&curType, &sv.Bool, &sv.Int64, &numeric, &sv.Date, &sv.Timestamptz,
		&sv.Text, &sv.EntityID, &sv.JSON, &validFrom, &validTo,
	)
	if err != nil {
		return 0, err
	}
	sv.Type = curType
	sv.Numeric = numeric

	if in.Value != nil {
		val := *in.Value
		val, sv, err = s.resolveAndEncodeValue(ctx, tx, propertyDtype, val)
		if err != nil {
			return 0, err
		}
	}

	if in.ReplaceValidTime {
		validFrom = in.ValidFrom
		validTo = in.ValidTo
	}
	if err := validateValidTime(validFrom, validTo); err != nil {
		return 0, err
	}

	if in.ReplaceQualifiers {
		if err := s.replaceStatementQualifiers(ctx, tx, statementID, in.Qualifiers); err != nil {
			return 0, err
		}
	}
	if in.ReplaceReferences {
		if err := s.replaceStatementReferences(ctx, tx, statementID, in.ReferenceIDs); err != nil {
			return 0, err
		}
	}

	_, err = tx.Exec(ctx, `
		UPDATE statement SET
			value_type = $2, value_bool = $3, value_int64 = $4, value_numeric = $5,
			value_date = $6, value_timestamptz = $7, value_text = $8, value_entity_id = $9,
			value_json = $10, valid_from = $11, valid_to = $12,
			current_revision_no = $13, updated_at = $14
		WHERE id = $1
	`, statementID, sv.Type, sv.Bool, sv.Int64, sv.Numeric, sv.Date, sv.Timestamptz,
		sv.Text, sv.EntityID, sv.JSON, validFrom, validTo, nextRev, now)
	if err != nil {
		return 0, err
	}

	_, err = tx.Exec(ctx, `
		UPDATE statement_current SET
			value_type = $2, value_bool = $3, value_int64 = $4, value_numeric = $5,
			value_date = $6, value_timestamptz = $7, value_text = $8, value_entity_id = $9,
			value_json = $10, valid_from = $11, valid_to = $12, updated_at = $13
		WHERE statement_id = $1
	`, statementID, sv.Type, sv.Bool, sv.Int64, sv.Numeric, sv.Date, sv.Timestamptz,
		sv.Text, sv.EntityID, sv.JSON, validFrom, validTo, now)
	if err != nil {
		return 0, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO statement_revision (
			id, statement_id, revision_no, status, value_type,
			value_bool, value_int64, value_numeric, value_date, value_timestamptz,
			value_text, value_entity_id, value_json, valid_from, valid_to,
			change_set_id, actor, created_at
		) VALUES (
			$1,$2,$3,'active',$4,
			$5,$6,$7,$8,$9,
			$10,$11,$12,$13,$14,$15,$16,$17
		)
	`, datatype.NewUUID(), statementID, nextRev, sv.Type,
		sv.Bool, sv.Int64, sv.Numeric, sv.Date, sv.Timestamptz,
		sv.Text, sv.EntityID, sv.JSON, validFrom, validTo, cs.id, meta.Actor, now)
	if err != nil {
		return 0, err
	}

	if err := s.snapshotStatementProvenance(ctx, tx, statementID, nextRev); err != nil {
		return 0, err
	}
	if err := cs.addItem(ctx, tx, "statement", statementID, publicID, "revise", map[string]any{"revisionNo": nextRev}); err != nil {
		return 0, err
	}
	return nextRev, nil
}
