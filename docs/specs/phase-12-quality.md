# Zadání — Fáze 12: Kvalita

**Status:** done

## Cíl

Observability, concurrency coverage, Helm kind smoke, uzavření ADR pro SQL/GraphQL realitu.

## Scope

**In:**

- ADR 0004 (manual SQL)
- Prometheus `/metrics`
- Request/correlation ID response headers
- `TestConcurrencyOptimisticLock`
- CI `helm-kind` job

## Acceptance

| ID | Scénář | Status |
|----|--------|--------|
| Concurrency | Exactly one revise wins under race | done |
