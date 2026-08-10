# Knowledge Core — dokumentace

Tato složka je zdroj pravdy pro vývoj: koncepty, uzavřená implementační rozhodnutí a zadání.

| Složka | Účel |
|--------|------|
| [ROADMAP.md](ROADMAP.md) | **Aktuální plán** — stav fází 0–8+, mezery, další kroky |
| [ops/](ops/) | Provozní návody (post-install, oprávnění) |
| [design/](design/) | Architektonický návrh v1 (vstupní specifikace) |
| [concepts/](concepts/) | Stabilní vysvětlení klíčových konceptů |
| [decisions/](decisions/) | ADR — konkrétní rozhodnutí (stack, §38, schema) |
| [specs/](specs/) | Zadání pro vývoj po fázích (API, schema, acceptance) |

## Pořadí čtení

1. [ROADMAP.md](ROADMAP.md) — **co je hotové a co dál**
2. [ops/post-install.md](ops/post-install.md) — **po instalaci: admin, OIDC, proč forbidden, první data**
3. [design/knowledge_core_v1_technicky_navrh.md](design/knowledge_core_v1_technicky_navrh.md) — cíl a invarianty
4. [decisions/0001-stack-and-open-questions.md](decisions/0001-stack-and-open-questions.md) — uzavřené volby
5. [concepts/overview.md](concepts/overview.md) — model v kostce
6. [specs/](specs/) — detailní zadání jednotlivých fází

## Stav implementace

### v1 jádro — hotovo

| Fáze | Obsah | Status |
|------|-------|--------|
| 0 | Skeleton (Go, PG, compose, health) | done |
| 1 | Canonical graph | done (A1/A2/A14) |
| 2 | History / ChangeSet | done (A7/A8/A9) |
| 3 | Provenance | done (A10) |
| 4 | Packages / releases | done (A11/A12/A13 + import) |
| 5 | Authorization | done (A4/A5/A6) + bootstrap auth |
| 6 | Lenses / GraphQL | done (A3) |
| 7 | Outbox / projections | done (A15) |

### Provoz — hotovo

| Fáze | Obsah | Status |
|------|-------|--------|
| 8 | Helm, CI, release, Pocket ID OIDC Job, runbook | done |

Detailní plán dalších fází (9–12): [ROADMAP.md](ROADMAP.md) — **fáze 8–12 hotové**.

Fáze 8–12: provoz, policy API, nested lenses, ACL/RDF/outbox, kvalita (viz ROADMAP).
