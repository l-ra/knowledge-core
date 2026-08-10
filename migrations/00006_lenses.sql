-- +goose Up
CREATE TABLE lens_definition (
    id         UUID PRIMARY KEY,
    code       TEXT NOT NULL UNIQUE,
    version    INT NOT NULL DEFAULT 1 CHECK (version > 0),
    labels     JSONB NOT NULL DEFAULT '{}',
    document   JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX lens_definition_code_idx ON lens_definition (code);

-- +goose Down
DROP TABLE IF EXISTS lens_definition;
