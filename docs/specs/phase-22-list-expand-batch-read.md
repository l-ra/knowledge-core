# Phase 22 — List expand, facets, batch-read

**Status:** done  
**Souvisí:** [phase-19-graph-api.md](phase-19-graph-api.md), IT Map `docs/zadani-cards-hub-performance.md` (fáze B)

Rozšíření read API, aby klienti (IT Map Cards hub a další) nemuseli skládat N+1 `GET …/statements` po `listEntities`.

## Scope

| ID | API | Povinné |
|----|-----|---------|
| B1 | `GET /v1/entities?include=effectiveClasses\|statements&properties=` | ano |
| B2 | `GET /v1/entities/facets?package=&groupBy=instanceOf` | ano |
| B3 | `POST /v1/entities/batch-read` | ano |
| B4 | Lens instances list | ne (odloženo) |

Shapes do této fáze nevstupují.

## B1 — Expand na listEntities

Query parametry:

| Parametr | Význam |
|----------|--------|
| `include` | CSV: `effectiveClasses`, `statements` |
| `properties` | CSV property public id (`P*`) nebo `iriLocal`; **povinné** při `include=statements` |

Pravidla:

- `include=effectiveClasses` — každá položka má `effectiveClasses` (instanceOf + předci), stejně jako `GET /v1/entities/{id}`.
- `include=statements` bez `properties` → `400 invalid`.
- Embedded statements: aktivní statements subjektu omezené na whitelist properties (max stejný jako list statements, typicky ≤200 / entita).

## B2 — Facets

```http
GET /v1/entities/facets?package=org&groupBy=instanceOf
```

Response:

```json
{
  "facets": [
    { "classId": "C…", "count": 42 }
  ]
}
```

`groupBy=instanceOf` počítá **přímé** `instanceOf` (ne expandované subclass stromy). Counts odpovídají cardinalitě `GET /v1/entities?instanceOf=C*&includeSubclasses=false` pro daný package (modulo ACL).

## B3 — Batch read

```http
POST /v1/entities/batch-read
{
  "ids": ["Q1", "Q2"],
  "include": ["effectiveClasses", "statements"],
  "properties": ["P…"]
}
```

- Max 200 ids / request.
- Chybějící / nepovolené id → položka v `results` s `"error": "not_found"` (ne 404 celé dávky).
- `include=statements` vyžaduje `properties`.

## Acceptance

| ID | Scénář | Očekávání |
|----|--------|-----------|
| B1-a | list 50 + `include=effectiveClasses` | 1 HTTP; položky mají classes |
| B1-b | list + `include=statements&properties=P*` | 1 HTTP; statements whitelist |
| B1-c | `include=statements` bez properties | 400 |
| B2-a | facets package | O(1) HTTP; count shoda s list instanceOf |
| B3-a | batch 100 id | 1 HTTP; missing id explicit |

## Out of scope

- Lens list instances (B4)
- GraphQL-only projection
- Write batch (phase-21)
