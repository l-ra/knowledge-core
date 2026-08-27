# Zadání — Fáze 16: Package UI + progressive ChangeSet draft

**Status:** implemented

> **Poznámka:** per-user JSON draft (`/v1/me/changeset-draft*`) má nahradit [Fáze 20 — Open ChangeSet v DB](phase-20-open-changeset.md). Package UI část této fáze zůstává.

## Cíl

UI pro správu packages/releases a postupné skládání ChangeSetu (draft per user na serveru).

## Scope

**In:**

- Per-user draft: `user_changeset_draft` + `GET/PUT/DELETE /v1/me/changeset-draft`, `POST …/commit`
- Default: každá editace = vlastní ChangeSet; s otevřeným draftem se ops bufferují a commitnou atomicky
- Optimistic locking přes `expectedRevision` v draft ops
- Package filter na seznamu entit (`?package=`)
- Package detail: objects, releases, publish, export; import na packages page
- Cross-package warning při add statement; release membership badges na entity detail
- ApplyChangeSet rozšířen o createEntity/Property/Class/Statement, deprecateStatement, deprecateEntity, deleteEntity + clientKey remapping (`$…`)

**Out:**

- Multi-draft per user
- Revert již commitnutých draft ops
- Release diff UI (jen object index / membership badges)

## Acceptance

| Scénář | Status |
|--------|--------|
| Draft open → queue create → commit → entity existuje, draft closed | done (test) |
| Bez draftu každá mutace vlastní ChangeSet | done (stávající chování) |
| Class v release/bundle | done (test) |
| Import bundle fail on collision | done (stávající) |
