-- +goose Up
CREATE INDEX IF NOT EXISTS statement_current_value_entity_idx
    ON statement_current (value_entity_id)
    WHERE value_entity_id IS NOT NULL;

ALTER TABLE shape_profile
    ADD COLUMN IF NOT EXISTS package_id UUID REFERENCES package (id);

UPDATE shape_profile sp
SET package_id = e.package_id
FROM entity e
WHERE e.id = sp.class_id
  AND sp.package_id IS NULL;

CREATE INDEX IF NOT EXISTS shape_profile_package_idx ON shape_profile (package_id);

-- +goose Down
DROP INDEX IF EXISTS shape_profile_package_idx;
ALTER TABLE shape_profile DROP COLUMN IF EXISTS package_id;
DROP INDEX IF EXISTS statement_current_value_entity_idx;
