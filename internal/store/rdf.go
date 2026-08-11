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

const (
	rdfNSEntity   = "https://knowledge-core.local/entity/"
	rdfNSProperty = "https://knowledge-core.local/property/"
	rdfNSStmt     = "https://knowledge-core.local/statement/"
	rdfPredicateLabel = "http://www.w3.org/2000/01/rdf-schema#label"
	rdfPredicateType  = "http://www.w3.org/1999/02/22-rdf-syntax-ns#type"
	rdfPredicateSubClass = "http://www.w3.org/2000/01/rdf-schema#subClassOf"
	rdfTypeEntity     = "https://knowledge-core.local/ontology/Entity"
	rdfTypeStatement  = "https://knowledge-core.local/ontology/Statement"
	rdfTypeProperty   = "http://www.w3.org/1999/02/22-rdf-syntax-ns#Property"
	rdfTypeClass      = "http://www.w3.org/2000/01/rdf-schema#Class"
)

func (s *Store) ApplyOutboxToRDFProjection(ctx context.Context, ev domain.OutboxEvent) error {
	switch ev.AggregateType {
	case "entity", "property", "class":
		return s.projectEntityRDF(ctx, ev.AggregateID)
	case "statement":
		return s.projectStatementRDF(ctx, ev.AggregateID)
	default:
		return nil
	}
}

func (s *Store) projectEntityRDF(ctx context.Context, qid string) error {
	var entityID uuid.UUID
	var status string
	err := s.pool.QueryRow(ctx, `SELECT id, status FROM entity WHERE public_id = $1`, qid).Scan(&entityID, &status)
	if err != nil {
		return err
	}
	_, _ = s.pool.Exec(ctx, `DELETE FROM projection_rdf WHERE object_type = 'entity' AND public_id = $1`, qid)
	if status == "deleted" {
		return nil
	}
	labels, err := s.loadLabels(ctx, `SELECT lang, text FROM entity_label WHERE entity_id = $1`, entityID)
	if err != nil {
		return err
	}
	subj := rdfNSEntity + qid
	now := time.Now().UTC()
	if err := s.insertRDFTriple(ctx, "entity", qid, subj, rdfPredicateType, "<"+rdfTypeEntity+">", now); err != nil {
		return err
	}
	var hasProp, hasClass bool
	_ = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM property_profile WHERE entity_id = $1)`, entityID).Scan(&hasProp)
	_ = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM class_profile WHERE entity_id = $1)`, entityID).Scan(&hasClass)
	if hasProp {
		if err := s.insertRDFTriple(ctx, "entity", qid, subj, rdfPredicateType, "<"+rdfTypeProperty+">", now); err != nil {
			return err
		}
	}
	if hasClass {
		if err := s.insertRDFTriple(ctx, "entity", qid, subj, rdfPredicateType, "<"+rdfTypeClass+">", now); err != nil {
			return err
		}
		var docJSON []byte
		if err := s.pool.QueryRow(ctx, `SELECT document FROM class_profile WHERE entity_id = $1`, entityID).Scan(&docJSON); err == nil {
			var doc domain.ClassDocument
			_ = json.Unmarshal(docJSON, &doc)
			if doc.SubClassOf != "" {
				if err := s.insertRDFTriple(ctx, "entity", qid, subj, rdfPredicateSubClass, "<"+rdfNSEntity+doc.SubClassOf+">", now); err != nil {
					return err
				}
			}
		}
	}
	if en, ok := labels["en"]; ok && en != "" {
		return s.insertRDFTriple(ctx, "entity", qid, subj, rdfPredicateLabel, quoteLiteral(en), now)
	}
	return nil
}

func (s *Store) projectStatementRDF(ctx context.Context, sid string) error {
	row := s.pool.QueryRow(ctx, `
		SELECT st.status, e.public_id, p.public_id, sc.value_type,
			sc.value_text, sc.value_bool, sc.value_int64, sc.value_numeric::text, sc.value_date::text,
			sc.value_entity_id
		FROM statement st
		JOIN entity e ON e.id = st.subject_id
		JOIN property_profile pp ON pp.entity_id = st.property_id
		JOIN entity p ON p.id = pp.entity_id
		LEFT JOIN statement_current sc ON sc.statement_id = st.id
		WHERE st.public_id = $1
	`, sid)
	var status, subject, property, valueType string
	var valueText, numeric, date *string
	var valueBool *bool
	var valueInt *int64
	var valueEntity *uuid.UUID
	if err := row.Scan(&status, &subject, &property, &valueType, &valueText, &valueBool, &valueInt, &numeric, &date, &valueEntity); err != nil {
		return err
	}
	_, _ = s.pool.Exec(ctx, `DELETE FROM projection_rdf WHERE object_type = 'statement' AND public_id = $1`, sid)
	if status == "deleted" {
		return nil
	}
	obj, err := formatRDFObject(ctx, s, valueType, valueText, valueBool, valueInt, numeric, date, valueEntity)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	subj := rdfNSEntity + subject
	pred := rdfNSProperty + property
	if err := s.insertRDFTriple(ctx, "statement", sid, subj, pred, obj, now); err != nil {
		return err
	}
	return s.insertRDFTriple(ctx, "statement", sid, rdfNSStmt+sid, rdfPredicateType, "<"+rdfTypeStatement+">", now)
}

func formatRDFObject(ctx context.Context, s *Store, valueType string, text *string, b *bool, i *int64, numeric, date *string, entityID *uuid.UUID) (string, error) {
	switch valueType {
	case "String", "URI":
		if text != nil {
			return quoteLiteral(*text), nil
		}
	case "Boolean":
		if b != nil {
			return fmt.Sprintf(`"%t"^^<http://www.w3.org/2001/XMLSchema#boolean>`, *b), nil
		}
	case "Integer":
		if i != nil {
			return fmt.Sprintf(`"%d"^^<http://www.w3.org/2001/XMLSchema#integer>`, *i), nil
		}
	case "Decimal":
		if numeric != nil {
			return fmt.Sprintf(`"%s"^^<http://www.w3.org/2001/XMLSchema#decimal>`, *numeric), nil
		}
	case "Date":
		if date != nil {
			return fmt.Sprintf(`"%s"^^<http://www.w3.org/2001/XMLSchema#date>`, *date), nil
		}
	case "EntityReference":
		if entityID != nil {
			var qid string
			if err := s.pool.QueryRow(ctx, `SELECT public_id FROM entity WHERE id = $1`, *entityID).Scan(&qid); err != nil {
				return "", err
			}
			return "<" + rdfNSEntity + qid + ">", nil
		}
	}
	return `""`, nil
}

func quoteLiteral(s string) string {
	escaped := strings.ReplaceAll(s, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	escaped = strings.ReplaceAll(escaped, "\n", `\n`)
	return `"` + escaped + `"`
}

func (s *Store) insertRDFTriple(ctx context.Context, objectType, publicID, subject, predicate, object string, now time.Time) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO projection_rdf (id, object_type, public_id, subject, predicate, object, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (object_type, public_id, predicate, object) DO UPDATE SET
			subject = EXCLUDED.subject,
			updated_at = EXCLUDED.updated_at
	`, datatype.NewUUID(), objectType, publicID, subject, predicate, object, now)
	return err
}

func (s *Store) RebuildRDFProjection(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `TRUNCATE projection_rdf`); err != nil {
		return err
	}

	entRows, err := tx.Query(ctx, `SELECT public_id FROM entity WHERE status <> 'deleted' ORDER BY public_id`)
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
		if err := s.projectEntityRDFTx(ctx, tx, qid); err != nil {
			return err
		}
	}

	stmtRows, err := tx.Query(ctx, `SELECT public_id FROM statement WHERE status = 'active' ORDER BY public_id`)
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
		if err := s.projectStatementRDFTx(ctx, tx, sid); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) projectEntityRDFTx(ctx context.Context, tx pgx.Tx, qid string) error {
	var entityID uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM entity WHERE public_id = $1`, qid).Scan(&entityID); err != nil {
		return err
	}
	labels, err := s.loadLabelsTx(ctx, tx, `SELECT lang, text FROM entity_label WHERE entity_id = $1`, entityID)
	if err != nil {
		return err
	}
	subj := rdfNSEntity + qid
	now := time.Now().UTC()
	if err := insertRDFTripleTx(ctx, tx, "entity", qid, subj, rdfPredicateType, "<"+rdfTypeEntity+">", now); err != nil {
		return err
	}
	var hasProp, hasClass bool
	_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM property_profile WHERE entity_id = $1)`, entityID).Scan(&hasProp)
	_ = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM class_profile WHERE entity_id = $1)`, entityID).Scan(&hasClass)
	if hasProp {
		if err := insertRDFTripleTx(ctx, tx, "entity", qid, subj, rdfPredicateType, "<"+rdfTypeProperty+">", now); err != nil {
			return err
		}
	}
	if hasClass {
		if err := insertRDFTripleTx(ctx, tx, "entity", qid, subj, rdfPredicateType, "<"+rdfTypeClass+">", now); err != nil {
			return err
		}
		var docJSON []byte
		if err := tx.QueryRow(ctx, `SELECT document FROM class_profile WHERE entity_id = $1`, entityID).Scan(&docJSON); err == nil {
			var doc domain.ClassDocument
			_ = json.Unmarshal(docJSON, &doc)
			if doc.SubClassOf != "" {
				if err := insertRDFTripleTx(ctx, tx, "entity", qid, subj, rdfPredicateSubClass, "<"+rdfNSEntity+doc.SubClassOf+">", now); err != nil {
					return err
				}
			}
		}
	}
	if en, ok := labels["en"]; ok && en != "" {
		return insertRDFTripleTx(ctx, tx, "entity", qid, subj, rdfPredicateLabel, quoteLiteral(en), now)
	}
	return nil
}

func (s *Store) projectStatementRDFTx(ctx context.Context, tx pgx.Tx, sid string) error {
	row := tx.QueryRow(ctx, `
		SELECT e.public_id, p.public_id, sc.value_type,
			sc.value_text, sc.value_bool, sc.value_int64, sc.value_numeric::text, sc.value_date::text,
			sc.value_entity_id
		FROM statement st
		JOIN entity e ON e.id = st.subject_id
		JOIN property_profile pp ON pp.entity_id = st.property_id
		JOIN entity p ON p.id = pp.entity_id
		LEFT JOIN statement_current sc ON sc.statement_id = st.id
		WHERE st.public_id = $1 AND st.status = 'active'
	`, sid)
	var subject, property, valueType string
	var valueText, numeric, date *string
	var valueBool *bool
	var valueInt *int64
	var valueEntity *uuid.UUID
	if err := row.Scan(&subject, &property, &valueType, &valueText, &valueBool, &valueInt, &numeric, &date, &valueEntity); err != nil {
		return err
	}
	obj, err := formatRDFObjectTx(ctx, tx, valueType, valueText, valueBool, valueInt, numeric, date, valueEntity)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := insertRDFTripleTx(ctx, tx, "statement", sid, rdfNSEntity+subject, rdfNSProperty+property, obj, now); err != nil {
		return err
	}
	return insertRDFTripleTx(ctx, tx, "statement", sid, rdfNSStmt+sid, rdfPredicateType, "<"+rdfTypeStatement+">", now)
}

func formatRDFObjectTx(ctx context.Context, tx pgx.Tx, valueType string, text *string, b *bool, i *int64, numeric, date *string, entityID *uuid.UUID) (string, error) {
	switch valueType {
	case "String", "URI":
		if text != nil {
			return quoteLiteral(*text), nil
		}
	case "Boolean":
		if b != nil {
			return fmt.Sprintf(`"%t"^^<http://www.w3.org/2001/XMLSchema#boolean>`, *b), nil
		}
	case "Integer":
		if i != nil {
			return fmt.Sprintf(`"%d"^^<http://www.w3.org/2001/XMLSchema#integer>`, *i), nil
		}
	case "Decimal":
		if numeric != nil {
			return fmt.Sprintf(`"%s"^^<http://www.w3.org/2001/XMLSchema#decimal>`, *numeric), nil
		}
	case "Date":
		if date != nil {
			return fmt.Sprintf(`"%s"^^<http://www.w3.org/2001/XMLSchema#date>`, *date), nil
		}
	case "EntityReference":
		if entityID != nil {
			var qid string
			if err := tx.QueryRow(ctx, `SELECT public_id FROM entity WHERE id = $1`, *entityID).Scan(&qid); err != nil {
				return "", err
			}
			return "<" + rdfNSEntity + qid + ">", nil
		}
	}
	return `""`, nil
}

func insertRDFTripleTx(ctx context.Context, tx pgx.Tx, objectType, publicID, subject, predicate, object string, now time.Time) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO projection_rdf (id, object_type, public_id, subject, predicate, object, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (object_type, public_id, predicate, object) DO UPDATE SET
			subject = EXCLUDED.subject,
			updated_at = EXCLUDED.updated_at
	`, datatype.NewUUID(), objectType, publicID, subject, predicate, object, now)
	return err
}

func (s *Store) ExportRDF(ctx context.Context) (string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT subject, predicate, object FROM projection_rdf ORDER BY subject, predicate, object
	`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var b strings.Builder
	for rows.Next() {
		var subj, pred, obj string
		if err := rows.Scan(&subj, &pred, &obj); err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "<%s> <%s> %s .\n", subj, pred, obj)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	return b.String(), nil
}
