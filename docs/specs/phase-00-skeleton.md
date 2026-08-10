# Zadání — Fáze 0: Skeleton

## Cíl

Spustitelná Go služba s PostgreSQL, migracemi a health checkem. Žádná doménová logika mimo bootstrap.

## Deliverables

- [x] Go module `github.com/rasekl/knowledge-core`
- [x] `cmd/knowledge-core` — HTTP server (chi)
- [x] `internal/config` — env konfigurace (`KC_*`)
- [x] goose migrace (`migrations/00001_canonical_graph.sql`)
- [x] `deploy/docker-compose.yml` — `postgres` + `app`
- [x] `GET /healthz` → `200` + DB ping
- [x] root `README.md` — jak spustit lokálně
- [x] `go test ./...` prochází

## Konfigurace (env)

| Proměnná | Default | Popis |
|----------|---------|-------|
| `KC_HTTP_ADDR` | `:8080` | Listen address |
| `KC_DATABASE_URL` | `postgres://kc:kc@localhost:5433/knowledge_core?sslmode=disable` | PG DSN (host port 5433) |
| `KC_LOG_LEVEL` | `info` | slog level |

## Acceptance

1. `docker compose -f deploy/docker-compose.yml up` zvedne PG + app.
2. `curl localhost:8080/healthz` vrátí OK když DB běží.
3. Migrace se aplikují při startu app (nebo explicitním migrate job).
