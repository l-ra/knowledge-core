# Phase 19 — Graph query API & schema packaging

Generické doplňky jádra pro nástroje nad ontologiemi (včetně ArchiMate Lite). Žádné ArchiMate typy v Go.

## P0

- `GET /v1/entities/{id}/incoming?property=` — statementy, kde `value_entity` je tato entita
- `GET /v1/entities?iriLocal=&package=&iri=` — resoluce podle `iri_local` / kanonického IRI / aliasu
- `rangeClasses` u `EntityReference` expanduje `instanceOf` cílového `Q*` (a `subClassOf`)
- `iriLocal`, `iri`, `packageCode` na `GET /v1/classes/{cid}` a `GET /v1/properties/{pid}`

## P1

- `GET /v1/entities?instanceOf=C*&includeSubclasses=true`
- `GET /v1/entities/{id}/graph?depth=1|2` — outgoing, incoming, neighbors
- `GET /v1/entities/{id}/statements?property=P*`
- Package dependencies JSON `dependsOnCode` / `versionRange`
- `shape_profile.package_id`; `GET /v1/shapes?package=`; shapes v release bundle (`objectType: shape`)

## P2

- `PATCH /v1/properties/{pid}` — `constraints`
- `POST /v1/entities/{id}/move` — `{ packageCode }`; přesune i statementy subjektu ve stejném starém package
- `POST /v1/statements` s `"upsert": true` — existující (subject, property, value) → 200
- `effectiveClasses` na GET entity (instanceOf + předci) a GET class (self + předci)
- OpenAPI `api/openapi.yaml` 0.2.0

Migrace: `00014_graph_api.sql`.
