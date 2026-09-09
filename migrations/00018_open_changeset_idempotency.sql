-- +goose Up
CREATE TABLE IF NOT EXISTS open_changeset_idempotency (
    changeset_id   UUID NOT NULL REFERENCES change_set (id) ON DELETE CASCADE,
    idempotency_key TEXT NOT NULL,
    request_hash    TEXT NOT NULL,
    response_body   JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (changeset_id, idempotency_key)
);

-- +goose Down
DROP TABLE IF EXISTS open_changeset_idempotency;
