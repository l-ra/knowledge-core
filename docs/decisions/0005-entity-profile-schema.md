# ADR 0005 — Entity + Profile schema (properties & classes)

**Status:** Accepted  
**Datum:** 2026-08-11

## Context

Properties a classes byly oddělené tabulky (`property_definition`, `class_definition`). O schema objektech nešlo dělat statementy; class potřebovala `canonicalEntityId` jako most do grafu.

Požadavek: všechno důležité v grafu je **entita**; schema kontrakt (datatype, constraints, subClassOf) žije v **privilegovaném profile**; ChangeSet/packages/lenses zůstávají.

## Decision

1. Property = řádek v `entity` s `public_id` `P*` + `property_profile` (1:1).
2. Class = řádek v `entity` s `public_id` `C*` + `class_profile` (1:1). Žádný canonical entity bridge.
3. Labels/descriptions jen přes `entity_label` / `entity_description`.
4. Statement subject = libovolná entita (Q/P/C). Predicate = pouze entita s `property_profile` (FK na `property_profile.entity_id`).
5. Profile je SoT pro schema; RDF může profile **emitovat** jako triplety, ne přepisovat přes běžné statements.
6. Historie property/class přes `entity_revision` + ChangeSet (žádné `property_revision`).
7. Greenfield: baseline migrace přepsány; žádný data backfill.

## Consequences

- `/v1/properties` a `/v1/classes` zůstávají schema views nad entity+profile.
- `/v1/entities/{id}` funguje i pro `P*` / `C*`.
- Validace domain/range odkazuje `C*` přímo.
- Breaking vůči starému `canonicalEntityId` a tabulkám `property_definition` / `class_definition`.
