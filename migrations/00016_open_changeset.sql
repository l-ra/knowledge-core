-- +goose Up
ALTER TABLE change_set
    ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'committed'
        CHECK (status IN ('open', 'committed', 'cancelled')),
    ADD COLUMN IF NOT EXISTS opened_at TIMESTAMPTZ,
    ALTER COLUMN committed_at DROP NOT NULL;

UPDATE change_set SET status = 'committed' WHERE status IS NULL OR status = '';
UPDATE change_set SET opened_at = committed_at WHERE opened_at IS NULL AND status = 'committed';

CREATE INDEX IF NOT EXISTS change_set_status_idx ON change_set (status);
CREATE INDEX IF NOT EXISTS change_set_status_actor_idx ON change_set (status, actor);

CREATE TABLE changeset_object_claim (
    id                UUID PRIMARY KEY,
    changeset_id      UUID NOT NULL REFERENCES change_set (id) ON DELETE CASCADE,
    object_type       TEXT NOT NULL CHECK (object_type IN ('entity', 'statement')),
    object_id         UUID NOT NULL,
    canonical_iri     TEXT NOT NULL DEFAULT '',
    base_revision_no  INT NOT NULL DEFAULT 0,
    op_kind           TEXT NOT NULL CHECK (op_kind IN ('create', 'update', 'deprecate', 'delete')),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (object_id)
);

CREATE UNIQUE INDEX changeset_object_claim_iri_uidx
    ON changeset_object_claim (canonical_iri)
    WHERE canonical_iri <> '';

CREATE INDEX changeset_object_claim_cs_idx ON changeset_object_claim (changeset_id);

CREATE TABLE changeset_entity_overlay (
    changeset_id   UUID NOT NULL REFERENCES change_set (id) ON DELETE CASCADE,
    object_id      UUID NOT NULL,
    public_id      TEXT NOT NULL,
    package_code   TEXT NOT NULL DEFAULT '',
    iri_local      TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL,
    labels         JSONB NOT NULL DEFAULT '{}',
    descriptions   JSONB NOT NULL DEFAULT '{}',
    kind           TEXT NOT NULL DEFAULT 'entity',
    datatype       TEXT,
    constraints    JSONB,
    subclass_of    TEXT,
    revision_no    INT NOT NULL DEFAULT 1,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (changeset_id, object_id)
);

CREATE INDEX changeset_entity_overlay_public_idx ON changeset_entity_overlay (public_id);

CREATE TABLE changeset_statement_overlay (
    changeset_id       UUID NOT NULL REFERENCES change_set (id) ON DELETE CASCADE,
    object_id          UUID NOT NULL,
    public_id          TEXT NOT NULL,
    package_code       TEXT NOT NULL DEFAULT '',
    subject_public_id  TEXT NOT NULL,
    property_public_id TEXT NOT NULL,
    status             TEXT NOT NULL,
    value_json         JSONB NOT NULL DEFAULT '{}',
    qualifiers_json    JSONB NOT NULL DEFAULT '[]',
    references_json    JSONB NOT NULL DEFAULT '[]',
    valid_from         TIMESTAMPTZ,
    valid_to           TIMESTAMPTZ,
    revision_no        INT NOT NULL DEFAULT 1,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (changeset_id, object_id)
);

CREATE INDEX changeset_statement_overlay_public_idx ON changeset_statement_overlay (public_id);
CREATE INDEX changeset_statement_overlay_subject_idx ON changeset_statement_overlay (subject_public_id);

DROP TABLE IF EXISTS user_changeset_draft;

-- +goose Down
CREATE TABLE IF NOT EXISTS user_changeset_draft (
    actor      TEXT PRIMARY KEY,
    document   JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

DROP TABLE IF EXISTS changeset_statement_overlay;
DROP TABLE IF EXISTS changeset_entity_overlay;
DROP TABLE IF EXISTS changeset_object_claim;

ALTER TABLE change_set DROP COLUMN IF EXISTS opened_at;
ALTER TABLE change_set DROP COLUMN IF EXISTS status;
UPDATE change_set SET committed_at = COALESCE(committed_at, now()) WHERE committed_at IS NULL;
ALTER TABLE change_set ALTER COLUMN committed_at SET NOT NULL;
