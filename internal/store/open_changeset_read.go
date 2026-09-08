package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/l-ra/knowledge-core/internal/domain"
)

func (s *Store) resolveActiveChangeSetIDs(ctx context.Context, publicIDs []string) ([]uuid.UUID, error) {
	if len(publicIDs) == 0 {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, public_id, status FROM change_set WHERE public_id = ANY($1)
	`, publicIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := map[string]uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		var pid, status string
		if err := rows.Scan(&id, &pid, &status); err != nil {
			return nil, err
		}
		if status != string(domain.ChangeSetOpen) {
			return nil, fmt.Errorf("%w: changeset %s is not open", ErrOpenChangeSetClosed, pid)
		}
		found[pid] = id
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(publicIDs))
	seenObj := map[uuid.UUID]string{}
	for _, pid := range publicIDs {
		id, ok := found[pid]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrOpenChangeSetNotFound, pid)
		}
		// detect overlapping claims across requested CS set
		claimRows, err := s.pool.Query(ctx, `SELECT object_id FROM changeset_object_claim WHERE changeset_id = $1`, id)
		if err != nil {
			return nil, err
		}
		for claimRows.Next() {
			var oid uuid.UUID
			if err := claimRows.Scan(&oid); err != nil {
				claimRows.Close()
				return nil, err
			}
			if other, ok := seenObj[oid]; ok {
				claimRows.Close()
				return nil, fmt.Errorf("active changesets %s and %s claim the same object", other, pid)
			}
			seenObj[oid] = pid
		}
		claimRows.Close()
		out = append(out, id)
	}
	return out, nil
}

func (s *Store) loadEntityOverlayByPublicID(ctx context.Context, csIDs []uuid.UUID, publicID string) (*domain.Entity, error) {
	var objectID uuid.UUID
	var ovPublicID, pkgCode, iriLocal, status, kind string
	var labelsJSON, descJSON, constraintsJSON, aliasesJSON []byte
	var datatypeStr, subclassOf *string
	var rev int
	var created, updated time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT object_id, public_id, package_code, iri_local, status, labels, descriptions, kind,
			datatype, constraints, subclass_of, revision_no, created_at, updated_at, iri_aliases_json
		FROM changeset_entity_overlay
		WHERE changeset_id = ANY($1) AND public_id = $2
		LIMIT 1
	`, csIDs, publicID).Scan(&objectID, &ovPublicID, &pkgCode, &iriLocal, &status, &labelsJSON, &descJSON, &kind,
		&datatypeStr, &constraintsJSON, &subclassOf, &rev, &created, &updated, &aliasesJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ent := &domain.Entity{
		ID: objectID, PublicID: ovPublicID, PackageCode: pkgCode, IRILocal: iriLocal,
		Status: domain.EntityStatus(status), Kind: domain.EntityKind(kind), RevisionNo: rev,
		CreatedAt: created, UpdatedAt: updated, IRI: ovPublicID,
	}
	ent.Labels, _ = jsonToLabels(labelsJSON)
	ent.Descriptions, _ = jsonToLabels(descJSON)
	attachOverlayProfiles(ent, datatypeStr, constraintsJSON, subclassOf)
	if aliasesJSON != nil {
		_ = json.Unmarshal(aliasesJSON, &ent.IRIAliases)
		if ent.IRIAliases == nil {
			ent.IRIAliases = []domain.EntityIRIAlias{}
		}
	} else {
		aliases, err := s.loadEntityIRIAliases(ctx, objectID)
		if err != nil {
			return nil, err
		}
		ent.IRIAliases = aliases
	}
	return ent, nil
}

func (s *Store) loadStatementOverlayByPublicID(ctx context.Context, csIDs []uuid.UUID, publicID string) (*domain.Statement, error) {
	var objectID uuid.UUID
	var ovPublicID, pkgCode, subject, property, status string
	var valueJSON, qualJSON, refJSON []byte
	var rev int
	var vf, vt *time.Time
	var created, updated time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT object_id, public_id, package_code, subject_public_id, property_public_id, status,
			value_json, qualifiers_json, references_json, valid_from, valid_to, revision_no, created_at, updated_at
		FROM changeset_statement_overlay
		WHERE changeset_id = ANY($1) AND public_id = $2
		LIMIT 1
	`, csIDs, publicID).Scan(&objectID, &ovPublicID, &pkgCode, &subject, &property, &status,
		&valueJSON, &qualJSON, &refJSON, &vf, &vt, &rev, &created, &updated)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	st := &domain.Statement{
		ID: objectID, PublicID: ovPublicID, PackageCode: pkgCode,
		SubjectQID: subject, PropertyPID: property, Status: domain.StatementStatus(status),
		RevisionNo: rev, ValidFrom: vf, ValidTo: vt, CreatedAt: created, UpdatedAt: updated,
	}
	_ = json.Unmarshal(valueJSON, &st.Value)
	_ = json.Unmarshal(qualJSON, &st.Qualifiers)
	_ = json.Unmarshal(refJSON, &st.ReferenceIDs)
	return st, nil
}

func (s *Store) mergeGetEntity(ctx context.Context, qid string) (*domain.Entity, error) {
	active := ActiveChangeSetsFrom(ctx)
	if len(active) == 0 {
		return s.GetEntityByPublicIDCommitted(ctx, qid)
	}
	csIDs, err := s.resolveActiveChangeSetIDs(ctx, active)
	if err != nil {
		return nil, err
	}
	ov, err := s.loadEntityOverlayByPublicID(ctx, csIDs, qid)
	if err != nil {
		return nil, err
	}
	if ov != nil {
		if ov.Status == domain.EntityDeleted {
			return nil, pgx.ErrNoRows
		}
		return ov, nil
	}
	return s.GetEntityByPublicIDCommitted(ctx, qid)
}

// GetEntityByPublicIDCommitted is the committed-only entity read.
func (s *Store) GetEntityByPublicIDCommitted(ctx context.Context, qid string) (*domain.Entity, error) {
	var e domain.Entity
	var pkgCode *string
	err := s.pool.QueryRow(ctx, `
		SELECT e.id, e.public_id, e.status, e.current_revision_no, e.created_at, e.updated_at, p.code, COALESCE(e.iri_local,'')
		FROM entity e
		LEFT JOIN package p ON p.id = e.package_id
		WHERE e.public_id = $1
	`, qid).Scan(&e.ID, &e.PublicID, &e.Status, &e.RevisionNo, &e.CreatedAt, &e.UpdatedAt, &pkgCode, &e.IRILocal)
	if err != nil {
		return nil, err
	}
	if pkgCode != nil {
		e.PackageCode = *pkgCode
	}
	e.Labels, err = s.loadLabels(ctx, `SELECT lang, text FROM entity_label WHERE entity_id = $1`, e.ID)
	if err != nil {
		return nil, err
	}
	e.Descriptions, err = s.loadLabels(ctx, `SELECT lang, text FROM entity_description WHERE entity_id = $1`, e.ID)
	if err != nil {
		return nil, err
	}
	if err := s.attachEntityProfiles(ctx, &e); err != nil {
		return nil, err
	}
	if err := s.fillEntityIRI(ctx, &e); err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *Store) mergeListEntities(ctx context.Context, opt ListOptions, committed []domain.Entity) ([]domain.Entity, error) {
	active := ActiveChangeSetsFrom(ctx)
	if len(active) == 0 {
		return committed, nil
	}
	csIDs, err := s.resolveActiveChangeSetIDs(ctx, active)
	if err != nil {
		return nil, err
	}
	byID := map[string]domain.Entity{}
	for _, e := range committed {
		byID[e.PublicID] = e
	}
	rows, err := s.pool.Query(ctx, `
		SELECT object_id, public_id, package_code, iri_local, status, labels, descriptions, kind,
			datatype, constraints, subclass_of, revision_no, created_at, updated_at, iri_aliases_json
		FROM changeset_entity_overlay WHERE changeset_id = ANY($1)
	`, csIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var objectID uuid.UUID
		var publicID, pkgCode, iriLocal, status, kind string
		var labelsJSON, descJSON, constraintsJSON, aliasesJSON []byte
		var datatypeStr, subclassOf *string
		var rev int
		var created, updated time.Time
		if err := rows.Scan(&objectID, &publicID, &pkgCode, &iriLocal, &status, &labelsJSON, &descJSON, &kind,
			&datatypeStr, &constraintsJSON, &subclassOf, &rev, &created, &updated, &aliasesJSON); err != nil {
			return nil, err
		}
		if status == string(domain.EntityDeleted) {
			delete(byID, publicID)
			continue
		}
		if pkg := strings.TrimSpace(opt.PackageCode); pkg != "" && pkgCode != pkg {
			continue
		}
		if kindFilter := strings.ToLower(strings.TrimSpace(opt.Kind)); kindFilter != "" {
			switch kindFilter {
			case "entity", "q":
				if kind != "entity" {
					continue
				}
			case "property", "p":
				if kind != "property" {
					continue
				}
			case "class", "c":
				if kind != "class" {
					continue
				}
			}
		}
		ent := domain.Entity{
			ID: objectID, PublicID: publicID, PackageCode: pkgCode, IRILocal: iriLocal,
			Status: domain.EntityStatus(status), Kind: domain.EntityKind(kind), RevisionNo: rev,
			CreatedAt: created, UpdatedAt: updated, IRI: publicID,
		}
		ent.Labels, _ = jsonToLabels(labelsJSON)
		ent.Descriptions, _ = jsonToLabels(descJSON)
		attachOverlayProfiles(&ent, datatypeStr, constraintsJSON, subclassOf)
		if aliasesJSON != nil {
			_ = json.Unmarshal(aliasesJSON, &ent.IRIAliases)
			if ent.IRIAliases == nil {
				ent.IRIAliases = []domain.EntityIRIAlias{}
			}
		} else {
			if aliases, err := s.loadEntityIRIAliases(ctx, objectID); err == nil {
				ent.IRIAliases = aliases
			}
		}
		if q := strings.TrimSpace(opt.Query); q != "" {
			lq := strings.ToLower(q)
			match := strings.Contains(strings.ToLower(publicID), lq) || strings.Contains(strings.ToLower(iriLocal), lq)
			for _, t := range ent.Labels {
				if strings.Contains(strings.ToLower(t), lq) {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}
		byID[publicID] = ent
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if iri := strings.TrimSpace(opt.IRI); iri != "" {
		for id, e := range byID {
			if e.IRIAliases == nil {
				if aliases, err := s.loadEntityIRIAliases(ctx, e.ID); err == nil {
					e.IRIAliases = aliases
					byID[id] = e
				}
			}
			if !entityMatchesListIRI(byID[id], iri) {
				delete(byID, id)
			}
		}
	}

	out := make([]domain.Entity, 0, len(byID))
	for _, e := range byID {
		out = append(out, e)
	}
	return out, nil
}

func entityMatchesListIRI(e domain.Entity, iri string) bool {
	if e.PublicID == iri || e.IRI == iri {
		return true
	}
	for _, a := range e.IRIAliases {
		if a.IRI == iri {
			return true
		}
	}
	return false
}

func (s *Store) mergeGetStatement(ctx context.Context, sid string) (*domain.Statement, error) {
	active := ActiveChangeSetsFrom(ctx)
	if len(active) == 0 {
		return s.getStatementByPublicIDCommitted(ctx, sid)
	}
	csIDs, err := s.resolveActiveChangeSetIDs(ctx, active)
	if err != nil {
		return nil, err
	}
	ov, err := s.loadStatementOverlayByPublicID(ctx, csIDs, sid)
	if err != nil {
		return nil, err
	}
	if ov != nil {
		return ov, nil
	}
	return s.getStatementByPublicIDCommitted(ctx, sid)
}

func (s *Store) mergeListStatements(ctx context.Context, opt StatementListOptions, committed []domain.Statement) ([]domain.Statement, error) {
	active := ActiveChangeSetsFrom(ctx)
	if len(active) == 0 {
		return committed, nil
	}
	csIDs, err := s.resolveActiveChangeSetIDs(ctx, active)
	if err != nil {
		return nil, err
	}
	byID := map[string]domain.Statement{}
	for _, st := range committed {
		byID[st.PublicID] = st
	}
	rows, err := s.pool.Query(ctx, `
		SELECT object_id, public_id, package_code, subject_public_id, property_public_id, status,
			value_json, qualifiers_json, references_json, valid_from, valid_to, revision_no, created_at, updated_at
		FROM changeset_statement_overlay WHERE changeset_id = ANY($1)
	`, csIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var objectID uuid.UUID
		var publicID, pkgCode, subject, property, status string
		var valueJSON, qualJSON, refJSON []byte
		var rev int
		var vf, vt *time.Time
		var created, updated time.Time
		if err := rows.Scan(&objectID, &publicID, &pkgCode, &subject, &property, &status,
			&valueJSON, &qualJSON, &refJSON, &vf, &vt, &rev, &created, &updated); err != nil {
			return nil, err
		}
		if subj := strings.TrimSpace(opt.SubjectQID); subj != "" && subject != subj {
			continue
		}
		if prop := strings.TrimSpace(opt.PropertyPID); prop != "" && property != prop {
			continue
		}
		if status != string(domain.StatementActive) {
			delete(byID, publicID)
			continue
		}
		st := domain.Statement{
			ID: objectID, PublicID: publicID, PackageCode: pkgCode,
			SubjectQID: subject, PropertyPID: property, Status: domain.StatementStatus(status),
			RevisionNo: rev, ValidFrom: vf, ValidTo: vt, CreatedAt: created, UpdatedAt: updated,
		}
		_ = json.Unmarshal(valueJSON, &st.Value)
		_ = json.Unmarshal(qualJSON, &st.Qualifiers)
		_ = json.Unmarshal(refJSON, &st.ReferenceIDs)
		byID[publicID] = st
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]domain.Statement, 0, len(byID))
	for _, st := range byID {
		out = append(out, st)
	}
	return out, nil
}
