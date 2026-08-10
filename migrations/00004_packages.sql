-- +goose Up
CREATE TABLE package (
    id         UUID PRIMARY KEY,
    code       TEXT NOT NULL UNIQUE,
    lifecycle  TEXT NOT NULL CHECK (lifecycle IN ('released', 'continuous')),
    labels     JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE package_dependency (
    package_id       UUID NOT NULL REFERENCES package (id) ON DELETE CASCADE,
    depends_on_code  TEXT NOT NULL,
    version_range    TEXT NOT NULL,
    PRIMARY KEY (package_id, depends_on_code)
);

ALTER TABLE entity ADD COLUMN package_id UUID REFERENCES package (id);
ALTER TABLE property_definition ADD COLUMN package_id UUID REFERENCES package (id);
ALTER TABLE statement ADD COLUMN package_id UUID REFERENCES package (id);

CREATE INDEX entity_package_idx ON entity (package_id);
CREATE INDEX property_package_idx ON property_definition (package_id);
CREATE INDEX statement_package_idx ON statement (package_id);

CREATE TABLE release (
    id           UUID PRIMARY KEY,
    package_id   UUID NOT NULL REFERENCES package (id) ON DELETE CASCADE,
    version      TEXT NOT NULL,
    published_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    manifest     JSONB NOT NULL DEFAULT '{}',
    UNIQUE (package_id, version)
);

CREATE TABLE release_dependency (
    release_id            UUID NOT NULL REFERENCES release (id) ON DELETE CASCADE,
    dependency_code       TEXT NOT NULL,
    dependency_version    TEXT NOT NULL,
    PRIMARY KEY (release_id, dependency_code)
);

CREATE TABLE release_object (
    release_id       UUID NOT NULL REFERENCES release (id) ON DELETE CASCADE,
    object_type      TEXT NOT NULL,
    object_public_id TEXT NOT NULL,
    revision_no      INT NOT NULL CHECK (revision_no > 0),
    PRIMARY KEY (release_id, object_type, object_public_id)
);

CREATE INDEX release_object_release_idx ON release_object (release_id);

-- +goose Down
DROP TABLE IF EXISTS release_object;
DROP TABLE IF EXISTS release_dependency;
DROP TABLE IF EXISTS release;
ALTER TABLE statement DROP COLUMN IF EXISTS package_id;
ALTER TABLE property_definition DROP COLUMN IF EXISTS package_id;
ALTER TABLE entity DROP COLUMN IF EXISTS package_id;
DROP TABLE IF EXISTS package_dependency;
DROP TABLE IF EXISTS package;
