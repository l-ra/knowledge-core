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
- GraphQL: `POST /v1/graphql` — typed query `application(code:)` + generic `domain(lens, key)`
- Acceptance A3

## Acceptance

| ID | Scénář | Status |
|----|--------|--------|
| A3 | Domain lens bez Q/P identity v klientovi | done |
