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

func (s *Store) emitOutboxEvents(ctx context.Context, tx pgx.Tx, cs *changeSetTx) error {
	for _, item := range cs.items {
		eventType := fmt.Sprintf("%s.%s", item.ObjectType, item.Op)
		payload, _ := json.Marshal(map[string]string{
			"changeSetId": cs.publicID,
			"publicId":    item.PublicID,
			"op":          item.Op,
		})
		_, err := tx.Exec(ctx, `
			INSERT INTO outbox_event (id, change_set_id, event_type, aggregate_type, aggregate_id, payload, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7)
		`, datatype.NewUUID(), cs.id, eventType, item.ObjectType, item.PublicID, payload, cs.committed)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListPendingOutboxEvents(ctx context.Context, limit int) ([]domain.OutboxEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, change_set_id, event_type, aggregate_type, aggregate_id, payload, created_at
		FROM outbox_event
		WHERE published_at IS NULL
		ORDER BY created_at
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.OutboxEvent
	for rows.Next() {
		var ev domain.OutboxEvent
		var id, csID uuid.UUID
		var payloadJSON []byte
		var createdAt time.Time
		if err := rows.Scan(&id, &csID, &ev.EventType, &ev.AggregateType, &ev.AggregateID, &payloadJSON, &createdAt); err != nil {
			return nil, err
		}
		ev.ID = id.String()
		ev.ChangeSetID = csID.String()
		ev.CreatedAt = createdAt.UTC().Format(time.RFC3339Nano)
		_ = json.Unmarshal(payloadJSON, &ev.Payload)
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *Store) MarkOutboxPublished(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		UPDATE outbox_event SET published_at = $1 WHERE id = ANY($2)
	`, now, ids)
	return err
}

func (s *Store) ApplyOutboxToSearchProjection(ctx context.Context, ev domain.OutboxEvent) error {
	switch ev.AggregateType {
	case "entity":
		return s.projectEntitySearch(ctx, ev.AggregateID)
	case "statement":
		return s.projectStatementSearch(ctx, ev.AggregateID)
	default:
		return nil
	}
}

func (s *Store) projectEntitySearch(ctx context.Context, qid string) error {
	var entityID uuid.UUID
	var status string
	err := s.pool.QueryRow(ctx, `SELECT id, status FROM entity WHERE public_id = $1`, qid).Scan(&entityID, &status)
	if err != nil {
		return err
	}
	if status == "deleted" {
		_, err = s.pool.Exec(ctx, `DELETE FROM projection_search WHERE object_type = 'entity' AND public_id = $1`, qid)
		return err
	}
	labels, err := s.loadLabels(ctx, `SELECT lang, text FROM entity_label WHERE entity_id = $1`, entityID)
	if err != nil {
		return err
	}
	text := joinLabelTexts(labels)
	now := time.Now().UTC()
	_, err = s.pool.Exec(ctx, `
		INSERT INTO projection_search (id, object_type, public_id, search_text, updated_at)
		VALUES ($1,'entity',$2,$3,$4)
		ON CONFLICT (object_type, public_id) DO UPDATE SET search_text = EXCLUDED.search_text, updated_at = EXCLUDED.updated_at
	`, datatype.NewUUID(), qid, text, now)
	return err
}

func (s *Store) projectStatementSearch(ctx context.Context, sid string) error {
	row := s.pool.QueryRow(ctx, `
		SELECT st.status, e.id, e.public_id, p.id, p.public_id, sc.value_type,
			sc.value_text, sc.value_bool, sc.value_int64, sc.value_numeric::text, sc.value_date::text
		FROM statement st
		JOIN entity e ON e.id = st.subject_id
		JOIN property_profile pp ON pp.entity_id = st.property_id
		JOIN entity p ON p.id = pp.entity_id
		LEFT JOIN statement_current sc ON sc.statement_id = st.id
		WHERE st.public_id = $1
	`, sid)
	var status, subject, property string
	var subjectID, propertyID uuid.UUID
	var valueType string
	var valueText, numeric, date *string
	var valueBool *bool
	var valueInt *int64
	if err := row.Scan(&status, &subjectID, &subject, &propertyID, &property, &valueType, &valueText, &valueBool, &valueInt, &numeric, &date); err != nil {
		return err
	}
	if status == "deleted" {
		_, err := s.pool.Exec(ctx, `DELETE FROM projection_search WHERE object_type = 'statement' AND public_id = $1`, sid)
		return err
	}
	val := formatSearchValue(valueType, valueText, valueBool, valueInt, numeric, date)
	subjectLabels, _ := s.loadLabels(ctx, `SELECT lang, text FROM entity_label WHERE entity_id = $1`, subjectID)
	propLabels, _ := s.loadLabels(ctx, `SELECT lang, text FROM entity_label WHERE entity_id = $1`, propertyID)
	text := strings.TrimSpace(joinLabelTexts(subjectLabels) + " " + joinLabelTexts(propLabels) + " " + val)
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		INSERT INTO projection_search (id, object_type, public_id, subject_qid, property_pid, search_text, updated_at)
		VALUES ($1,'statement',$2,$3,$4,$5,$6)
		ON CONFLICT (object_type, public_id) DO UPDATE SET
			subject_qid = EXCLUDED.subject_qid,
			property_pid = EXCLUDED.property_pid,
			search_text = EXCLUDED.search_text,
			updated_at = EXCLUDED.updated_at
	`, datatype.NewUUID(), sid, subject, property, text, now)
	return err
}

func joinLabelTexts(labels map[string]string) string {
	parts := make([]string, 0, len(labels))
	for _, t := range labels {
		if t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, " ")
}

func formatSearchValue(valueType string, text *string, b *bool, i *int64, numeric, date *string) string {
	switch valueType {
	case "String", "URI":
		if text != nil {
			return *text
		}
	case "Boolean":
		if b != nil {
			if *b {
				return "true"
			}
			return "false"
		}
	case "Integer":
		if i != nil {
			return fmt.Sprintf("%d", *i)
		}
	case "Decimal":
		if numeric != nil {
			return *numeric
		}
	case "Date":
		if date != nil {
			return *date
		}
	}
	return ""
}

func (s *Store) RebuildSearchProjection(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `TRUNCATE projection_search`); err != nil {
		return err
	}

	entRows, err := tx.Query(ctx, `
		SELECT e.public_id FROM entity e WHERE e.status <> 'deleted' ORDER BY e.public_id
	`)
	if err != nil {
		return err
	}
	var qids []string
	for entRows.Next() {
		var qid string
		if err := entRows.Scan(&qid); err != nil {
			entRows.Close()
			return err
		}
		qids = append(qids, qid)
	}
	entRows.Close()
	if err := entRows.Err(); err != nil {
		return err
	}
	for _, qid := range qids {
		if err := s.projectEntitySearchTx(ctx, tx, qid); err != nil {
			return err
		}
	}

	stmtRows, err := tx.Query(ctx, `
		SELECT st.public_id FROM statement st WHERE st.status = 'active' ORDER BY st.public_id
	`)
	if err != nil {
		return err
	}
	var sids []string
	for stmtRows.Next() {
		var sid string
		if err := stmtRows.Scan(&sid); err != nil {
			stmtRows.Close()
			return err
		}
		sids = append(sids, sid)
	}
	stmtRows.Close()
	if err := stmtRows.Err(); err != nil {
		return err
	}
	for _, sid := range sids {
		if err := s.projectStatementSearchTx(ctx, tx, sid); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) projectEntitySearchTx(ctx context.Context, tx pgx.Tx, qid string) error {
	var entityID uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM entity WHERE public_id = $1`, qid).Scan(&entityID)
	if err != nil {
		return err
	}
	labels, err := s.loadLabelsTx(ctx, tx, `SELECT lang, text FROM entity_label WHERE entity_id = $1`, entityID)
	if err != nil {
		return err
	}
	text := joinLabelTexts(labels)
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		INSERT INTO projection_search (id, object_type, public_id, search_text, updated_at)
		VALUES ($1,'entity',$2,$3,$4)
		ON CONFLICT (object_type, public_id) DO UPDATE SET search_text = EXCLUDED.search_text, updated_at = EXCLUDED.updated_at
	`, datatype.NewUUID(), qid, text, now)
	return err
}

func (s *Store) projectStatementSearchTx(ctx context.Context, tx pgx.Tx, sid string) error {
	row := tx.QueryRow(ctx, `
		SELECT e.id, e.public_id, p.id, p.public_id, sc.value_type,
			sc.value_text, sc.value_bool, sc.value_int64, sc.value_numeric::text, sc.value_date::text
		FROM statement st
		JOIN entity e ON e.id = st.subject_id
		JOIN property_profile pp ON pp.entity_id = st.property_id
		JOIN entity p ON p.id = pp.entity_id
		LEFT JOIN statement_current sc ON sc.statement_id = st.id
		WHERE st.public_id = $1 AND st.status = 'active'
	`, sid)
	var subjectID, propertyID uuid.UUID
	var subject, property, valueType string
	var valueText, numeric, date *string
	var valueBool *bool
	var valueInt *int64
	if err := row.Scan(&subjectID, &subject, &propertyID, &property, &valueType, &valueText, &valueBool, &valueInt, &numeric, &date); err != nil {
		return err
	}
	subjectLabels, _ := s.loadLabelsTx(ctx, tx, `SELECT lang, text FROM entity_label WHERE entity_id = $1`, subjectID)
	propLabels, _ := s.loadLabelsTx(ctx, tx, `SELECT lang, text FROM entity_label WHERE entity_id = $1`, propertyID)
	val := formatSearchValue(valueType, valueText, valueBool, valueInt, numeric, date)
	text := strings.TrimSpace(joinLabelTexts(subjectLabels) + " " + joinLabelTexts(propLabels) + " " + val)
	now := time.Now().UTC()
	_, err := tx.Exec(ctx, `
		INSERT INTO projection_search (id, object_type, public_id, subject_qid, property_pid, search_text, updated_at)
		VALUES ($1,'statement',$2,$3,$4,$5,$6)
		ON CONFLICT (object_type, public_id) DO UPDATE SET
			subject_qid = EXCLUDED.subject_qid,
			property_pid = EXCLUDED.property_pid,
			search_text = EXCLUDED.search_text,
			updated_at = EXCLUDED.updated_at
	`, datatype.NewUUID(), sid, subject, property, text, now)
	return err
}

func (s *Store) SearchProjection(ctx context.Context, query string, limit int) ([]domain.SearchHit, error) {
	if limit <= 0 {
		limit = 20
	}
	pattern := "%" + strings.ToLower(query) + "%"
	rows, err := s.pool.Query(ctx, `
		SELECT object_type, public_id, COALESCE(subject_qid,''), COALESCE(property_pid,''), search_text
		FROM projection_search
		WHERE lower(search_text) LIKE $1
		ORDER BY public_id
		LIMIT $2
	`, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SearchHit
	for rows.Next() {
		var h domain.SearchHit
		if err := rows.Scan(&h.ObjectType, &h.PublicID, &h.SubjectQID, &h.PropertyPID, &h.SearchText); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *Store) TruncateSearchProjection(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `TRUNCATE projection_search`)
	return err
}

func (s *Store) CountSearchProjection(ctx context.Context) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM projection_search`).Scan(&n)
	return n, err
}
