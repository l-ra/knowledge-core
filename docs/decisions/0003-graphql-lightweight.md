# ADR 0003 — GraphQL adapter remains lightweight

**Status:** Accepted  
**Datum:** 2026-08-10  
**Kontext:** Fáze 10; ADR 0001 původně volil `gqlgen`

## Rozhodnutí

Zachovat lightweight GraphQL adapter (`POST /v1/graphql`) nad Lens engine
místo migrace na `gqlgen`.

## Důvody

1. Domain API je primárně REST lens read/patch; GraphQL je tenký adapter.
2. Dynamické lens dokumenty by vyžadovaly runtime schema generování (non-goal v1).
3. Existující `application(code:)` a `domain(lens, key)` pokrývají acceptance A3.
4. `gqlgen` by přidal codegen overhead bez typed schema stability.

## Důsledky

- ADR 0001 GraphQL řádek se aktualizuje na „lightweight adapter“.
- Plný GraphQL schema (gqlgen) je explicitní non-goal v1; možné v budoucí major verzi.
