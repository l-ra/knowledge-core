-- +goose Up
INSERT INTO id_counter (kind, last_value) VALUES ('class', 0)
ON CONFLICT (kind) DO NOTHING;

-- Schema profile: entity with public_id C* is a class when this row exists.
CREATE TABLE class_profile (
    entity_id  UUID PRIMARY KEY REFERENCES entity (id) ON DELETE CASCADE,
    document   JSONB NOT NULL DEFAULT '{}'
);

CREATE TABLE shape_profile (
    id         UUID PRIMARY KEY,
    code       TEXT NOT NULL UNIQUE,
    class_id   UUID NOT NULL REFERENCES class_profile (entity_id) ON DELETE CASCADE,
    document   JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX shape_profile_class_idx ON shape_profile (class_id);

CREATE TABLE model_schema_config (
    id                    INT PRIMARY KEY CHECK (id = 1),
    instance_of_property  TEXT NOT NULL DEFAULT '',
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO model_schema_config (id, instance_of_property) VALUES (1, '');

CREATE TABLE validation_report (
    id          UUID PRIMARY KEY,
    scope       TEXT NOT NULL,
    entity_id   UUID REFERENCES entity (id) ON DELETE SET NULL,
    entity_qid  TEXT,
    findings    JSONB NOT NULL DEFAULT '[]',
    summary     JSONB NOT NULL DEFAULT '{}',
    created_by  TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX validation_report_entity_idx ON validation_report (entity_qid);

-- +goose Down
DROP TABLE IF EXISTS validation_report;
DROP TABLE IF EXISTS model_schema_config;
DROP TABLE IF EXISTS shape_profile;
DROP TABLE IF EXISTS class_profile;
DELETE FROM id_counter WHERE kind = 'class';
