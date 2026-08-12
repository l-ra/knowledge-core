package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/l-ra/knowledge-core/internal/domain"
)

func (s *Store) GetChangeSetDraft(ctx context.Context, actor string) (*domain.ChangeSetDraft, error) {
	var raw []byte
	var updated, created time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT document, updated_at, created_at FROM user_changeset_draft WHERE actor = $1
	`, actor).Scan(&raw, &updated, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		return &domain.ChangeSetDraft{Open: false, Operations: []domain.ChangeOperation{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var draft domain.ChangeSetDraft
	if err := json.Unmarshal(raw, &draft); err != nil {
		return nil, err
	}
	if draft.Operations == nil {
		draft.Operations = []domain.ChangeOperation{}
	}
	draft.UpdatedAt = updated.UTC().Format(time.RFC3339Nano)
	draft.CreatedAt = created.UTC().Format(time.RFC3339Nano)
	return &draft, nil
}

func (s *Store) PutChangeSetDraft(ctx context.Context, actor string, draft domain.ChangeSetDraft) (*domain.ChangeSetDraft, error) {
	if draft.Operations == nil {
		draft.Operations = []domain.ChangeOperation{}
	}
	now := time.Now().UTC()
	raw, err := json.Marshal(draft)
	if err != nil {
		return nil, err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO user_changeset_draft (actor, document, updated_at, created_at)
		VALUES ($1, $2, $3, $3)
		ON CONFLICT (actor) DO UPDATE SET document = EXCLUDED.document, updated_at = EXCLUDED.updated_at
	`, actor, raw, now)
	if err != nil {
		return nil, err
	}
	draft.UpdatedAt = now.Format(time.RFC3339Nano)
	if draft.CreatedAt == "" {
		draft.CreatedAt = draft.UpdatedAt
	}
	return &draft, nil
}

func (s *Store) DeleteChangeSetDraft(ctx context.Context, actor string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM user_changeset_draft WHERE actor = $1`, actor)
	return err
}
