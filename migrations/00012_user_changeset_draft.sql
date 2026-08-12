-- +goose Up
CREATE TABLE user_changeset_draft (
    actor      TEXT PRIMARY KEY,
    document   JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE IF EXISTS user_changeset_draft;
