# Zadání — Fáze 7: Outbox / Projections

**Status:** done

## Cíl

Transactional outbox pro publikaci změn do externích projekcí. Search projection s možností full rebuild z canonical datastore (A15).

## Scope

**In:**

- Tabulka `outbox_event` (zápis v téže transakci jako ChangeSet)
- Search projection `projection_search`
- Projector: `POST /v1/projections/outbox/process`
- Rebuild: `POST /v1/projections/search/rebuild` (z canonical, ne z outbox replay)
- Query: `GET /v1/projections/search?q=…`
- Acceptance A15

## Acceptance

| ID | Scénář | Status |
|----|--------|--------|
| A15 | Projection rebuild z canonical po smazání | done |
