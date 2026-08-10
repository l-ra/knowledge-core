# Knowledge Core

All-in-one knowledge graph core (Wikibase-inspired) — Go + PostgreSQL.

## Dokumentace

Viz [docs/README.md](docs/README.md): koncepty, ADR, zadání fází.

## Rychlý start

```bash
# PostgreSQL
docker compose -f deploy/docker-compose.yml up -d postgres

# App (lokálně)
export KC_DATABASE_URL='postgres://kc:kc@localhost:5433/knowledge_core?sslmode=disable'
go run ./cmd/knowledge-core

# nebo celý stack
docker compose -f deploy/docker-compose.yml up --build
```

Health: `GET http://localhost:8080/healthz`

## API (Fáze 1–7)

All `/v1/*` endpoints require authentication.

**Dev auth** (`KC_AUTH_MODE=dev`, default):

```bash
curl -H 'X-Subject: alice' -H 'X-Roles: editor,viewer' ...
```

**OIDC** (`KC_AUTH_MODE=oidc`): `Authorization: Bearer <jwt>` (requires `KC_OIDC_ISSUER`).

Writes accept headers: `Idempotency-Key`, `X-Actor`, `X-Correlation-Id`.

Create responses: `{ "data": {…}, "changeSet": { "id": "C1", … } }`

Graph writes accept optional `packageCode` for ownership.

- `POST /v1/entities` — `{ "packageCode": "…", "labels": { "en": "…" } }`
- `PATCH /v1/entities/{Qid}` — `{ "labels": {…}, "expectedRevision": 1 }`
- `GET /v1/entities/{Qid}/history`
- `POST /v1/properties` — `{ "packageCode": "…", "datatype": "String", "labels": { "en": "…" } }`
- `POST /v1/references` — `{ "fields": { "sourceUrl": "…", … } }`
- `GET /v1/references/{Rid}`
- `POST /v1/statements` — supports `packageCode`, `qualifiers`, `referenceIds`, `validFrom`, `validTo`
- `POST /v1/statements/{Sid}/revise` — optional `value`, `qualifiers`, `referenceIds`, valid time
- `GET /v1/statements/{Sid}/history`
- `POST /v1/changesets` — batch `{ "operations": […] }`
- `GET /v1/changesets/{Cid}`
- `POST /v1/packages` — `{ "code": "…", "lifecycle": "released", "labels": { "en": "…" }, "dependencies": […] }`
- `GET /v1/packages/{code}`
- `POST /v1/packages/{code}/releases` — `{ "version": "1.0.0" }`
- `GET /v1/packages/{code}/releases/{version}`
- `GET /v1/packages/{code}/releases/{version}/bundle`
- `POST /v1/releases/import` — import exported bundle (promotion)
- `POST /v1/packages/{code}/releases/{version}/mutate` — rejects immutable release (409)

**Lens / domain API (fáze 6):**

- `POST /v1/lenses` — register lens definition (JSON document)
- `GET /v1/lenses/{code}`
- `GET /v1/lenses/{code}/instances/{key}` — domain read bez Q/P
- `POST /v1/lenses/{code}/instances/{key}/patch` — `{ "operations": [{ "op": "set", "field": "name", "value": … }] }`
- `POST /v1/graphql` — GraphQL adapter (`application(code: "…")`, generic `domain(lens, key)`)

**Projections (fáze 7):**

- `POST /v1/projections/outbox/process` — zpracovat pending outbox události
- `POST /v1/projections/search/rebuild` — full rebuild search projekce z canonical
- `GET /v1/projections/search?q=…` — full-text vyhledávání v projekci

## Env

| Proměnná | Default |
|----------|---------|
| `KC_HTTP_ADDR` | `:8080` |
| `KC_DATABASE_URL` | `postgres://kc:kc@localhost:5433/knowledge_core?sslmode=disable` |
| `KC_LOG_LEVEL` | `info` |
| `KC_AUTH_MODE` | `dev` (`dev` \| `oidc`) |
| `KC_BOOTSTRAP_ADMIN_SUBJECT` | `admin` |
| `KC_OIDC_ISSUER` | — (required for `oidc` mode) |
| `KC_OIDC_AUDIENCE` | — (optional JWT audience) |

## Testy

```bash
go test ./...
```
