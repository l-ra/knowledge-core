# Koncepty Knowledge Core

Zkrácený model pro implementátory. Normativní detaily: [technický návrh](../design/knowledge_core_v1_technicky_navrh.md).  
Fyzické PostgreSQL schema a ER vazby: [data-model.md](data-model.md).  
Schema objekty: [ADR 0005 — Entity + Profile](../decisions/0005-entity-profile-schema.md).

## Čtyři vrstvy

1. **Knowledge graph** — Entity (včetně P*/C*), Statement, Qualifier, Reference
2. **Model / schema profiles** — `property_profile`, `class_profile`, Lens, Policy, Package, Release (privilegovaná metadata)
3. **Software** — Go služba + případné registrované domain hooks (žádný user script v lens/policy)
4. **Projections** — odvozené read modely; rebuildable; nejsou SoT

Canonical SoT = PostgreSQL datastore Knowledge Core.

## Identity

| Typ | Interní ID | Veřejný ID |
|-----|------------|------------|
| Entity | UUID | `Q<n>` / `P<n>` / `C<n>` (P = property profile, C = class profile) |
| Property (schema view) | stejné UUID jako entita | `P<n>` |
| Class (schema view) | stejné UUID jako entita | `C<n>` |
| Statement | UUID | `S<n>` |
| Reference | UUID | `R<n>` (v1) |
| ChangeSet | UUID | — |

Veřejné ID se **nikdy nerecyklují**. Label není identita.

Typing entity: statement `instanceOf` → entita `C*` (class_profile). Property/class jsou entity; o nich lze dělat statementy. Schema kontrakt (datatype, constraints, subClassOf) žije v profile, ne v obyčejných statements.

## Statement

First-class tvrzení: subject + property + typed value + status + volitelný valid time + package + qualifiers + references.

- Duplicitní `(subject, property, value)` je **povoleno**.
- Mutace jen přes explicitní operace: `create`, `revise`, `deprecate`, `delete`.
- Každá mutace patří do atomického **ChangeSetu**.

## Label

`label.en` je povinné systémové prezentační metadata pojmenovaných objektů — **není** Statement.

## Čas

- **Transaction time** — kdy ChangeSet commitnul (audit)
- **Valid time** — volitelný interval platnosti tvrzení ve světě
- Interval jako datatype ≠ valid time sloupce Statementu

## Package / Release

- Package vlastní modelové i graph objekty; cross-package reference je povolena.
- Release je **immutable** snapshot (bundle), ne kopie celé authoring DB.
- Revision ≠ Release.

## Lens

Deklarativní mapování graph ↔ domain objekt. Není security boundary, není ontologie, není executable kód.

Write jen explicitní patch: `set` / `clear` / `add` / `remove`.

## Authorization

Centrální engine na každém API path. Default **deny**. RBAC + omezený ABAC. Provenance ≠ audit ≠ ACL metadata.
