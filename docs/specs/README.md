# Roadmapa zadání

**v1 jádro (fáze 0–7) je hotové.** Aktuální plán včetně fáze 8+ a mezer: [../ROADMAP.md](../ROADMAP.md).

## Fáze 0–7 (hotovo)

| Fáze | Spec | Scope | Acceptance |
|------|------|-------|------------|
| 0 | [phase-00-skeleton.md](phase-00-skeleton.md) | Go, PG, compose, health | — |
| 1 | [phase-01-canonical-graph.md](phase-01-canonical-graph.md) | Entity, Property, Statement, CRUD | A1, A2, A14 — **done** |
| 2 | [phase-02-history.md](phase-02-history.md) | Revisions, ChangeSet, optimistic lock, idempotency | A7, A8, A9 — **done** |
| 3 | [phase-03-provenance.md](phase-03-provenance.md) | Qualifiers, References, valid time | A10 — **done** |
| 4 | [phase-04-packages.md](phase-04-packages.md) | Packages, SemVer, releases, bundles, promotion | A11, A12, A13 — **done** |
| 5 | [phase-05-authorization.md](phase-05-authorization.md) | OIDC, RBAC/ABAC, ACL filter, bootstrap auth | A4, A5, A6 — **done** |
| 6 | [phase-06-lenses.md](phase-06-lenses.md) | Lens DSL, patch writes, GraphQL adapter | A3 — **done** |
| 7 | [phase-07-projections.md](phase-07-projections.md) | Outbox, search rebuild | A15 — **done** |

## Fáze 8+ (plánováno)

| Fáze | Scope | Stav |
|------|-------|------|
| 8 | Helm, CI, PostgreSQL + Pocket ID subcharts, bootstrap admin | **done** |
| 9 | Policy REST API, OpenAPI | **done** |
| 10 | Nested lens, gqlgen, patch add/remove | **done** (ADR 0003: lightweight GQL) |
| 11 | RDF projection, ACL-aware search, outbox worker | **done** |
| 12 | sqlc, observability, Helm integrační testy | **done** (ADR 0004: manual SQL) |
| 13 | Built-in web UI `/ui` | **done** (MVP) |

Rozhodnutí zůstávají v [decisions/](../decisions/).
