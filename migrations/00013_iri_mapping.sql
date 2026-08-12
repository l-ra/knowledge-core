-- +goose Up
ALTER TABLE package
    ADD COLUMN IF NOT EXISTS iri_base TEXT NOT NULL DEFAULT '';

ALTER TABLE entity
    ADD COLUMN IF NOT EXISTS iri_local TEXT NOT NULL DEFAULT '';

-- Unique package IRI base when set (empty means platform default namespace).
CREATE UNIQUE INDEX IF NOT EXISTS package_iri_base_uidx
    ON package (iri_base)
    WHERE iri_base <> '';

-- Unique relative IRI within a package (empty local allowed multiple times until set).
CREATE UNIQUE INDEX IF NOT EXISTS entity_package_iri_local_uidx
    ON entity (package_id, iri_local)
    WHERE package_id IS NOT NULL AND iri_local <> '';

CREATE TABLE entity_iri_alias (
    entity_id  UUID NOT NULL REFERENCES entity (id) ON DELETE CASCADE,
    iri        TEXT NOT NULL,
    kind       TEXT NOT NULL CHECK (kind IN ('sameAs', 'imported', 'canonical_export')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (entity_id, iri)
);

CREATE UNIQUE INDEX entity_iri_alias_iri_uidx ON entity_iri_alias (iri);

-- +goose Down
DROP TABLE IF EXISTS entity_iri_alias;
DROP INDEX IF EXISTS entity_package_iri_local_uidx;
DROP INDEX IF EXISTS package_iri_base_uidx;
ALTER TABLE entity DROP COLUMN IF EXISTS iri_local;
ALTER TABLE package DROP COLUMN IF EXISTS iri_base;
