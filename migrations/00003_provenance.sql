-- +goose Up
CREATE TABLE reference (
    id         UUID PRIMARY KEY,
    public_id  TEXT NOT NULL UNIQUE,
    fields     JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE statement_reference (
    statement_id UUID NOT NULL REFERENCES statement (id) ON DELETE CASCADE,
    reference_id UUID NOT NULL REFERENCES reference (id) ON DELETE CASCADE,
    PRIMARY KEY (statement_id, reference_id)
);

CREATE INDEX statement_reference_ref_idx ON statement_reference (reference_id);

CREATE TABLE statement_qualifier (
    id          UUID PRIMARY KEY,
    statement_id UUID NOT NULL REFERENCES statement (id) ON DELETE CASCADE,
    property_id UUID NOT NULL REFERENCES property_definition (id),
    value_type  TEXT NOT NULL,
    value_bool         BOOLEAN,
    value_int64        BIGINT,
    value_numeric      NUMERIC,
    value_date         DATE,
    value_timestamptz  TIMESTAMPTZ,
    value_text         TEXT,
    value_entity_id    UUID REFERENCES entity (id),
    value_json         JSONB,
    CONSTRAINT statement_qualifier_value_type_check CHECK (
        value_type IN (
            'EntityReference', 'String', 'LocalizedString', 'Boolean', 'Integer',
            'Decimal', 'Date', 'DateTime', 'URI', 'ExternalIdentifier', 'Quantity', 'Interval'
        )
    )
);

CREATE INDEX statement_qualifier_stmt_idx ON statement_qualifier (statement_id);

CREATE TABLE statement_revision_reference (
    statement_id UUID NOT NULL REFERENCES statement (id) ON DELETE CASCADE,
    revision_no  INT NOT NULL CHECK (revision_no > 0),
    reference_id UUID NOT NULL REFERENCES reference (id),
    PRIMARY KEY (statement_id, revision_no, reference_id)
);

CREATE TABLE statement_revision_qualifier (
    id           UUID PRIMARY KEY,
    statement_id UUID NOT NULL REFERENCES statement (id) ON DELETE CASCADE,
    revision_no  INT NOT NULL CHECK (revision_no > 0),
    property_id  UUID NOT NULL REFERENCES property_definition (id),
    value_type   TEXT NOT NULL,
    value_bool         BOOLEAN,
    value_int64        BIGINT,
    value_numeric      NUMERIC,
    value_date         DATE,
    value_timestamptz  TIMESTAMPTZ,
    value_text         TEXT,
    value_entity_id    UUID REFERENCES entity (id),
    value_json         JSONB
);

CREATE INDEX statement_revision_qualifier_idx ON statement_revision_qualifier (statement_id, revision_no);

-- +goose Down
DROP TABLE IF EXISTS statement_revision_qualifier;
DROP TABLE IF EXISTS statement_revision_reference;
DROP TABLE IF EXISTS statement_qualifier;
DROP TABLE IF EXISTS statement_reference;
DROP TABLE IF EXISTS reference;
