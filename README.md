# Knowledge Core

All-in-one knowledge graph core (Wikibase-inspired) — Go + PostgreSQL.

Repository: [github.com/l-ra/knowledge-core](https://github.com/l-ra/knowledge-core)

## Dokumentace

Viz [docs/README.md](docs/README.md): koncepty, ADR, zadání fází. **Aktuální plán:** [docs/ROADMAP.md](docs/ROADMAP.md).

## Rychlý start

```bash
# PostgreSQL + Pocket ID (OIDC)
docker compose -f deploy/docker-compose.yml up -d postgres pocket-id
# or: podman-compose -f deploy/docker-compose.yml up -d postgres pocket-id
# Compose uses subnet 172.25.90.0/24 so Podman works on hosts with a 10.0.0.0/8 route.

# Pocket ID setup (jednou): otevři http://pocket-id.localhost:1411/setup
# Vytvoř public PKCE klienta id=knowledge-core, redirect=http://localhost:8080/ui/callback

# App (lokálně proti compose DB + IdP)
export KC_DATABASE_URL='postgres://kc:kc@localhost:5433/knowledge_core?sslmode=disable'
export KC_AUTH_MODE=oidc
export KC_OIDC_ISSUER='http://pocket-id.localhost:1411'
export KC_OIDC_AUDIENCE=knowledge-core
go run ./cmd/knowledge-core
# (nebo bez OIDC: KC_AUTH_MODE=bootstrap / dev)

# nebo celý stack (app v kontejneru, OIDC)
docker compose -f deploy/docker-compose.yml up --build
```

| Služba | URL |
|--------|-----|
| App / UI | http://localhost:8080/ui/ |
| Pocket ID | http://pocket-id.localhost:1411 |
| Postgres | localhost:5433 |

Health: `GET http://localhost:8080/healthz`  
Metrics: `GET http://localhost:8080/metrics`  
UI: `http://localhost:8080/ui/`

## Web UI

Vestavěné SPA (React) na `/ui`:

- OIDC (PKCE), bootstrap heslo, nebo dev headers
- i18n cs/en; light/dark dle `prefers-color-scheme`
- **Data:** search, entity editor (statements + advanced qualifiers/refs/valid time)
- **Model:** properties, packages, lenses, policies
- **Admin:** outbox / projection rebuild

```bash
make ui-build   # npm build → web/ui/dist (embed)
make run
```


## API (Fáze 1–7)

All `/v1/*` endpoints require authentication.

**Bootstrap auth** (`KC_AUTH_MODE=bootstrap`, default in Helm):

```bash
# Password is generated on first start (logged once) or set via KC_BOOTSTRAP_ADMIN_PASSWORD
curl -H 'Authorization: Bearer <password>' ...
# or
curl -H 'X-Admin-Password: <password>' ...
```

Reset inside container: `knowledge-core admin reset-password`

**Dev auth** (`KC_AUTH_MODE=dev`):

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

**Auth policies (fáze 9):**

- `GET /v1/policies` — list
- `POST /v1/policies` — upsert `{ "name", "priority", "document" }`
- `GET/PUT/DELETE /v1/policies/{name}`

OpenAPI: [api/openapi.yaml](api/openapi.yaml)

**Lens / domain API (fáze 6):**

- `POST /v1/lenses` — register lens definition (JSON document)
- `GET /v1/lenses/{code}`
- `GET /v1/lenses/{code}/instances/{key}` — domain read bez Q/P
- `POST /v1/lenses/{code}/instances/{key}/patch` — `{ "operations": [{ "op": "set", "field": "name", "value": … }] }`
- `POST /v1/graphql` — GraphQL adapter (`application(code: "…")`, generic `domain(lens, key)`)

**Projections (fáze 7 + 11):**

- `POST /v1/projections/outbox/process` — zpracovat pending outbox události
- `POST /v1/projections/search/rebuild` — full rebuild search projekce z canonical
- `GET /v1/projections/search?q=…` — full-text vyhledávání (ACL-aware)
- `POST /v1/projections/rdf/rebuild` — full rebuild RDF projekce
- `GET /v1/projections/rdf` — export N-Triples

Outbox worker (CLI / CronJob): `knowledge-core outbox process`

## Env

| Proměnná | Default |
|----------|---------|
| `KC_HTTP_ADDR` | `:8080` |
| `KC_DATABASE_URL` | `postgres://kc:kc@localhost:5433/knowledge_core?sslmode=disable` |
| `KC_LOG_LEVEL` | `info` |
| `KC_AUTH_MODE` | `dev` (`dev` \| `oidc` \| `bootstrap`) |
| `KC_BOOTSTRAP_ADMIN_SUBJECT` | `admin` |
| `KC_BOOTSTRAP_PASSWORD_FILE` | `/data/admin.password` |
| `KC_BOOTSTRAP_ADMIN_PASSWORD` | — (optional fixed password) |
| `KC_OIDC_ISSUER` | — (required for `oidc` mode) |
| `KC_OIDC_AUDIENCE` | — (optional JWT audience) |

## Kubernetes (Helm)

Chart v `deploy/helm/knowledge-core` — subcharts **postgresql** (default) a volitelný **pocket-id** (OIDC).

```bash
helm upgrade --install kc deploy/helm/knowledge-core \
  --set image.repository=ghcr.io/l-ra/knowledge-core \
  --set image.tag=latest
```

Detail: [deploy/helm/README.md](deploy/helm/README.md) · Runbook: [deploy/RUNBOOK.md](deploy/RUNBOOK.md)

CI (`.github/workflows/ci.yml`) buildí image `ghcr.io/l-ra/knowledge-core` a publikuje chart do `oci://ghcr.io/l-ra`.
Tag `v*` spustí [release workflow](.github/workflows/release.yml) (semver image + chart).

## Testy

```bash
go test ./...
```
