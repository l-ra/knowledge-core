package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
)

func (s *Store) DeprecateStatement(ctx context.Context, meta domain.WriteMeta, publicID string, expectedRevision int) (*domain.WriteResult[domain.Statement], error) {
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

	if err := s.assertNotManagedPackageCodeStatementTx(ctx, tx, publicID); err != nil {
		return nil, err
	}

	var statementID uuid.UUID
	var status string
	err = tx.QueryRow(ctx, `
		SELECT id, status FROM statement WHERE public_id = $1
	`, publicID).Scan(&statementID, &status)
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

	cs, err := s.beginChangeSetTx(ctx, tx, meta)
	if err != nil {
		return nil, err
	}

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

	if err := s.finalizeChangeSet(ctx, tx, cs, st); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Statement]{Value: *st, ChangeSet: s.changeSetDomain(cs, meta)}, nil
}
