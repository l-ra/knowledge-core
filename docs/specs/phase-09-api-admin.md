# Zadání — Fáze 9: API a správa

**Status:** done

## Cíl

REST CRUD pro auth policies, OpenAPI popis, konzistentní error envelope.

## Scope

**In:**

- `GET/POST /v1/policies`, `GET/PUT/DELETE /v1/policies/{name}`
- OpenAPI 3: `api/openapi.yaml`
- Error envelope `{ "error": { "code", "message" } }`
- Acceptance: správa policy bez přímého SQL

## Acceptance

| ID | Scénář | Status |
|----|--------|--------|
| PolicyAPI | Upsert/list/delete policy přes REST, reload auth engine | done |
