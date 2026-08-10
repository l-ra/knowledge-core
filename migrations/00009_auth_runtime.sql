-- +goose Up
CREATE TABLE auth_runtime (
    id              INT PRIMARY KEY CHECK (id = 1),
    auth_mode       TEXT NOT NULL,
    oidc_issuer     TEXT NOT NULL DEFAULT '',
    oidc_client_id  TEXT NOT NULL DEFAULT '',
    oidc_audience   TEXT NOT NULL DEFAULT '',
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by      TEXT NOT NULL DEFAULT ''
);

-- +goose Down
DROP TABLE IF EXISTS auth_runtime;
