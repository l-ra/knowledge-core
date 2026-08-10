# Knowledge Core — dokumentace

Tato složka je zdroj pravdy pro vývoj: koncepty, uzavřená implementační rozhodnutí a zadání.

| Složka | Účel |
|--------|------|
| [design/](design/) | Architektonický návrh v1 (vstupní specifikace) |
| [concepts/](concepts/) | Stabilní vysvětlení klíčových konceptů |
| [decisions/](decisions/) | ADR — konkrétní rozhodnutí (stack, §38, schema) |
| [specs/](specs/) | Zadání pro vývoj po fázích (API, schema, acceptance) |

## Pořadí čtení

1. [design/knowledge_core_v1_technicky_navrh.md](design/knowledge_core_v1_technicky_navrh.md) — cíl a invarianty
2. [decisions/0001-stack-and-open-questions.md](decisions/0001-stack-and-open-questions.md) — uzavřené volby
3. [concepts/overview.md](concepts/overview.md) — model v kostce
4. [specs/phase-01-canonical-graph.md](specs/phase-01-canonical-graph.md) — aktuální vývojové zadání

## Stav implementace

| Fáze | Obsah | Status |
|------|-------|--------|
| 0 | Skeleton (Go, PG, compose, health) | done |
| 1 | Canonical graph | done (A1/A2/A14) |
| 2 | History / ChangeSet | done (A7/A8/A9) |
| 3 | Provenance | done (A10) |
| 4 | Packages / releases | done (A11/A12/A13 + import) |
| 5 | Authorization | done (A4/A5/A6) |
| 6 | Lenses / GraphQL | done (A3) |
| 7 | Outbox / projections | done (A15) |
