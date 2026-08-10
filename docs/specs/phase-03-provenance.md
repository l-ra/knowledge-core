# Zadání — Fáze 3: Provenance

## Cíl

Qualifiers, first-class References (`R<n>`), valid time na Statement; provenance oddělené od ChangeSet auditu.

## Scope

**In:**

- `reference`, `statement_reference`, `statement_qualifier`
- Revision snapshots qualifierů a referencí (`statement_revision_*`)
- Valid time (`validFrom` / `validTo`) na create/revise
- `POST /v1/references`, `GET /v1/references/{rid}`
- Statement create/revise s `qualifiers`, `referenceIds`, `validFrom`, `validTo`
- Acceptance A10

**Out:** packages, auth

## Deliverables

- [x] `reference`, `statement_reference`, `statement_qualifier`, revision snapshots
- [x] Valid time on create/revise
- [x] `POST/GET /v1/references`
- [x] Statement `qualifiers`, `referenceIds`, `validFrom`/`validTo`
- [x] Acceptance A10

## Stav

Implementováno v migraci `00003_provenance.sql`, `internal/store/{provenance,revision}.go`.
