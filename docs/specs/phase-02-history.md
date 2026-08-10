# Zadání — Fáze 2: History

## Cíl

Immutable revisions, ChangeSet jako transakční jednotka, optimistic locking, idempotentní writes, history API.

## Scope

**In:**

- `entity_revision`, `property_revision`, `statement_revision`
- `current_revision_no` na objektech
- ChangeSet s veřejným `C<n>`, actor, operation_type, idempotency_key, response replay
- `POST /v1/changesets` — atomický batch operací
- `POST /v1/statements/{sid}/revise` — revise s `expectedRevision`
- `PATCH /v1/entities/{qid}` — update labels s `expectedRevision`
- `GET /v1/changesets/{cid}`, history endpointy
- Header `Idempotency-Key` na všech write operacích

**Out:** plná authorization (A7 simulace přes validační reject), qualifiers/references (Fáze 3)

## Acceptance

| ID | Test |
|----|------|
| A7 | Batch se 3 revise; třetí s wrong revision → 0 committed |
| A8 | Revise s stale expectedRevision → 409 |
| A9 | Stejný Idempotency-Key 2× → 1 ChangeSet |

## API kontrakt

### Revise statement

`POST /v1/statements/{sid}/revise`

```json
{
  "expectedRevision": 1,
  "value": { "type": "String", "string": "new" }
}
```

### Batch ChangeSet

`POST /v1/changesets`

```json
{
  "operationType": "BatchRevise",
  "operations": [
    { "op": "reviseStatement", "statement": "S1", "expectedRevision": 1, "value": {...} }
  ]
}
```

## Deliverables

- [x] `entity_revision`, `property_revision`, `statement_revision`
- [x] `current_revision_no` + immutable revision rows on every mutation
- [x] ChangeSet `C<n>`, idempotency replay, request hash
- [x] `POST /v1/changesets` atomic batch
- [x] `POST /v1/statements/{sid}/revise` + `PATCH /v1/entities/{qid}`
- [x] History + ChangeSet GET endpoints
- [x] Acceptance A7/A8/A9

## Stav

Implementováno v migraci `00003_provenance.sql`, `internal/store/{provenance,revision}.go`.

Headers: `Idempotency-Key`, optional `X-Actor`, `X-Correlation-Id`

Response `409` on revision conflict. Duplicate idempotency key with same body → replay stored response.
