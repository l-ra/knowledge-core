package store

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/l-ra/knowledge-core/internal/domain"
	"github.com/shopspring/decimal"
)

var ErrInvalidCursor = errors.New("invalid cursor")

type ChangeSetListOptions struct {
	Limit         int
	Cursor        string
	Query         string
	Actor         string
	OperationType string
	CorrelationID string
	ObjectID      string
	CommittedFrom *time.Time
	CommittedTo   *time.Time
}

func (s *Store) GetChangeSetByPublicID(ctx context.Context, cid string) (*domain.ChangeSet, error) {
	var cs domain.ChangeSet
	var idem, corr *string
	err := s.pool.QueryRow(ctx, `
		SELECT id, public_id, COALESCE(actor,''), operation_type, COALESCE(comment,''),
			committed_at, idempotency_key, correlation_id
		FROM change_set WHERE public_id = $1
	`, cid).Scan(&cs.ID, &cs.PublicID, &cs.Actor, &cs.OperationType, &cs.Comment, &cs.CommittedAt, &idem, &corr)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	cs.ItemCount = len(cs.Items)
	return &cs, nil
}

func (s *Store) ListChangeSets(ctx context.Context, opt ChangeSetListOptions) ([]domain.ChangeSet, string, error) {
	if opt.Limit <= 0 {
		opt.Limit = 50
	}
	if opt.Limit > 200 {
		opt.Limit = 200
	}

	args := []any{}
	where := `TRUE`
	if actor := strings.TrimSpace(opt.Actor); actor != "" {
		args = append(args, actor)
		where += ` AND cs.actor = $` + strconv.Itoa(len(args))
	}
	if opType := strings.TrimSpace(opt.OperationType); opType != "" {
		args = append(args, opType)
		where += ` AND cs.operation_type = $` + strconv.Itoa(len(args))
	}
	if corr := strings.TrimSpace(opt.CorrelationID); corr != "" {
		args = append(args, corr)
		where += ` AND cs.correlation_id = $` + strconv.Itoa(len(args))
	}
	if obj := strings.TrimSpace(opt.ObjectID); obj != "" {
		args = append(args, obj)
		where += ` AND EXISTS (
			SELECT 1 FROM change_set_item csi
			WHERE csi.change_set_id = cs.id AND csi.public_id = $` + strconv.Itoa(len(args)) + `
		)`
	}
	if opt.CommittedFrom != nil {
		args = append(args, *opt.CommittedFrom)
		where += ` AND cs.committed_at >= $` + strconv.Itoa(len(args))
	}
	if opt.CommittedTo != nil {
		args = append(args, *opt.CommittedTo)
		where += ` AND cs.committed_at <= $` + strconv.Itoa(len(args))
	}
	if q := strings.TrimSpace(opt.Query); q != "" {
		args = append(args, "%"+strings.ToLower(q)+"%")
		n := strconv.Itoa(len(args))
		where += ` AND (
			lower(cs.public_id) LIKE $` + n + `
			OR lower(COALESCE(cs.actor,'')) LIKE $` + n + `
			OR lower(COALESCE(cs.operation_type,'')) LIKE $` + n + `
			OR lower(COALESCE(cs.comment,'')) LIKE $` + n + `
			OR lower(COALESCE(cs.correlation_id,'')) LIKE $` + n + `
			OR lower(COALESCE(cs.idempotency_key,'')) LIKE $` + n + `
		)`
	}
	if opt.Cursor != "" {
		ts, pid, ok := parseChangeSetCursor(opt.Cursor)
		if !ok {
			return nil, "", ErrInvalidCursor
		}
		args = append(args, ts, pid)
		tsN := strconv.Itoa(len(args) - 1)
		pidN := strconv.Itoa(len(args))
		where += ` AND (cs.committed_at, cs.public_id) < ($` + tsN + `::timestamptz, $` + pidN + `)`
	}

	args = append(args, opt.Limit+1)
	limitArg := `$` + strconv.Itoa(len(args))

	rows, err := s.pool.Query(ctx, `
		SELECT cs.id, cs.public_id, COALESCE(cs.actor,''), cs.operation_type, COALESCE(cs.comment,''),
			cs.committed_at, cs.idempotency_key, cs.correlation_id,
			(SELECT count(*)::int FROM change_set_item csi WHERE csi.change_set_id = cs.id)
		FROM change_set cs
		WHERE `+where+`
		ORDER BY cs.committed_at DESC, cs.public_id DESC
		LIMIT `+limitArg+`
	`, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var out []domain.ChangeSet
	for rows.Next() {
		var cs domain.ChangeSet
		var idem, corr *string
		if err := rows.Scan(
			&cs.ID, &cs.PublicID, &cs.Actor, &cs.OperationType, &cs.Comment,
			&cs.CommittedAt, &idem, &corr, &cs.ItemCount,
		); err != nil {
			return nil, "", err
		}
		if idem != nil {
			cs.IdempotencyKey = *idem
		}
		if corr != nil {
			cs.CorrelationID = *corr
		}
		out = append(out, cs)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > opt.Limit {
		last := out[opt.Limit-1]
		next = encodeChangeSetCursor(last.CommittedAt, last.PublicID)
		out = out[:opt.Limit]
	}
	return out, next, nil
}

func encodeChangeSetCursor(committedAt time.Time, publicID string) string {
	return committedAt.UTC().Format(time.RFC3339Nano) + "|" + publicID
}

func parseChangeSetCursor(cursor string) (time.Time, string, bool) {
	i := strings.LastIndex(cursor, "|")
	if i <= 0 || i == len(cursor)-1 {
		return time.Time{}, "", false
	}
	ts, err := time.Parse(time.RFC3339Nano, cursor[:i])
	if err != nil {
		ts, err = time.Parse(time.RFC3339, cursor[:i])
		if err != nil {
			return time.Time{}, "", false
		}
	}
	return ts, cursor[i+1:], true
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
