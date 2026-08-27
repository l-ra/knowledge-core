# Datový model Knowledge Core

Fyzické PostgreSQL schema odvozené z migrací `migrations/00001`–`00014`.  
Logické koncepty: [overview.md](overview.md). Schema objekty: [ADR 0005](../decisions/0005-entity-profile-schema.md).

**SoT** = canonical graph + schema profiles v PostgreSQL.  
**Projekce** (`projection_*`) a outbox jsou odvozené a rebuildable.

## Identity

| Typ | Tabulka | Public ID |
|-----|---------|-----------|
| Ordinary entity | `entity` | `Q*` |
| Property | `entity` + `property_profile` | `P*` |
| Class | `entity` + `class_profile` | `C*` |
| Statement | `statement` | `S*` |
| Reference | `reference` | `R*` |

## Canonical graph + profiles (ER)

```mermaid
erDiagram
    package ||--o{ entity : owns
    entity ||--o{ entity_label : has
    entity ||--o{ entity_description : has
    entity ||--o| property_profile : "P* schema"
    entity ||--o| class_profile : "C* schema"
    class_profile ||--o{ shape_profile : shaped_by
    package ||--o{ shape_profile : owns

    entity ||--o{ statement : subject
    property_profile ||--o{ statement : predicate
    entity ||--o{ statement : "value_entity opt"

    statement ||--|| statement_current : mirror
    statement ||--o{ statement_qualifier : has
    property_profile ||--o{ statement_qualifier : pred
    statement ||--o{ statement_reference : cites
    reference ||--o{ statement_reference : used_by

    entity {
        uuid id PK
        text public_id UK
        text status
        uuid package_id FK
    }
    property_profile {
        uuid entity_id PK_FK
        text datatype
        jsonb constraints
    }
    class_profile {
        uuid entity_id PK_FK
        jsonb document
    }
    statement {
        uuid id PK
        text public_id UK
        uuid subject_id FK
        uuid property_id FK
        text value_type
        uuid value_entity_id FK
    }
```

### Vazby

| Od | Do | Poznámka |
|----|-----|----------|
| `statement.subject_id` | `entity` | Q/P/C |
| `statement.property_id` | `property_profile.entity_id` | jen entity s profilem |
| `property_profile.entity_id` | `entity` | 1:1, public_id `P*` |
| `class_profile.entity_id` | `entity` | 1:1, public_id `C*` |
| `shape_profile.class_id` | `class_profile` | |
| `shape_profile.package_id` | `package` | volitelné vlastnictví + release bundle |
| `instanceOf` hodnota | `C*` entita | přes `model_schema_config.instance_of_property` |

Profile = SoT pro datatype/constraints/subClassOf. RDF může profile emitovat (`rdf:Property`, `rdfs:Class`).

## Historie / ChangeSet

Beze změny konceptu: `change_set`, `entity_revision`, `statement_revision`. Property/class historie = `entity_revision` (+ payload v change_set_item).

## Packages / projections / auth

`package`, `release*`, `lens_definition`, `auth_policy`, `auth_runtime`, `outbox_event`, `projection_search`, `projection_rdf`, `model_schema_config`, `validation_report` — viz migrace 00004–00010.

## Katalog migrací

| Migrace | Obsah |
|---------|--------|
| `00001` | entity, property_profile, statement, statement_current, change_set |
| `00002` | entity_revision, statement_revision |
| `00003` | reference, qualifiers |
| `00004` | package, release |
| `00005`–`00009` | auth, lenses, outbox, RDF, auth_runtime |
| `00010` | class_profile, shape_profile, schema config, validation_report |
| `00011` | schema model properties |
| `00012` | user ChangeSet draft |
| `00013` | IRI mapping / aliases |
| `00014` | incoming index; `shape_profile.package_id` |
| `00015` | ChangeSet list indexes |
