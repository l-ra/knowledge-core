-- +goose Up
ALTER TABLE changeset_entity_overlay
    ADD COLUMN IF NOT EXISTS iri_aliases_json JSONB;

-- +goose Down
ALTER TABLE changeset_entity_overlay
    DROP COLUMN IF EXISTS iri_aliases_json;
