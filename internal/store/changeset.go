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

var (
	ErrConflict             = errors.New("revision conflict")
	ErrIdempotencyConflict  = errors.New("idempotency key reused with different request")
)

type idempotencyHit struct {
	publicID     string
	responseBody []byte
}

func (s *Store) checkIdempotency(ctx context.Context, tx pgx.Tx, meta domain.WriteMeta) (*idempotencyHit, error) {
	if meta.IdempotencyKey == "" {
		return nil, nil
	}
	var csID uuid.UUID
	var requestHash *string
	var responseBody []byte
	var publicID *string
	err := tx.QueryRow(ctx, `
		SELECT id, public_id, request_hash, response_body
		FROM change_set WHERE idempotency_key = $1
	`, meta.IdempotencyKey).Scan(&csID, &publicID, &requestHash, &responseBody)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if requestHash != nil && *requestHash != meta.RequestHash {
		return nil, ErrIdempotencyConflict
	}
	pid := ""
	if publicID != nil {
		pid = *publicID
	}
	return &idempotencyHit{publicID: pid, responseBody: responseBody}, nil
}

type changeSetTx struct {
	id        uuid.UUID
	publicID  string
	committed time.Time
	items     []domain.ChangeSetItem
}

func (s *Store) beginChangeSetTx(ctx context.Context, tx pgx.Tx, meta domain.WriteMeta) (*changeSetTx, error) {
	id := datatype.NewUUID()
	publicID, err := s.nextPublicID(ctx, tx, "changeset", "C")
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	actor := meta.Actor
	if actor == "" {
		actor = "system"
	}
	opType := meta.OperationType
	if opType == "" {
		opType = "mutation"
	}
	var reqHash *string
	if meta.RequestHash != "" {
		reqHash = &meta.RequestHash
	}
	var corr *string
	if meta.CorrelationID != "" {
		corr = &meta.CorrelationID
	}
	var idem *string
	if meta.IdempotencyKey != "" {
		idem = &meta.IdempotencyKey
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO change_set (id, public_id, actor, committed_at, operation_type, idempotency_key, correlation_id, request_hash)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
	`, id, publicID, actor, now, opType, idem, corr, reqHash)
	if err != nil {
		return nil, err
	}
	return &changeSetTx{id: id, publicID: publicID, committed: now}, nil
}

func (c *changeSetTx) addItem(ctx context.Context, tx pgx.Tx, objectType string, objectID uuid.UUID, publicID, op string, payload any) error {
	var payloadJSON []byte
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		payloadJSON = b
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO change_set_item (id, change_set_id, object_type, object_id, public_id, op, payload)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`, datatype.NewUUID(), c.id, objectType, objectID, publicID, op, payloadJSON)
	if err != nil {
		return err
	}
	c.items = append(c.items, domain.ChangeSetItem{
		ObjectType: objectType, ObjectID: objectID, PublicID: publicID, Op: op,
	})
	return nil
}

func (s *Store) finalizeChangeSet(ctx context.Context, tx pgx.Tx, cs *changeSetTx, responseBody any) error {
	var body []byte
	if responseBody != nil {
		b, err := json.Marshal(responseBody)
		if err != nil {
			return err
		}
		body = b
	}
	_, err := tx.Exec(ctx, `UPDATE change_set SET response_body = $2 WHERE id = $1`, cs.id, body)
	if err != nil {
		return err
	}
	return s.emitOutboxEvents(ctx, tx, cs)
}

func (s *Store) changeSetDomain(cs *changeSetTx, meta domain.WriteMeta) *domain.ChangeSet {
	return &domain.ChangeSet{
		ID: cs.id, PublicID: cs.publicID, Actor: meta.Actor,
		OperationType: meta.OperationType, CommittedAt: cs.committed,
		IdempotencyKey: meta.IdempotencyKey, CorrelationID: meta.CorrelationID,
		Items: cs.items,
	}
}

func labelsToJSON(m map[string]string) ([]byte, error) {
	if m == nil {
		m = map[string]string{}
	}
	return json.Marshal(m)
}

func jsonToLabels(b []byte) (map[string]string, error) {
	out := map[string]string{}
	if len(b) == 0 {
		return out, nil
	}
	err := json.Unmarshal(b, &out)
	return out, err
}

func (s *Store) assertEntityRevision(ctx context.Context, tx pgx.Tx, entityID uuid.UUID, expected int) (int, error) {
	var current int
	err := tx.QueryRow(ctx, `SELECT current_revision_no FROM entity WHERE id = $1 FOR UPDATE`, entityID).Scan(&current)
	if err != nil {
		return 0, err
	}
	if expected > 0 && current != expected {
		return current, fmt.Errorf("%w: entity revision %d expected %d", ErrConflict, current, expected)
	}
	return current, nil
}

func (s *Store) assertStatementRevision(ctx context.Context, tx pgx.Tx, statementID uuid.UUID, expected int) (int, error) {
	var current int
	err := tx.QueryRow(ctx, `SELECT current_revision_no FROM statement WHERE id = $1 FOR UPDATE`, statementID).Scan(&current)
	if err != nil {
		return 0, err
	}
	if expected > 0 && current != expected {
		return current, fmt.Errorf("%w: statement revision %d expected %d", ErrConflict, current, expected)
	}
	return current, nil
}
