-- +goose Up
CREATE TABLE projection_rdf (
    id           UUID PRIMARY KEY,
    object_type  TEXT NOT NULL,
    public_id    TEXT NOT NULL,
    subject      TEXT NOT NULL,
    predicate    TEXT NOT NULL,
    object       TEXT NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (object_type, public_id, predicate, object)
);

CREATE INDEX projection_rdf_public_id_idx ON projection_rdf (object_type, public_id);

-- +goose Down
DROP TABLE IF EXISTS projection_rdf;
