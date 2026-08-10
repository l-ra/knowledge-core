# Zadání — Fáze 1: Canonical graph

## Cíl

Open-world Entity / Property / Statement s closed datatype setem, povinným `label.en` a synchronní current-state projekcí. Generic Graph API (CRUD).

## Scope

**In:**

- Entity lifecycle (active create; status field ready)
- PropertyDefinition (datatype immutable po create)
- Statement create + read (status `active`)
- Labels / descriptions (povinné `label.en`)
- Hybrid typed value storage (ADR 0001)
- Synchronní update `statement_current` ve stejné txn
- Generic REST pod `/v1`

**Out (pozdější fáze):** ChangeSet audit UI, revisions API, qualifiers, references, packages, auth, lenses.

> Pozn.: Interně už Fáze 1 může vytvářet minimal ChangeSet při mutaci (příprava na Fázi 2), ale veřejný history kontrakt ještě není povinný.

## Datatypes v1

`EntityReference`, `String`, `LocalizedString`, `Boolean`, `Integer` (int64), `Decimal` (exact numeric), `Date`, `DateTime` (UTC), `URI`, `ExternalIdentifier`, `Quantity`, `Interval`.

## REST kontrakt (minimum)

| Method | Path | Popis |
|--------|------|-------|
| `POST` | `/v1/entities` | Create entity + labels |
| `GET` | `/v1/entities/{qid}` | Get entity |
| `POST` | `/v1/properties` | Create property definition |
| `GET` | `/v1/properties/{pid}` | Get property |
| `POST` | `/v1/statements` | Create statement |
| `GET` | `/v1/statements/{sid}` | Get statement |
| `GET` | `/v1/entities/{qid}/statements` | List statements of entity |

### Create Entity (request)

```json
{
  "labels": { "en": "Customer" },
  "descriptions": { "en": "A customer organization" }
}
```

Response `201`: `{ "id": "Q1", "canonicalId": "...", "labels": {...}, ... }`

Chybí `labels.en` → `400`.

### Create Property

```json
{
  "datatype": "String",
  "labels": { "en": "Code" }
}
```

### Create Statement

```json
{
  "subject": "Q1",
  "property": "P1",
  "value": { "type": "String", "string": "CRM-01" }
}
```

Value union: discriminator `type` + typovaná pole dle datatype Property.

## PG tabulky (minimum)

- `id_sequence` — alokace Q/P/S čísel
- `entity`
- `property_definition`
- `entity_label`, `entity_description`, `property_label`, `property_description`
- `statement`
- `statement_current` — current projection (sync)
- `change_set`, `change_set_item` — stub pro mutace (plně Fáze 2)

Detail schema: migrace v `migrations/`.

## Acceptance (z návrhu)

| ID | Scénář | Expected |
|----|--------|----------|
| A1 | Runtime create Property + Statement | Bez SQL migrace; Statement validní |
| A2 | Create Entity bez `label.en` | Reject |
| A14 | Write + immediate read | Current state vidí nová data bez async |

## Stav

Implementováno v `internal/{datatype,domain,store,engine,api/http}` + migrace `00001`. Acceptance A1/A2/A14: `internal/api/http/acceptance_test.go` (vyžaduje `KC_DATABASE_URL`).

