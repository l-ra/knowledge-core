# Phase 14 — Schema model & validation

Extends the knowledge model with classes, property constraints, and SHACL-like shape profiles while preserving open-world data entry.

## Model

- **ClassDefinition** (`C*`) — labels, optional `subClassOf`, optional `canonicalEntityId` linking to the entity used in `instanceOf` statements.
- **Property constraints** — JSON on `property_definition.constraints`: `domainClasses`, `rangeClasses`, `minCount`, `maxCount`, `severity`.
- **ShapeProfile** — per-class required/allowed properties, optional `closed` flag.
- **Schema config** — `instanceOfProperty` (property public id) configured via `PUT /v1/admin/schema-config`.

## Validation modes

| Mode | Header / query | Behaviour |
|------|----------------|-----------|
| `relaxed` | default | Writes succeed; API returns `validation` block with findings |
| `strict` | `X-Validation-Mode: strict` | HTTP 422 before write when error-level findings would occur |
| `off` | `X-Validation-Mode: off` | Skip validation |

Query override: `?validation=relaxed`

## API

- `GET/POST /v1/classes`, `GET /v1/classes/{cid}`
- `GET/POST /v1/shapes`, `GET /v1/shapes/{code}`
- `GET/PUT /v1/admin/schema-config`
- `GET /v1/entities/{qid}/validation`
- `POST /v1/validation/reports` — body `{ entityIds, persist?, scope? }`
- `GET /v1/validation/reports/{id}`

Write responses for `POST /v1/statements` include optional `validation` when mode is not `off`.

## UI

- Model → Classes, Shapes
- Admin → Schema config
- Entity detail → validation mode (advanced), post-save warnings, link to validation report

## Finding codes

- `domain_mismatch`, `range_mismatch`, `cardinality_min`, `cardinality_max`
- `shape_required_missing`, `shape_extra_property`
