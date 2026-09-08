package store

import (
	"context"
	"errors"
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
)

func fallbackNSForPublicID(publicID string) string {
	if datatype.IsIRI(publicID) {
		return ""
	}
	if strings.HasPrefix(publicID, "P") {
		return rdfNSProperty
	}
	return rdfNSEntity
}

func packageFallbackBase(packageCode string) string {
	packageCode = strings.TrimSpace(packageCode)
	if packageCode == "" {
		return "urn:kc:resource:"
	}
	return "urn:kc:" + packageCode + ":"
}

func resolvePublicIRI(base, iriLocal, fallbackLocal, packageCode string) string {
	local := strings.TrimSpace(iriLocal)
	if local == "" {
		local = strings.TrimSpace(fallbackLocal)
	}
	base = strings.TrimSpace(base)
	if base != "" {
		return datatype.ResolveIRI(base, local, fallbackLocal, "")
	}
	return packageFallbackBase(packageCode) + local
}

func (s *Store) packageIRIBaseByID(ctx context.Context, q pgx.Tx, packageID interface{}) (string, string, error) {
	var code, base string
	err := q.QueryRow(ctx, `SELECT code, COALESCE(iri_base,'') FROM package WHERE id = $1`, packageID).Scan(&code, &base)
	if err != nil {
		return "", "", err
	}
	return code, base, nil
}

func (s *Store) resolveEntityIRI(ctx context.Context, publicID string) (string, error) {
	var base, local string
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(p.iri_base, ''), COALESCE(e.iri_local, '')
		FROM entity e
		LEFT JOIN package p ON p.id = e.package_id
		WHERE e.public_id = $1
	`, publicID).Scan(&base, &local)
	if err != nil {
		return "", err
	}
	return datatype.ResolveIRI(base, local, publicID, fallbackNSForPublicID(publicID)), nil
}

func (s *Store) resolveEntityIRITx(ctx context.Context, tx pgx.Tx, publicID string) (string, error) {
	var base, local string
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(p.iri_base, ''), COALESCE(e.iri_local, '')
		FROM entity e
		LEFT JOIN package p ON p.id = e.package_id
		WHERE e.public_id = $1
	`, publicID).Scan(&base, &local)
	if err != nil {
		return "", err
	}
	return datatype.ResolveIRI(base, local, publicID, fallbackNSForPublicID(publicID)), nil
}

func (s *Store) fillEntityIRI(ctx context.Context, e *domain.Entity) error {
	if e == nil {
		return nil
	}
	var base string
	if e.PackageCode != "" {
		_ = s.pool.QueryRow(ctx, `SELECT COALESCE(iri_base,'') FROM package WHERE code = $1`, e.PackageCode).Scan(&base)
	}
	e.IRI = datatype.ResolveIRI(base, e.IRILocal, e.PublicID, fallbackNSForPublicID(e.PublicID))
	aliases, err := s.loadEntityIRIAliases(ctx, e.ID)
	if err != nil {
		return err
	}
	e.IRIAliases = aliases
	return nil
}

func (s *Store) loadEntityIRIAliases(ctx context.Context, entityID interface{}) ([]domain.EntityIRIAlias, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT iri, kind FROM entity_iri_alias WHERE entity_id = $1 ORDER BY iri
	`, entityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.EntityIRIAlias, 0)
	for rows.Next() {
		var a domain.EntityIRIAlias
		if err := rows.Scan(&a.IRI, &a.Kind); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func normalizeOptionalIRILocal(raw string) (string, error) {
	return datatype.NormalizeIRILocal(raw)
}

func generatedIRILocal(kind string) string {
	switch strings.TrimSpace(kind) {
	case "entity":
		return datatype.NewFallbackIRILocal("e")
	case "property":
		return datatype.NewFallbackIRILocal("p")
	case "class":
		return datatype.NewFallbackIRILocal("c")
	case "statement":
		return datatype.NewFallbackIRILocal("s")
	case "reference":
		return datatype.NewFallbackIRILocal("r")
	default:
		return datatype.NewFallbackIRILocal("id")
	}
}

func normalizeEntityIRIAliases(aliases []domain.EntityIRIAlias) ([]domain.EntityIRIAlias, error) {
	out := make([]domain.EntityIRIAlias, 0, len(aliases))
	seen := map[string]struct{}{}
	for _, a := range aliases {
		iri := strings.TrimSpace(a.IRI)
		kind := strings.TrimSpace(a.Kind)
		if iri == "" {
			continue
		}
		if kind == "" {
			kind = "sameAs"
		}
		if kind != "sameAs" && kind != "imported" && kind != "canonical_export" {
			return nil, fmt.Errorf("invalid alias kind %q", kind)
		}
		if !strings.HasPrefix(iri, "http://") && !strings.HasPrefix(iri, "https://") {
			return nil, fmt.Errorf("alias iri must be absolute http(s)")
		}
		if strings.ContainsAny(iri, " \t\n\r") {
			return nil, fmt.Errorf("alias iri must not contain whitespace")
		}
		if _, ok := seen[iri]; ok {
			continue
		}
		seen[iri] = struct{}{}
		out = append(out, domain.EntityIRIAlias{IRI: iri, Kind: kind})
	}
	return out, nil
}

func (s *Store) replaceEntityIRIAliasesTx(ctx context.Context, tx pgx.Tx, entityID interface{}, aliases []domain.EntityIRIAlias) error {
	norm, err := normalizeEntityIRIAliases(aliases)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM entity_iri_alias WHERE entity_id = $1`, entityID); err != nil {
		return err
	}
	for _, a := range norm {
		if _, err := tx.Exec(ctx, `
			INSERT INTO entity_iri_alias (entity_id, iri, kind) VALUES ($1,$2,$3)
		`, entityID, a.IRI, a.Kind); err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("%w: alias IRI already used by another entity", ErrConflict)
			}
			return err
		}
	}
	return nil
}

func (s *Store) assertAliasIRIsAvailableTx(
	ctx context.Context,
	tx pgx.Tx,
	csID uuid.UUID,
	entityID uuid.UUID,
	aliases []domain.EntityIRIAlias,
) error {
	for _, a := range aliases {
		var otherID uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT entity_id FROM entity_iri_alias WHERE iri = $1 AND entity_id <> $2
		`, a.IRI, entityID).Scan(&otherID)
		if err == nil {
			return fmt.Errorf("%w: alias IRI %q already used by another entity", ErrConflict, a.IRI)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var ownerPub string
		err = tx.QueryRow(ctx, `
			SELECT o.public_id
			FROM changeset_entity_overlay o
			JOIN change_set cs ON cs.id = o.changeset_id AND cs.status = 'open'
			WHERE o.object_id <> $1
			  AND o.iri_aliases_json IS NOT NULL
			  AND EXISTS (
				SELECT 1 FROM jsonb_array_elements(o.iri_aliases_json) el
				WHERE el->>'iri' = $2
			  )
			LIMIT 1
		`, entityID, a.IRI).Scan(&ownerPub)
		if err == nil {
			return fmt.Errorf("%w: alias IRI %q already pending on %s", ErrConflict, a.IRI, ownerPub)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		_ = csID
	}
	return nil
}

func (s *Store) SetEntityIRIAliases(ctx context.Context, meta domain.WriteMeta, publicID string, aliases []domain.EntityIRIAlias) (*domain.WriteResult[domain.Entity], error) {
	if meta.OpenChangeSetID != "" {
		return s.SetEntityIRIAliasesInOpenChangeSet(ctx, meta, publicID, aliases)
	}

	norm, err := normalizeEntityIRIAliases(aliases)
	if err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var entityID uuid.UUID
	var status string
	if err := tx.QueryRow(ctx, `SELECT id, status FROM entity WHERE public_id = $1`, publicID).Scan(&entityID, &status); err != nil {
		return nil, err
	}
	if err := errIfEntityDeleted(status); err != nil {
		return nil, err
	}
	if err := s.replaceEntityIRIAliasesTx(ctx, tx, entityID, norm); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	_ = s.projectEntityRDF(ctx, publicID)
	ent, err := s.GetEntityByPublicID(ctx, publicID)
	if err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Entity]{Value: *ent}, nil
}

func (s *Store) SetEntityIRIAliasesInOpenChangeSet(
	ctx context.Context,
	meta domain.WriteMeta,
	publicID string,
	aliases []domain.EntityIRIAlias,
) (*domain.WriteResult[domain.Entity], error) {
	norm, err := normalizeEntityIRIAliases(aliases)
	if err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	row, err := s.loadOpenChangeSetTx(ctx, tx, meta.OpenChangeSetID)
	if err != nil {
		return nil, err
	}
	if err := s.assertOpenChangeSetActor(row, meta.Actor); err != nil {
		return nil, err
	}

	ent, baseRev, fromOverlay, err := s.resolveEntityForOpenWrite(ctx, tx, row.id, publicID)
	if err != nil {
		return nil, err
	}
	if err := errIfEntityDeleted(string(ent.Status)); err != nil {
		return nil, err
	}
	if err := s.assertAliasIRIsAvailableTx(ctx, tx, row.id, ent.ID, norm); err != nil {
		return nil, err
	}

	ent.IRIAliases = norm
	ent.UpdatedAt = time.Now().UTC()
	opKind := "update"
	if fromOverlay && baseRev == 0 {
		opKind = "create"
	}
	if err := s.claimObjectTx(ctx, tx, row.id, "entity", ent.ID, ent.PublicID, baseRev, opKind); err != nil {
		return nil, err
	}
	dt, cons, sub := entityOverlayProfile(ent)
	if err := s.upsertEntityOverlayTx(ctx, tx, row.id, *ent, dt, cons, sub); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &domain.WriteResult[domain.Entity]{Value: *ent, ChangeSet: s.openChangeSetDomain(row)}, nil
}
