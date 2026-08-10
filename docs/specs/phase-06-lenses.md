# Zadání — Fáze 6: Lenses / GraphQL

**Status:** hotovo (A3)

## Cíl

Deklarativní Lens mapující graph ↔ domain objekt. Klient pracuje s field names, ne Q/P IDs. GraphQL adapter nad lens engine.

## Scope

**In:**

- Tabulka `lens_definition` (JSON dokument)
- Lens DSL v1: selector, key, fields (property, type, cardinality)
- Read engine s authorization filtrem
- Patch write: `set`, `clear` (cardinality `one`)
- REST: `POST/GET /v1/lenses`, `GET /v1/lenses/{code}/instances/{key}`, `POST .../patch`
- GraphQL: `POST /v1/graphql` — typed query `application(code:)` + generic `domain(lens, key)` (lightweight adapter, ne gqlgen)
- Acceptance A3

**Out (odloženo — viz [ROADMAP](../ROADMAP.md) fáze 10):**

- Nested lens read/write
- Patch `add` / `remove` pro `many` cardinality
- gqlgen schema

## Acceptance

| ID | Scénář | Status |
|----|--------|--------|
| A3 | Domain lens bez Q/P identity v klientovi | done |
