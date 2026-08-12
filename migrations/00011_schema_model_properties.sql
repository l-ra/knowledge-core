-- +goose Up
ALTER TABLE model_schema_config
    ADD COLUMN IF NOT EXISTS model_properties JSONB NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE model_schema_config DROP COLUMN IF EXISTS model_properties;
