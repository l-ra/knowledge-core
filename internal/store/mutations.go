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
)

func (s *Store) UpdateEntity(ctx context.Context, meta domain.WriteMeta, publicID string, in domain.UpdateEntityInput) (*domain.WriteResult[domain.Entity], error) {
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

	var entityID uuid.UUID
	err = tx.QueryRow(ctx, `SELECT id FROM entity WHERE public_id = $1`, publicID).Scan(&entityID)
	if err != nil {
		return nil, err
	}

	currentRev, err := s.assertEntityRevision(ctx, tx, entityID, in.ExpectedRevision)
	if err != nil {
		return nil, err
	}
	nextRev := currentRev + 1

	labels := in.Labels
	descs := in.Descriptions
	if labels != nil {
		norm, err := datatype.NormalizeLabels(labels)
		if err != nil {
			return nil, err
		}
		if err := datatype.RequireLabelEN(norm); err != nil {
			return nil, err
		}
		labels = norm
		_, err = tx.Exec(ctx, `DELETE FROM entity_label WHERE entity_id = $1`, entityID)
		if err != nil {
			return nil, err
		}
		for lang, text := range labels {
			if _, err := tx.Exec(ctx, `INSERT INTO entity_label (entity_id, lang, text) VALUES ($1,$2,$3)`, entityID, lang, text); err != nil {
				return nil, err
			}
		}
	}
	if descs != nil {
		_, err = tx.Exec(ctx, `DELETE FROM entity_description WHERE entity_id = $1`, entityID)
		if err != nil {
			return nil, err
		}
		for lang, text := range descs {
			if _, err := tx.Exec(ctx, `INSERT INTO entity_description (entity_id, lang, text) VALUES ($1,$2,$3)`, entityID, lang, text); err != nil {
				return nil, err
			}
		}
	}

	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `UPDATE entity SET current_revision_no = $2, updated_at = $3 WHERE id = $1`, entityID, nextRev, now)
	if err != nil {
		return nil, err
	}

	ent, err := s.loadEntityTx(ctx, tx, publicID)
	if err != nil {
		return nil, err
	}
	ent.RevisionNo = nextRev

	labelsJSON, _ := labelsToJSON(ent.Labels)
	descJSON, _ := labelsToJSON(ent.Descriptions)
	cs, err := s.beginChangeSetTx(ctx, tx, meta)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO entity_revision (id, entity_id, revision_no, status, labels, descriptions, change_set_id, actor, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, datatype.NewUUID(), entityID, nextRev, ent.Status, labelsJSON, descJSON, cs.id, meta.Actor, now)
	if err != nil {
		return nil, err
	}
	if err := cs.addItem(ctx, tx, "entity", entityID, publicID, "update", map[string]any{"revisionNo": nextRev}); err != nil {
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

func (s *Store) ReviseStatement(ctx context.Context, meta domain.WriteMeta, publicID string, in domain.ReviseStatementInput) (*domain.WriteResult[domain.Statement], error) {
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

	var statementID uuid.UUID
	var propertyPID string
	var dtype string
	err = tx.QueryRow(ctx, `
		SELECT st.id, p.public_id, p.datatype
		FROM statement st
		JOIN property_definition p ON p.id = st.property_id
		WHERE st.public_id = $1
	`, publicID).Scan(&statementID, &propertyPID, &dtype)
	if err != nil {
		return nil, fmt.Errorf("statement: %w", err)
	}

	currentRev, err := s.assertStatementRevision(ctx, tx, statementID, in.ExpectedRevision)
	if err != nil {
		return nil, err
	}

	cs, err := s.beginChangeSetTx(ctx, tx, meta)
	if err != nil {
		return nil, err
	}
	nextRev, err := s.writeStatementRevision(ctx, tx, cs, meta, statementID, publicID, datatype.Type(dtype), currentRev, in)
	if err != nil {
		return nil, err
	}

	st, err := s.getStatementTx(ctx, tx, publicID)
	if err != nil {
		return nil, err
	}
	st.RevisionNo = nextRev
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

	results := make([]map[string]any, 0, len(in.Operations))
	for _, op := range in.Operations {
		switch op.Op {
		case "reviseStatement":
			if op.Statement == "" {
				return nil, fmt.Errorf("reviseStatement requires statement")
			}
			val := op.Value
			stRes, err := s.reviseStatementInTx(ctx, tx, cs, meta, op.Statement, domain.ReviseStatementInput{
				Value:            &val,
				ExpectedRevision: op.ExpectedRevision,
			})
			if err != nil {
				return nil, err
			}
			results = append(results, map[string]any{"op": op.Op, "statement": stRes.PublicID, "revisionNo": stRes.RevisionNo})
		case "updateEntity":
			if op.Entity == "" {
				return nil, fmt.Errorf("updateEntity requires entity")
			}
			entRes, err := s.updateEntityInTx(ctx, tx, cs, meta, op.Entity, domain.UpdateEntityInput{
				Labels: op.Labels, Descriptions: op.Descriptions, ExpectedRevision: op.ExpectedRevision,
			})
			if err != nil {
				return nil, err
			}
			results = append(results, map[string]any{"op": op.Op, "entity": entRes.PublicID, "revisionNo": entRes.RevisionNo})
		default:
			return nil, fmt.Errorf("unsupported operation %q", op.Op)
		}
	}

	response := map[string]any{
		"changeSet": cs.publicID,
		"results":   results,
	}
	responseBody, _ := json.Marshal(response)
	domainCS := s.changeSetDomain(cs, meta)
	domainCS.Items = cs.items
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

func (s *Store) reviseStatementInTx(ctx context.Context, tx pgx.Tx, cs *changeSetTx, meta domain.WriteMeta, publicID string, in domain.ReviseStatementInput) (*domain.Statement, error) {
	var statementID uuid.UUID
	var dtype string
	err := tx.QueryRow(ctx, `
		SELECT st.id, p.datatype FROM statement st
		JOIN property_definition p ON p.id = st.property_id
		WHERE st.public_id = $1
	`, publicID).Scan(&statementID, &dtype)
	if err != nil {
		return nil, fmt.Errorf("statement: %w", err)
	}

	currentRev, err := s.assertStatementRevision(ctx, tx, statementID, in.ExpectedRevision)
	if err != nil {
		return nil, err
	}
	nextRev, err := s.writeStatementRevision(ctx, tx, cs, meta, statementID, publicID, datatype.Type(dtype), currentRev, in)
	if err != nil {
		return nil, err
	}
	st, err := s.getStatementTx(ctx, tx, publicID)
	if err != nil {
		return nil, err
	}
	st.RevisionNo = nextRev
	if err := s.enrichStatement(ctx, tx, st); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Store) updateEntityInTx(ctx context.Context, tx pgx.Tx, cs *changeSetTx, meta domain.WriteMeta, publicID string, in domain.UpdateEntityInput) (*domain.Entity, error) {
	var entityID uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM entity WHERE public_id = $1`, publicID).Scan(&entityID)
	if err != nil {
		return nil, err
	}
	currentRev, err := s.assertEntityRevision(ctx, tx, entityID, in.ExpectedRevision)
	if err != nil {
		return nil, err
	}
	nextRev := currentRev + 1

	if in.Labels != nil {
		norm, err := datatype.NormalizeLabels(in.Labels)
		if err != nil {
			return nil, err
		}
		if err := datatype.RequireLabelEN(norm); err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `DELETE FROM entity_label WHERE entity_id = $1`, entityID)
		if err != nil {
			return nil, err
		}
		for lang, text := range norm {
			if _, err := tx.Exec(ctx, `INSERT INTO entity_label (entity_id, lang, text) VALUES ($1,$2,$3)`, entityID, lang, text); err != nil {
				return nil, err
			}
		}
	}
	if in.Descriptions != nil {
		_, err = tx.Exec(ctx, `DELETE FROM entity_description WHERE entity_id = $1`, entityID)
		if err != nil {
			return nil, err
		}
		for lang, text := range in.Descriptions {
			if _, err := tx.Exec(ctx, `INSERT INTO entity_description (entity_id, lang, text) VALUES ($1,$2,$3)`, entityID, lang, text); err != nil {
				return nil, err
			}
		}
	}
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `UPDATE entity SET current_revision_no = $2, updated_at = $3 WHERE id = $1`, entityID, nextRev, now)
	if err != nil {
		return nil, err
	}
	ent, err := s.loadEntityTx(ctx, tx, publicID)
	if err != nil {
		return nil, err
	}
	ent.RevisionNo = nextRev
	labelsJSON, _ := labelsToJSON(ent.Labels)
	descJSON, _ := labelsToJSON(ent.Descriptions)
	_, err = tx.Exec(ctx, `
		INSERT INTO entity_revision (id, entity_id, revision_no, status, labels, descriptions, change_set_id, actor, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
	`, datatype.NewUUID(), entityID, nextRev, ent.Status, labelsJSON, descJSON, cs.id, meta.Actor, now)
	if err != nil {
		return nil, err
	}
	if err := cs.addItem(ctx, tx, "entity", entityID, publicID, "update", map[string]any{"revisionNo": nextRev}); err != nil {
		return nil, err
	}
	return ent, nil
}

func (s *Store) loadEntityTx(ctx context.Context, tx pgx.Tx, qid string) (*domain.Entity, error) {
	var e domain.Entity
	err := tx.QueryRow(ctx, `
		SELECT id, public_id, status, current_revision_no, created_at, updated_at FROM entity WHERE public_id = $1
	`, qid).Scan(&e.ID, &e.PublicID, &e.Status, &e.RevisionNo, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, err
	}
	e.Labels, err = s.loadLabelsTx(ctx, tx, `SELECT lang, text FROM entity_label WHERE entity_id = $1`, e.ID)
	if err != nil {
		return nil, err
	}
	e.Descriptions, err = s.loadLabelsTx(ctx, tx, `SELECT lang, text FROM entity_description WHERE entity_id = $1`, e.ID)
	return &e, err
}

func (s *Store) getStatementTx(ctx context.Context, tx pgx.Tx, sid string) (*domain.Statement, error) {
	row := tx.QueryRow(ctx, `
		SELECT st.id, st.public_id, st.subject_id, e.public_id, st.property_id, p.public_id, st.status,
			st.value_type, st.value_bool, st.value_int64, st.value_numeric, st.value_date, st.value_timestamptz,
			st.value_text, st.value_entity_id, st.value_json, st.valid_from, st.valid_to,
			st.current_revision_no, st.created_at, st.updated_at
		FROM statement st
		JOIN entity e ON e.id = st.subject_id
		JOIN property_definition p ON p.id = st.property_id
		WHERE st.public_id = $1
	`, sid)
	return scanStatementRow(row)
}

func (s *Store) loadLabelsTx(ctx context.Context, tx pgx.Tx, q string, id uuid.UUID) (map[string]string, error) {
	rows, err := tx.Query(ctx, q, id)
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

func (s *Store) resolveAndEncodeValue(ctx context.Context, tx pgx.Tx, dtype datatype.Type, val datatype.Value) (datatype.Value, storedValue, error) {
	if val.Type == "" {
		val.Type = dtype
	}
	if dtype == datatype.EntityReference && val.EntityID != nil {
		var refUUID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM entity WHERE public_id = $1 OR id::text = $1`, *val.EntityID).Scan(&refUUID)
		if err != nil {
			return val, storedValue{}, fmt.Errorf("value entity: %w", err)
		}
		s := refUUID.String()
		val.EntityID = &s
	}
	if dtype == datatype.Quantity && val.UnitEntityID != nil {
		var unitUUID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM entity WHERE public_id = $1 OR id::text = $1`, *val.UnitEntityID).Scan(&unitUUID)
		if err != nil {
			return val, storedValue{}, fmt.Errorf("unit entity: %w", err)
		}
		s := unitUUID.String()
		val.UnitEntityID = &s
	}
	sv, err := encodeValue(dtype, val)
	return val, sv, err
}
