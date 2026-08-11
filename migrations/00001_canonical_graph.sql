-- +goose Up
CREATE TABLE id_counter (
    kind       TEXT PRIMARY KEY,
    last_value BIGINT NOT NULL DEFAULT 0 CHECK (last_value >= 0)
);

INSERT INTO id_counter (kind, last_value) VALUES
    ('entity', 0),
    ('property', 0),
    ('statement', 0),
    ('reference', 0);

CREATE TABLE entity (
    id         UUID PRIMARY KEY,
    public_id  TEXT NOT NULL UNIQUE,
    status     TEXT NOT NULL CHECK (status IN ('active', 'deprecated', 'redirected', 'deleted')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE entity_label (
    entity_id UUID NOT NULL REFERENCES entity (id) ON DELETE CASCADE,
    lang      TEXT NOT NULL,
    text      TEXT NOT NULL CHECK (length(trim(text)) > 0),
    PRIMARY KEY (entity_id, lang)
);

CREATE TABLE entity_description (
    entity_id UUID NOT NULL REFERENCES entity (id) ON DELETE CASCADE,
    lang      TEXT NOT NULL,
    text      TEXT NOT NULL,
    PRIMARY KEY (entity_id, lang)
);

-- Schema profile: entity with public_id P* is a property when this row exists.
CREATE TABLE property_profile (
    entity_id   UUID PRIMARY KEY REFERENCES entity (id) ON DELETE CASCADE,
    datatype    TEXT NOT NULL,
    constraints JSONB NOT NULL DEFAULT '{}'
);

CREATE TABLE change_set (
    id              UUID PRIMARY KEY,
    actor           TEXT,
    committed_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    idempotency_key TEXT UNIQUE,
    correlation_id  TEXT
);

CREATE TABLE change_set_item (
    id            UUID PRIMARY KEY,
    change_set_id UUID NOT NULL REFERENCES change_set (id) ON DELETE CASCADE,
    object_type   TEXT NOT NULL,
    object_id     UUID NOT NULL,
    public_id     TEXT,
    op            TEXT NOT NULL,
    payload       JSONB
);

CREATE INDEX change_set_item_cs_idx ON change_set_item (change_set_id);

CREATE TABLE statement (
    id                 UUID PRIMARY KEY,
    public_id          TEXT NOT NULL UNIQUE,
    subject_id         UUID NOT NULL REFERENCES entity (id),
    property_id        UUID NOT NULL REFERENCES property_profile (entity_id),
    status             TEXT NOT NULL CHECK (status IN ('active', 'deprecated', 'deleted')),
    value_type         TEXT NOT NULL,
    value_bool         BOOLEAN,
    value_int64        BIGINT,
    value_numeric      NUMERIC,
    value_date         DATE,
    value_timestamptz  TIMESTAMPTZ,
    value_text         TEXT,
    value_entity_id    UUID REFERENCES entity (id),
    value_json         JSONB,
    valid_from         TIMESTAMPTZ,
    valid_to           TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT statement_value_type_check CHECK (
        value_type IN (
            'EntityReference', 'String', 'LocalizedString', 'Boolean', 'Integer',
            'Decimal', 'Date', 'DateTime', 'URI', 'ExternalIdentifier', 'Quantity', 'Interval'
        )
    )
);

CREATE INDEX statement_subject_idx ON statement (subject_id);
CREATE INDEX statement_property_idx ON statement (property_id);

CREATE TABLE statement_current (
    statement_id       UUID PRIMARY KEY REFERENCES statement (id) ON DELETE CASCADE,
    public_id          TEXT NOT NULL UNIQUE,
    subject_id         UUID NOT NULL REFERENCES entity (id),
    property_id        UUID NOT NULL REFERENCES property_profile (entity_id),
    value_type         TEXT NOT NULL,
    value_bool         BOOLEAN,
    value_int64        BIGINT,
    value_numeric      NUMERIC,
    value_date         DATE,
    value_timestamptz  TIMESTAMPTZ,
    value_text         TEXT,
    value_entity_id    UUID REFERENCES entity (id),
    value_json         JSONB,
    valid_from         TIMESTAMPTZ,
    valid_to           TIMESTAMPTZ,
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX statement_current_subject_idx ON statement_current (subject_id);

-- +goose Down
DROP TABLE IF EXISTS statement_current;
DROP TABLE IF EXISTS statement;
DROP TABLE IF EXISTS change_set_item;
DROP TABLE IF EXISTS change_set;
DROP TABLE IF EXISTS property_profile;
DROP TABLE IF EXISTS entity_description;
DROP TABLE IF EXISTS entity_label;
DROP TABLE IF EXISTS entity;
DROP TABLE IF EXISTS id_counter;
