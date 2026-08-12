package store

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/l-ra/knowledge-core/internal/datatype"
	"github.com/l-ra/knowledge-core/internal/domain"
)

const (
	rdfNSEntity   = "https://knowledge-core.local/entity/"
	rdfNSProperty = "https://knowledge-core.local/property/"
)

func fallbackNSForPublicID(publicID string) string {
	if strings.HasPrefix(publicID, "P") {
		return rdfNSProperty
	}
	return rdfNSEntity
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

func (s *Store) SetEntityIRIAliases(ctx context.Context, publicID string, aliases []domain.EntityIRIAlias) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var entityID interface{}
	if err := tx.QueryRow(ctx, `SELECT id FROM entity WHERE public_id = $1`, publicID).Scan(&entityID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM entity_iri_alias WHERE entity_id = $1`, entityID); err != nil {
		return err
	}
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
			return fmt.Errorf("invalid alias kind %q", kind)
		}
		if !strings.HasPrefix(iri, "http://") && !strings.HasPrefix(iri, "https://") {
			return fmt.Errorf("alias iri must be absolute http(s)")
		}
		if strings.ContainsAny(iri, " \t\n\r") {
			return fmt.Errorf("alias iri must not contain whitespace")
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO entity_iri_alias (entity_id, iri, kind) VALUES ($1,$2,$3)
		`, entityID, iri, kind); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	_ = s.projectEntityRDF(ctx, publicID)
	return nil
}
