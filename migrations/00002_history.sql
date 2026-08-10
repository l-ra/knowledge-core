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

ALTER TABLE property_definition
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

CREATE TABLE property_revision (
    id            UUID PRIMARY KEY,
    property_id   UUID NOT NULL REFERENCES property_definition (id) ON DELETE CASCADE,
    revision_no   INT NOT NULL CHECK (revision_no > 0),
    status        TEXT NOT NULL,
    datatype      TEXT NOT NULL,
    labels        JSONB NOT NULL DEFAULT '{}',
    descriptions  JSONB NOT NULL DEFAULT '{}',
    change_set_id UUID REFERENCES change_set (id),
    actor         TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (property_id, revision_no)
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
CREATE INDEX property_revision_property_idx ON property_revision (property_id, revision_no DESC);
CREATE INDEX statement_revision_statement_idx ON statement_revision (statement_id, revision_no DESC);

-- Backfill revision 1 for existing rows (Phase 1 databases).
INSERT INTO entity_revision (id, entity_id, revision_no, status, labels, descriptions, actor, created_at)
SELECT
    gen_random_uuid(),
    e.id,
    1,
    e.status,
    COALESCE((SELECT jsonb_object_agg(lang, text) FROM entity_label el WHERE el.entity_id = e.id), '{}'::jsonb),
    COALESCE((SELECT jsonb_object_agg(lang, text) FROM entity_description ed WHERE ed.entity_id = e.id), '{}'::jsonb),
    'system',
    e.created_at
FROM entity e
WHERE NOT EXISTS (SELECT 1 FROM entity_revision er WHERE er.entity_id = e.id AND er.revision_no = 1);

INSERT INTO property_revision (id, property_id, revision_no, status, datatype, labels, descriptions, actor, created_at)
SELECT
    gen_random_uuid(),
    p.id,
    1,
    p.status,
    p.datatype,
    COALESCE((SELECT jsonb_object_agg(lang, text) FROM property_label pl WHERE pl.property_id = p.id), '{}'::jsonb),
    COALESCE((SELECT jsonb_object_agg(lang, text) FROM property_description pd WHERE pd.property_id = p.id), '{}'::jsonb),
    'system',
    p.created_at
FROM property_definition p
WHERE NOT EXISTS (SELECT 1 FROM property_revision pr WHERE pr.property_id = p.id AND pr.revision_no = 1);

INSERT INTO statement_revision (
    id, statement_id, revision_no, status, value_type,
    value_bool, value_int64, value_numeric, value_date, value_timestamptz,
    value_text, value_entity_id, value_json, valid_from, valid_to, actor, created_at
)
SELECT
    gen_random_uuid(),
    s.id,
    1,
    s.status,
    s.value_type,
    s.value_bool, s.value_int64, s.value_numeric, s.value_date, s.value_timestamptz,
    s.value_text, s.value_entity_id, s.value_json, s.valid_from, s.valid_to,
    'system',
    s.created_at
FROM statement s
WHERE NOT EXISTS (SELECT 1 FROM statement_revision sr WHERE sr.statement_id = s.id AND sr.revision_no = 1);

-- +goose Down
DROP TABLE IF EXISTS statement_revision;
DROP TABLE IF EXISTS property_revision;
DROP TABLE IF EXISTS entity_revision;

ALTER TABLE statement DROP COLUMN IF EXISTS current_revision_no;
ALTER TABLE property_definition DROP COLUMN IF EXISTS current_revision_no;
ALTER TABLE entity DROP COLUMN IF EXISTS current_revision_no;

ALTER TABLE change_set
    DROP COLUMN IF EXISTS response_body,
    DROP COLUMN IF EXISTS request_hash,
    DROP COLUMN IF EXISTS comment,
    DROP COLUMN IF EXISTS operation_type,
    DROP COLUMN IF EXISTS public_id;

DELETE FROM id_counter WHERE kind = 'changeset';
