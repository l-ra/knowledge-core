# Zadání — Fáze 17: Package/Entity IRI mapping for RDF

**Status:** implemented

## Cíl

Stabilní SoT identity (`Q*`/`P*`/`C*`) + konfigurovatelné kanonické exportní IRI pro RDF interoperabilitu.

## Model

- `package.iri_base` — absolutní http(s) base končící `/` nebo `#` (unikátní pokud nastaveno)
- `entity.iri_local` — volitelná relativní cesta; unikátní v rámci package
- Kanonické IRI = `iri_base + coalesce(iriLocal, public_id)`; bez base fallback na `https://knowledge-core.local/{entity|property}/`
- `entity_iri_alias` — externí IRIs (`sameAs` / `imported` / `canonical_export`); v RDF jako `owl:sameAs`

Well-known RDF/RDFS/OWL mapování (import ↔ export): [rdf-well-known-vocab.md](rdf-well-known-vocab.md).

## API / UI

- Package create/PATCH: `iriBase`
- Entity create/PATCH: `iriLocal`; GET vrací `iri`, `iriLocal`, `iriAliases`
- `PUT /v1/entities/{id}/iri-aliases`
- UI: package create/detail, entity create + edit labels, alias editor

## Out (later)

- RDF Turtle import → ChangeSet
- Auto-rebuild RDF při změně `iri_base` package (zatím manuální `/v1/projections/rdf/rebuild`)
