package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/rasekl/knowledge-core/internal/domain"
	"github.com/shopspring/decimal"
)

func (s *Store) GetChangeSetByPublicID(ctx context.Context, cid string) (*domain.ChangeSet, error) {
	var cs domain.ChangeSet
	var idem, corr *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, public_id, COALESCE(actor,''), operation_type, committed_at, idempotency_key, correlation_id
		FROM change_set WHERE public_id = $1
	`, cid).Scan(&cs.ID, &cs.PublicID, &cs.Actor, &cs.OperationType, &cs.CommittedAt, &idem, &corr)
	if err != nil {
		return nil, err
	}
	if idem != nil {
		cs.IdempotencyKey = *idem
	}
	if corr != nil {
		cs.CorrelationID = *corr
	}
	rows, err := s.pool.Query(ctx, `
		SELECT object_type, object_id, COALESCE(public_id,''), op
		FROM change_set_item WHERE change_set_id = $1 ORDER BY id
	`, cs.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item domain.ChangeSetItem
		if err := rows.Scan(&item.ObjectType, &item.ObjectID, &item.PublicID, &item.Op); err != nil {
			return nil, err
		}
		cs.Items = append(cs.Items, item)
	}
	return &cs, rows.Err()
}

func (s *Store) GetChangeSetByIdempotencyKey(ctx context.Context, key string) (*domain.ChangeSet, error) {
	var publicID string
	err := s.pool.QueryRow(ctx, `SELECT public_id FROM change_set WHERE idempotency_key = $1`, key).Scan(&publicID)
	if err != nil {
		return nil, err
	}
	return s.GetChangeSetByPublicID(ctx, publicID)
}

func (s *Store) GetEntityHistory(ctx context.Context, qid string) ([]domain.EntityRevision, error) {
	var entityID string
	err := s.pool.QueryRow(ctx, `SELECT id::text FROM entity WHERE public_id = $1`, qid).Scan(&entityID)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT revision_no, status, labels, descriptions, actor, change_set_id, created_at
		FROM entity_revision WHERE entity_id = $1::uuid
		ORDER BY revision_no ASC
	`, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.EntityRevision
	for rows.Next() {
		var rev domain.EntityRevision
		var labelsJSON, descJSON []byte
		var actor *string
		if err := rows.Scan(&rev.RevisionNo, &rev.Status, &labelsJSON, &descJSON, &actor, &rev.ChangeSetID, &rev.CreatedAt); err != nil {
			return nil, err
		}
		rev.Labels, _ = jsonToLabels(labelsJSON)
		rev.Descriptions, _ = jsonToLabels(descJSON)
		if actor != nil {
			rev.Actor = *actor
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}

func (s *Store) GetStatementHistory(ctx context.Context, sid string) ([]domain.StatementRevision, error) {
	var statementID uuid.UUID
	err := s.pool.QueryRow(ctx, `SELECT id FROM statement WHERE public_id = $1`, sid).Scan(&statementID)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT revision_no, status, value_type,
			value_bool, value_int64, value_numeric, value_date, value_timestamptz,
			value_text, value_entity_id, value_json, valid_from, valid_to, actor, change_set_id, created_at
		FROM statement_revision WHERE statement_id = $1
		ORDER BY revision_no ASC
	`, statementID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.StatementRevision
	for rows.Next() {
		var rev domain.StatementRevision
		var sv storedValue
		var numeric *decimal.Decimal
		var actor *string
		if err := rows.Scan(
			&rev.RevisionNo, &rev.Status, &sv.Type,
			&sv.Bool, &sv.Int64, &numeric, &sv.Date, &sv.Timestamptz,
			&sv.Text, &sv.EntityID, &sv.JSON, &rev.ValidFrom, &rev.ValidTo, &actor, &rev.ChangeSetID, &rev.CreatedAt,
		); err != nil {
			return nil, err
		}
		sv.Numeric = numeric
		val, err := decodeValue(sv)
		if err != nil {
			return nil, err
		}
		rev.Value = val
		if actor != nil {
			rev.Actor = *actor
		}
		quals, refs, err := s.loadStatementProvenanceAtRevision(ctx, s.pool, statementID, rev.RevisionNo)
		if err != nil {
			return nil, err
		}
		rev.Qualifiers = quals
		rev.ReferenceIDs = refs
		out = append(out, rev)
	}
	return out, rows.Err()
}

func scanStatementRow(row pgx.Row) (*domain.Statement, error) {
	return scanStatement(row)
}
