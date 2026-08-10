# Zadání — Fáze 13: Vestavěné webové UI

**Status:** done (MVP)

## Cíl

Vestavěné SPA na `/ui` — administrace, editace modelu, Wikibase-like editor dat, search, OIDC, i18n cs/en, light/dark dle systému.

## Scope

**In:**

- React + Vite SPA, `go:embed`, cesta `/ui`
- Auth: OIDC PKCE + bootstrap + dev
- i18n: čeština / angličtina
- Data: search, entities list/create/edit statements (+ advanced qualifiers/refs/valid time)
- Model: properties, packages, lenses, policies (oddělená navigace)
- Ops: outbox / rebuild
- List API: `GET /v1/entities|properties|packages|lenses`, `GET /v1/ui/config`, `GET /v1/me`

## Design

CSS `prefers-color-scheme` light/dark; Fraunces + Source Sans 3.
