package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type AuthRuntime struct {
	AuthMode     string
	OIDCIssuer   string
	OIDCClientID string
	OIDCAudience string
	UpdatedAt    time.Time
	UpdatedBy    string
}

func (s *Store) GetAuthRuntime(ctx context.Context) (*AuthRuntime, error) {
	var row AuthRuntime
	err := s.pool.QueryRow(ctx, `
		SELECT auth_mode, oidc_issuer, oidc_client_id, oidc_audience, updated_at, updated_by
		FROM auth_runtime WHERE id = 1`).Scan(
		&row.AuthMode, &row.OIDCIssuer, &row.OIDCClientID, &row.OIDCAudience, &row.UpdatedAt, &row.UpdatedBy,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) UpsertAuthRuntime(ctx context.Context, row AuthRuntime) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO auth_runtime (id, auth_mode, oidc_issuer, oidc_client_id, oidc_audience, updated_at, updated_by)
		VALUES (1, $1, $2, $3, $4, now(), $5)
		ON CONFLICT (id) DO UPDATE SET
			auth_mode = EXCLUDED.auth_mode,
			oidc_issuer = EXCLUDED.oidc_issuer,
			oidc_client_id = EXCLUDED.oidc_client_id,
			oidc_audience = EXCLUDED.oidc_audience,
			updated_at = now(),
			updated_by = EXCLUDED.updated_by`,
		row.AuthMode, row.OIDCIssuer, row.OIDCClientID, row.OIDCAudience, row.UpdatedBy,
	)
	return err
}
