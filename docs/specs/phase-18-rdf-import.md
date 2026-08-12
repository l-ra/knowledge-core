# Zadání — Fáze 18: RDF N-Triples import

**Status:** implemented (global)

## Cíl

Globální import RDF (N-Triples): analýza prefixů → mapování na packages → create/update packages → per-package import (stávající ChangeSet logika).

## API

- `POST /v1/rdf/analyze` `{ ntriples?, turtlePrefixes? }` → kandidáti prefixů (`iriBase`, suggested code/action create|update|use, `source` detected|turtle|detected+turtle), seznam existujících packages. Alespoň jedno z polí je povinné.
- `POST /v1/rdf/prefixes` `{ turtlePrefixes }` → extrakce `@prefix` / `PREFIX` deklarací z Turtle hlavičky (`prefix`, `iriBase`, `suggestedCode`)
- `POST /v1/rdf/import` `{ ntriples, dryRun, assignments[] }`  
  assignment: `{ iriBase, packageCode, create, setIriBase, label? }`  
  → vytvoří/aktualizuje packages, filtruje triples podle subject prefixu (longest match), volá stávající per-package import
- Legacy: `POST /v1/packages/{code}/rdf/import` zůstává (jeden package)

## Pravidla importu (per package)

- Match: `iri_base+iri_local` nebo `entity_iri_alias`
- Kind: `rdf:Property` → P*, `rdfs:Class`/`owl:Class` → C*, jinak Q*
- Structural: type/label/sameAs/subClassOf/domain(warning)/range
- Konflikty: neupdatovat existující; duplikáty skip; datatype mismatch skip
- Datatype nových P*: range + pozorované hodnoty; konflikt → `Any`

Well-known vocab (co se mapuje / co se nepřekládá na package): [rdf-well-known-vocab.md](rdf-well-known-vocab.md).

## UI

Packages list:

1. Upload / paste N-Triples
2. Volitelně paste Turtle `@prefix` hlavičky → **Extrahovat @prefix** (merge do tabulky) nebo **Analyzovat** (NT detekce + turtle)
3. Ruční úprava řádků: změna `iriBase` / package code, přidání / odebrání
4. Dry-run → commit
