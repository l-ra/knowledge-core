# Roadmapa Knowledge Core

**Aktualizace:** 2026-08-10  
**Repozitář:** [github.com/l-ra/knowledge-core](https://github.com/l-ra/knowledge-core)

Tento dokument nahrazuje „aktuální vývojové zadání“ v [docs/README.md](README.md) a doplňuje [specs/](specs/) po dokončení v1 jádra (fáze 0–7).

---

## Shrnutí

| Oblast | Stav |
|--------|------|
| **v1 jádro** (fáze 0–7) | **Hotovo** — všechny plánované acceptance testy procházejí |
| **Provoz / distribuce** (fáze 8) | **Hotovo** — Helm, CI, release, runbook, OIDC Job |
| **Dokončení mezer v1** (fáze 9–11) | **Hotovo** |
| **Refaktoring / kvalita** (fáze 12) | **Hotovo** |
| **Web UI** (fáze 13) | **Hotovo** (MVP) |
| **Schema validation** (fáze 14) | **Hotovo** |
| **Entity + Profile** (fáze 15) | **Hotovo** — P*/C* jako entity s profilem |

---

## Fáze 0–7 — v1 jádro (hotovo)

| Fáze | Spec | Implementace | Acceptance |
|------|------|--------------|------------|
| 0 | [phase-00-skeleton.md](specs/phase-00-skeleton.md) | Go, PG, goose, compose, `/healthz` | — |
| 1 | [phase-01-canonical-graph.md](specs/phase-01-canonical-graph.md) | Entity, Property, Statement, datatypes, current state | A1, A2, A14 |
| 2 | [phase-02-history.md](specs/phase-02-history.md) | Revisions, ChangeSet, optimistic lock, idempotency | A7, A8, A9 |
| 3 | [phase-03-provenance.md](specs/phase-03-provenance.md) | Qualifiers, References, valid time | A10 |
| 4 | [phase-04-packages.md](specs/phase-04-packages.md) | Packages, SemVer, releases, bundle export/import | A11, A12, A13 + import |
| 5 | [phase-05-authorization.md](specs/phase-05-authorization.md) | RBAC/ABAC, dev/OIDC/bootstrap auth, read filter | A4, A5, A6 |
| 6 | [phase-06-lenses.md](specs/phase-06-lenses.md) | Lens DSL, REST read/patch, GraphQL adapter | A3 |
| 7 | [phase-07-projections.md](specs/phase-07-projections.md) | Outbox, search projection, rebuild | A15 |

**Migrace:** `00001` … `00007`  
**Testy:** `go test ./...` (acceptance vyžaduje běžící Postgres)

### Co v1 jádro umí

- Canonical graph s hybridními typed values a synchronním `statement_current`
- Immutable historie a atomické ChangeSety
- Provenance (qualifiers, references, valid time)
- Model lifecycle (packages, immutable releases, bundle promotion)
- Centrální authorization (default deny, bootstrap admin)
- Deklarativní lenses + domain-oriented REST/GraphQL
- Transactional outbox + search projection s full rebuild z canonical

---

## Fáze 8 — Provoz a distribuce (hotovo)

| Položka | Stav | Poznámka |
|---------|------|----------|
| Helm chart `deploy/helm/knowledge-core` | done | Hlavní chart |
| Subchart PostgreSQL | done | PG 16, generované heslo |
| Subchart Pocket ID (volitelný OIDC) | done | `pocketId.enabled=true` |
| GitHub Actions CI | done | test, build, `helm lint`, publish image + chart |
| Bootstrap admin heslo (`KC_AUTH_MODE=bootstrap`) | done | log + reset v kontejneru |
| Modul `github.com/l-ra/knowledge-core` | done | |
| Auto-registrace OIDC klienta (KC ↔ Pocket ID) | done | post-install Job + `pocketId.adminApiKey` |
| Production hardening (PDB, resources, network policies) | done | PDB default on; NetworkPolicy opt-in |
| Verzované release (git tag → image + chart semver) | done | `.github/workflows/release.yml` |
| Dokumentace provozu (runbook, backup PG) | done | `deploy/RUNBOOK.md` |

Detail instalace: [deploy/helm/README.md](../deploy/helm/README.md) · Runbook: [deploy/RUNBOOK.md](../deploy/RUNBOOK.md)

---

## Mezery oproti návrhu a ADR (v rámci „hotových“ fází)

Tyto body jsou **úmyslně odložené** nebo **zjednodušené** oproti [technickému návrhu §39](design/knowledge_core_v1_technicky_navrh.md) a [ADR 0001](decisions/0001-stack-and-open-questions.md):

| Oblast | Návrh / ADR | Skutečný stav | Doporučená fáze |
|--------|-------------|---------------|-----------------|
| SQL vrstva | `pgx` + **sqlc** | **done** — ruční SQL (ADR 0004) | 12 |
| GraphQL | **gqlgen** nad lens engine | **done** — lightweight adapter (ADR 0003) | 10 |
| RDF export | volitelná projekce (§39 fáze 7) | **done** (fáze 11) | 11 |
| Search ACL | filtr **před** score (ADR 38.7) | **done** (fáze 11) | 11 |
| Lens nested | pole `lens` v DSL | **done** (fáze 10) | 10 |
| Lens patch | `add` / `remove` (concepts) | **done** (fáze 10) | 10 |
| Policy API | deklarativní správa | **done** — REST CRUD `/v1/policies` | 9 |
| Outbox worker | integrace | **done** — CLI + Helm CronJob | 11 |
| Deploy (ADR 0001) | Docker Compose | Compose + **Helm** | 8 |

---

## Fáze 9 — API a správa (hotovo)

**Cíl:** Dopsat provozní a administrátorské mezery mimo core graph.

- [x] REST CRUD pro `auth_policy` (`GET/POST/DELETE /v1/policies`)
- [x] OpenAPI 3 popis veřejného API
- [x] Konzistentní error envelope a request validation
- [x] Acceptance: správa policy bez přímého SQL

**Priorita:** střední — bootstrap admin stačí pro early adopters

---

## Fáze 10 — Lens a GraphQL (hotovo)

**Cíl:** Srovnat implementaci s Lens DSL v1 z návrhu §21.

- [x] Nested lens (read)
- [x] Patch operace `add` / `remove` pro `many` cardinality
- [x] Explicitní ADR 0003: lightweight GraphQL adapter (ne gqlgen)
- [x] Rozšířené acceptance pro nested a many-field writes

**Priorita:** střední — základní lens scénáře (A3) fungují

---

## Fáze 11 — Projections v2 (hotovo)

**Cíl:** Dokončit §39 fázi 7 a ADR 38.7.

- [x] RDF export projection (+ rebuild endpoint)
- [x] ACL-aware search (discover/read filter na hit úrovni)
- [x] Background outbox processor (CLI + Helm CronJob)
- [x] Acceptance: search neprozrazuje entity bez `discover`

**Priorita:** vysoká pro multi-tenant / produkční nasazení s jemným ACL

---

## Fáze 12 — Kvalita a refaktoring (hotovo)

- [x] ADR 0004: ponechat ruční SQL (`pgx`), ne sqlc
- [x] Integrační testy Helm (kind job v CI)
- [x] Concurrency test ChangeSet / optimistic lock
- [x] Observability: Prometheus `/metrics`, `X-Request-Id` / `X-Correlation-Id`
- [x] Aktualizace ADR 0001 (deploy, GraphQL, SQL)

---

## Fáze 13 — Web UI (hotovo MVP)

- [x] Vestavěné SPA `/ui` (React + Vite, go:embed)
- [x] OIDC PKCE + bootstrap/dev login
- [x] i18n cs/en, light/dark dle systému
- [x] Oddělená navigace Data / Model / Admin
- [x] Entity editor s advanced (qualifiers, references, valid time)
- [x] List API entities/properties/packages/lenses

Detail: [specs/phase-13-web-ui.md](specs/phase-13-web-ui.md)

---

## Fáze 14 — Schema validation (hotovo)

- [x] Property constraints, shape profiles, validation modes
- Detail: [specs/phase-14-schema-validation.md](specs/phase-14-schema-validation.md)

---

## Fáze 15 — Entity + Profile (hotovo)

- [x] Properties/classes = entity + `property_profile` / `class_profile` (public_id `P*`/`C*`)
- [x] Statementy o schema entitách; predicate jen s property_profile
- [x] RDF emit `rdf:Property` / `rdfs:Class` z profile
- Detail: [specs/phase-15-entity-profile.md](specs/phase-15-entity-profile.md), [ADR 0005](decisions/0005-entity-profile-schema.md)

**Poznámka:** greenfield rewrite migrací — lokální DB wipe (`compose down -v`) před upgrade.

---

## Doporučené pořadí další práce

```text
UI v1.1: lens domain editor, OIDC groups→roles, bundle wizard
```

---

## Acceptance matice (kompletní)

| ID | Scénář | Test | Stav |
|----|--------|------|------|
| A1 | Create entity/property/statement | `TestAcceptanceA1A2A14` | done |
| A2 | Datatype validation | `TestAcceptanceA1A2A14` | done |
| A3 | Domain lens bez Q/P | `TestAcceptanceA3` | done |
| A4 | Partial update nesmaže skrytá data | `TestAcceptanceA4A5A6` | done |
| A5 | Property-level read filter | `TestAcceptanceA4A5A6` | done |
| A6 | Bez discover → 404 | `TestAcceptanceA4A5A6` | done |
| A7 | Optimistic lock | `TestAcceptanceA7A8A9` | done |
| A8 | Idempotency | `TestAcceptanceA7A8A9` | done |
| A9 | ChangeSet batch | `TestAcceptanceA7A8A9` | done |
| A10 | Provenance | `TestAcceptanceA10` | done |
| A11 | Package lifecycle | `TestAcceptanceA11A12A13` | done |
| A12 | Release immutability | `TestAcceptanceA11A12A13` | done |
| A13 | Bundle export | `TestAcceptanceA11A12A13` | done |
| A14 | Label.en required | `TestAcceptanceA1A2A14` | done |
| A15 | Projection rebuild | `TestAcceptanceA15` | done |
| — | Import promotion | `TestAcceptanceImportPromotion` | done |
| — | Schema validation relaxed | `TestAcceptanceSchemaValidationRelaxed` | done |
| — | Entity+Profile | `TestAcceptanceEntityProfile` | done |

---

## Odkazy

- [Technický návrh v1](design/knowledge_core_v1_technicky_navrh.md) — normativní záměr
- [ADR 0001 — stack](decisions/0001-stack-and-open-questions.md) — uzavřené volby (částečně zastaralé u deploy/graphql/sqlc)
- [Koncepty](concepts/overview.md) — stabilní vysvětlení modelu
- [Specifikace fází](specs/README.md) — detailní zadání 0–7
