# Phase 15 — Entity + Profile

Greenfield přepis: properties a classes jsou entity s profilem. Viz [ADR 0005](../decisions/0005-entity-profile-schema.md).

## Schema

- `property_profile (entity_id PK → entity, datatype, constraints JSONB)`
- `class_profile (entity_id PK → entity, document JSONB)`
- `statement.property_id` → `property_profile(entity_id)`
- `shape_profile.class_id` → `class_profile(entity_id)`
- Dropped: `property_definition*`, `property_revision`, `class_definition*`

## API

| Endpoint | Poznámka |
|----------|----------|
| `POST /v1/properties` | Vytvoří entity `P*` + profile |
| `GET /v1/properties`, `GET /v1/properties/{pid}` | Schema view |
| `POST /v1/classes` | Vytvoří entity `C*` + profile (`subClassOf`, bez canonicalEntityId) |
| `GET /v1/classes`, `GET /v1/classes/{cid}` | Schema view |
| `GET /v1/entities/{id}` | Funguje pro Q/P/C |
| `POST /v1/statements` | Subject Q/P/C; property musí mít profile |

## Acceptance

- [x] Create property → entity `P1` + datatype; `GET /v1/entities/P1` OK
- [x] Create class → entity `C1`; statement `instanceOf → C1` OK
- [x] Statement o property (subject `P1`) OK
- [x] Statement s property = `Q*` bez profile → 400
- [x] Shape/validation domainClasses používá `C*`
- [x] Packages/ChangeSet stále fungují
