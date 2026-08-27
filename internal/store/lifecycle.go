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

func errIfEntityDeleted(status string) error {
	if status == string(domain.EntityDeleted) {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *Store) DeprecateEntity(ctx context.Context, meta domain.WriteMeta, publicID string, expectedRevision int) (*domain.WriteResult[domain.Entity], error) {
	return s.mutateEntityStatus(ctx, meta, func(ctx context.Context, tx pgx.Tx, cs *changeSetTx, meta domain.WriteMeta) (*domain.Entity, error) {
		return s.deprecateEntityInTx(ctx, tx, cs, meta, publicID, expectedRevision)
	})
}

func (s *Store) DeleteEntity(ctx context.Context, meta domain.WriteMeta, publicID string, expectedRevision int) (*domain.WriteResult[domain.Entity], error) {
	return s.mutateEntityStatus(ctx, meta, func(ctx context.Context, tx pgx.Tx, cs *changeSetTx, meta domain.WriteMeta) (*domain.Entity, error) {
		return s.deleteEntityInTx(ctx, tx, cs, meta, publicID, expectedRevision)
	})
}

func (s *Store) mutateEntityStatus(
	ctx context.Context,
	meta domain.WriteMeta,
	fn func(context.Context, pgx.Tx, *changeSetTx, domain.WriteMeta) (*domain.Entity, error),
) (*domain.WriteResult[domain.Entity], error) {
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

	cs, err := s.beginChangeSetTx(ctx, tx, meta)
	if err != nil {
		return nil, err
	}
	ent, err := fn(ctx, tx, cs, meta)
	if err != nil {
		return nil, err
	}
	if err := s.finalizeChangeSet(ctx, tx, cs, ent); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Entity]{Value: *ent, ChangeSet: s.changeSetDomain(cs, meta)}, nil
}

func (s *Store) deprecateEntityInTx(ctx context.Context, tx pgx.Tx, cs *changeSetTx, meta domain.WriteMeta, publicID string, expectedRevision int) (*domain.Entity, error) {
	if err := s.assertNotPackageRootTx(ctx, tx, publicID); err != nil {
		return nil, err
	}
	return s.setEntityStatusInTx(ctx, tx, cs, meta, publicID, expectedRevision, domain.EntityDeprecated, "deprecate", func(status string) error {
		if status != string(domain.EntityActive) {
			return fmt.Errorf("%w: entity is not active", ErrNotActive)
		}
		return nil
	})
}

func (s *Store) deleteEntityInTx(ctx context.Context, tx pgx.Tx, cs *changeSetTx, meta domain.WriteMeta, publicID string, expectedRevision int) (*domain.Entity, error) {
	if err := s.assertNotPackageRootTx(ctx, tx, publicID); err != nil {
		return nil, err
	}
	entityID, _, _, err := s.lockEntityForMutation(ctx, tx, publicID)
	if err != nil {
		return nil, err
	}
	refs, err := s.listBlockingEntityRefs(ctx, tx, entityID)
	if err != nil {
		return nil, err
	}
	if len(refs) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrEntityReferenced, strings.Join(refs, ", "))
	}
	outgoing, err := s.listOutgoingActiveStatements(ctx, tx, entityID)
	if err != nil {
		return nil, err
	}
	for _, st := range outgoing {
		if _, err := s.deprecateStatementInTx(ctx, tx, cs, meta, st.publicID, st.revisionNo); err != nil {
			return nil, err
		}
	}
	return s.setEntityStatusInTx(ctx, tx, cs, meta, publicID, expectedRevision, domain.EntityDeleted, "delete", nil)
}

type statementRevRef struct {
	publicID   string
	revisionNo int
}

func (s *Store) lockEntityForMutation(ctx context.Context, tx pgx.Tx, publicID string) (uuid.UUID, string, int, error) {
	var entityID uuid.UUID
	var status string
	var rev int
	err := tx.QueryRow(ctx, `
		SELECT id, status, current_revision_no FROM entity WHERE public_id = $1 FOR UPDATE
	`, publicID).Scan(&entityID, &status, &rev)
	if err != nil {
		return uuid.Nil, "", 0, err
	}
	if err := errIfEntityDeleted(status); err != nil {
		return uuid.Nil, "", 0, err
	}
	return entityID, status, rev, nil
}

func (s *Store) listOutgoingActiveStatements(ctx context.Context, tx pgx.Tx, entityID uuid.UUID) ([]statementRevRef, error) {
	rows, err := tx.Query(ctx, `
		SELECT public_id, current_revision_no
		FROM statement
		WHERE subject_id = $1 AND status = 'active'
		ORDER BY public_id
		FOR UPDATE
	`, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []statementRevRef
	for rows.Next() {
		var st statementRevRef
		if err := rows.Scan(&st.publicID, &st.revisionNo); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Store) listBlockingEntityRefs(ctx context.Context, tx pgx.Tx, entityID uuid.UUID) ([]string, error) {
	rows, err := tx.Query(ctx, `
		SELECT DISTINCT st.public_id
		FROM statement st
		LEFT JOIN statement_qualifier sq ON sq.statement_id = st.id
		WHERE st.status = 'active'
		  AND st.subject_id <> $1
		  AND (
			st.property_id = $1
			OR st.value_entity_id = $1
			OR sq.property_id = $1
			OR sq.value_entity_id = $1
		  )
		ORDER BY st.public_id
		LIMIT 8
	`, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) setEntityStatusInTx(
	ctx context.Context,
	tx pgx.Tx,
	cs *changeSetTx,
	meta domain.WriteMeta,
	publicID string,
	expectedRevision int,
	newStatus domain.EntityStatus,
	op string,
	checkStatus func(string) error,
) (*domain.Entity, error) {
	entityID, status, _, err := s.lockEntityForMutation(ctx, tx, publicID)
	if err != nil {
		return nil, err
	}
	if checkStatus != nil {
		if err := checkStatus(status); err != nil {
			return nil, err
		}
	}
	currentRev, err := s.assertEntityRevision(ctx, tx, entityID, expectedRevision)
	if err != nil {
		return nil, err
	}
	nextRev := currentRev + 1
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		UPDATE entity SET status = $2, current_revision_no = $3, updated_at = $4 WHERE id = $1
	`, entityID, newStatus, nextRev, now)
	if err != nil {
		return nil, err
	}
	ent, err := s.loadEntityTx(ctx, tx, publicID)
	if err != nil {
		return nil, err
	}
	ent.RevisionNo = nextRev
	ent.Status = newStatus
	labelsJSON, _ := labelsToJSON(ent.Labels)
	descJSON, _ := labelsToJSON(ent.Descriptions)
	_, err = tx.Exec(ctx, `
		INSERT INTO entity_revision (id, entity_id, revision_no, status, labels, descriptions, change_set_id, actor, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, datatype.NewUUID(), entityID, nextRev, newStatus, labelsJSON, descJSON, cs.id, meta.Actor, now)
	if err != nil {
		return nil, err
	}
	if err := cs.addItem(ctx, tx, "entity", entityID, publicID, op, map[string]any{"revisionNo": nextRev}); err != nil {
		return nil, err
	}
	return ent, nil
}
