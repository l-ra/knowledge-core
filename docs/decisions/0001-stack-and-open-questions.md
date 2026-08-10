# ADR 0001 — Stack a uzavření §38

**Status:** Accepted  
**Datum:** 2026-08-10  
**Kontext:** [technický návrh §38–39](../design/knowledge_core_v1_technicky_navrh.md)

## Rozhodnutí — stack

| Volba | Hodnota |
|-------|---------|
| Jazyk | Go 1.24+ (runtime 1.25 OK) |
| Modul | `github.com/l-ra/knowledge-core` |
| HTTP | `chi` + `net/http` |
| DB | PostgreSQL 16 |
| Driver / SQL | `pgx/v5` (ruční; [ADR 0004](0004-manual-sql.md)) |
| Migrace | `goose` |
| Auth | OIDC (`coreos/go-oidc`); dev headers; bootstrap password (Helm) |
| GraphQL (fáze 6) | Lightweight adapter nad Lens engine ([ADR 0003](0003-graphql-lightweight.md)) |
| Logging | `log/slog` |
| Deploy | Docker Compose + Helm chart (`deploy/helm/knowledge-core`) |

## Rozhodnutí — otevřené body §38

### 38.1 Typed values — hybrid

- Discriminátor `value_type`
- Typed sloupce pro skaláry: `bool`, `int64`, `numeric`, `date`, `timestamptz`, `text`, `entity_id` (UUID FK)
- Constrained JSONB pro: `LocalizedString`, `ExternalIdentifier`, `Quantity`, `Interval`
- CHECK + Go validace; **zakázán** nevalidovaný holý `value JSONB`

### 38.2 Quantity units

- Built-in unit registry v system package
- Unit = odkaz na Entity (UUID)
- **Bez** automatických conversions v1

### 38.3 Interval

- Open-ended na jedné straně povolen
- Empty / `from > to` → hard reject
- Quantity interval: obě meze stejná unit (nebo open)

### 38.4 Reference

- First-class objekt s UUID a veřejným `R<n>`
- Sdílitelný mezi Statements

### 38.5 Statement replacement

Explicitní API operace:

- `create` — nové Statement ID
- `revise` — nová revision téhož Statement ID
- `deprecate` / `delete` — změna statusu

Žádné tiché „replace by matching value“.

### 38.6 Policy language

Deklarativní YAML/JSON DSL: subject attributes, resource selectors, operations, `allow`/`deny`. Deterministické, bez scriptů.

### 38.7 Search ACL leakage

ACL filtr **před** score / count / facets. Bez `discover` konzistentní non-existence (404 / prázdný výsledek).

### 38.8 Lens DSL v1

Schema dle návrhu §21.2: selector, key, fields, cardinality, nested lens, write capabilities.

### 38.9 Package dependencies

SemVer ranges: exact, `^`, `~`.

### 38.10 Release granularity

Bundle: `manifest.yaml` + object-revision index + JSONL snapshots. Immutable.

## NFR v1

Single application process, single PostgreSQL cluster, no sharding, no distributed transactions.

## Důsledky

- Fáze implementace kopírují §39 návrhu.
- Každá fáze má zadání ve [specs/](../specs/).
- Změna těchto defaultů vyžaduje nové ADR.
