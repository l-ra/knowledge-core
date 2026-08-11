-- +goose Up
INSERT INTO id_counter (kind, last_value) VALUES ('changeset', 0) ON CONFLICT DO NOTHING;

ALTER TABLE change_set
    ADD COLUMN IF NOT EXISTS public_id TEXT UNIQUE,
    ADD COLUMN IF NOT EXISTS operation_type TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS comment TEXT,
    ADD COLUMN IF NOT EXISTS request_hash TEXT,
    ADD COLUMN IF NOT EXISTS response_body JSONB;

ALTER TABLE entity
    ADD COLUMN IF NOT EXISTS current_revision_no INT NOT NULL DEFAULT 1;

ALTER TABLE statement
    ADD COLUMN IF NOT EXISTS current_revision_no INT NOT NULL DEFAULT 1;

CREATE TABLE entity_revision (
    id           UUID PRIMARY KEY,
    entity_id    UUID NOT NULL REFERENCES entity (id) ON DELETE CASCADE,
    revision_no  INT NOT NULL CHECK (revision_no > 0),
    status       TEXT NOT NULL,
    labels       JSONB NOT NULL DEFAULT '{}',
    descriptions JSONB NOT NULL DEFAULT '{}',
    change_set_id UUID REFERENCES change_set (id),
    actor        TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (entity_id, revision_no)
);

CREATE TABLE statement_revision (
    id                UUID PRIMARY KEY,
    statement_id      UUID NOT NULL REFERENCES statement (id) ON DELETE CASCADE,
    revision_no       INT NOT NULL CHECK (revision_no > 0),
    status            TEXT NOT NULL,
    value_type        TEXT NOT NULL,
    value_bool        BOOLEAN,
    value_int64       BIGINT,
    value_numeric     NUMERIC,
    value_date        DATE,
    value_timestamptz TIMESTAMPTZ,
    value_text        TEXT,
    value_entity_id   UUID REFERENCES entity (id),
    value_json        JSONB,
    valid_from        TIMESTAMPTZ,
    valid_to          TIMESTAMPTZ,
    change_set_id     UUID REFERENCES change_set (id),
    actor             TEXT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (statement_id, revision_no)
);

CREATE INDEX entity_revision_entity_idx ON entity_revision (entity_id, revision_no DESC);
CREATE INDEX statement_revision_statement_idx ON statement_revision (statement_id, revision_no DESC);

-- +goose Down
DROP TABLE IF EXISTS statement_revision;
DROP TABLE IF EXISTS entity_revision;

ALTER TABLE statement DROP COLUMN IF EXISTS current_revision_no;
ALTER TABLE entity DROP COLUMN IF EXISTS current_revision_no;

ALTER TABLE change_set
    DROP COLUMN IF EXISTS response_body,
    DROP COLUMN IF EXISTS request_hash,
    DROP COLUMN IF EXISTS comment,
    DROP COLUMN IF EXISTS operation_type,
    DROP COLUMN IF EXISTS public_id;

DELETE FROM id_counter WHERE kind = 'changeset';
