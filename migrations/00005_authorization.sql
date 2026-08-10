-- +goose Up
CREATE TABLE auth_policy (
    id         UUID PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE,
    priority   INT NOT NULL DEFAULT 100,
    document   JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX auth_policy_priority_idx ON auth_policy (priority DESC);

-- Bootstrap: admin role has full manage
INSERT INTO auth_policy (id, name, priority, document) VALUES (
    '00000000-0000-4000-8000-000000000001',
    'bootstrap-admin',
    10000,
    '{"effect":"allow","operations":["manage"],"roles":["admin"]}'::jsonb
);

-- +goose Down
DROP TABLE IF EXISTS auth_policy;
