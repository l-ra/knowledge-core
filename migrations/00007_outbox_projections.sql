-- +goose Up
CREATE TABLE outbox_event (
    id              UUID PRIMARY KEY,
    change_set_id   UUID NOT NULL REFERENCES change_set (id) ON DELETE CASCADE,
    event_type      TEXT NOT NULL,
    aggregate_type  TEXT NOT NULL,
    aggregate_id    TEXT NOT NULL,
    payload         JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at    TIMESTAMPTZ
);

CREATE INDEX outbox_event_pending_idx ON outbox_event (created_at)
    WHERE published_at IS NULL;

CREATE TABLE projection_search (
    id           UUID PRIMARY KEY,
    object_type  TEXT NOT NULL,
    public_id    TEXT NOT NULL,
    subject_qid  TEXT,
    property_pid TEXT,
    search_text  TEXT NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (object_type, public_id)
);

CREATE INDEX projection_search_text_idx ON projection_search (search_text text_pattern_ops);

-- +goose Down
DROP TABLE IF EXISTS projection_search;
DROP TABLE IF EXISTS outbox_event;
