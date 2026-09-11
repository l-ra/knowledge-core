# API kontrakt pro klienty

Shrnutí HTTP kontraktu Knowledge Core pro integraci. Kompletní seznam cest: [`api/openapi.yaml`](../../api/openapi.yaml).

---

## Base URL a autentizace

Všechny `/v1/*` endpointy vyžadují autentizaci (kromě `GET /v1/ui/config`).

| Režim (`KC_AUTH_MODE`) | Headers |
|------------------------|---------|
| `bootstrap` | `Authorization: Bearer <password>` nebo `X-Admin-Password: <password>` |
| `oidc` | `Authorization: Bearer <JWT>` |
| `dev` | `X-Subject: <id>` + volitelně `X-Roles: admin,editor` |

```bash
curl -H "Authorization: Bearer $KC_TOKEN" http://localhost:8080/v1/me
```

---

## Write headers

| Header | Účel |
|--------|------|
| `Content-Type: application/json` | Povinné u POST/PATCH/PUT |
| `Idempotency-Key` | Retry-safe writes — stejný klíč + stejné tělo → 200 replay; jiné tělo → 409 |
| `X-Actor` | Volitelný actor ID (default = authenticated subject) |
| `X-Correlation-Id` | Korelace requestů (echo v response) |
| `X-Validation-Mode` | `relaxed` (default) / `strict` / `off` |
| `X-Knowledge-Changeset` | Zápis do open ChangeSetu (overlay); bez headeru = okamžitý committed CS |
| `X-Knowledge-Changesets` | Read-merge: čárkou oddělené open CS id (GET entity/list/statements) |

Alternativa validace: query param `?validation=strict`.

### Open ChangeSet (postupné skládání)

1. `POST /v1/changesets/open` → `{ data: { id, status: "open", … } }`
2. Mutace s `X-Knowledge-Changeset: <id>` zapisují do overlay (committed graf se nemění)
3. Čtení s `X-Knowledge-Changesets: <id>` vrací committed ⊕ overlay
4. `PATCH /v1/changesets/{id}` — úprava popisných polí open CS (`comment`, `operationType`)
5. `POST /v1/changesets/{id}/commit` — optimistic lock na claimnutých objektech; úspěch = další committed CS; konflikt → 409, CS zůstane open
6. `POST /v1/changesets/{id}/cancel` — smaže overlay

Grafové zápisy do overlay: entity/statement CRUD, property/class create, `PATCH /properties/{pid}`, `PUT …/iri-aliases`, `POST …/move`, a **`POST /v1/changesets` batch** (append do open CS).

Veřejné REST mutátory grafu jsou thin wrappers: sestaví `operations` délky 1 a volají stejný `ApplyChangeSet` jako batch (committed i open).

Limity batch: max **500** ops (400 `batch_limit_exceeded`), body ≤ **4 MiB** (413).

S open-CS headerem → **400** `unsupported_in_open_changeset` (ne tichý committed write): packages, releases, RDF import, shapes, lens create, schema-config, reference create.

`PUT …/iri-aliases` vždy tvoří ChangeSet (committed i open).

Detail: [phase-20-open-changeset.md](../specs/phase-20-open-changeset.md), [phase-21-unified-batch-writes.md](../specs/phase-21-unified-batch-writes.md).

---

## Response obálky

### Writes (create/update)

```json
{
  "data": { …resource… },
  "changeSet": {
    "id": "C1",
    "canonicalId": "uuid",
    "actor": "admin",
    "operationType": "createEntity",
    "committedAt": "2026-08-17T08:00:00Z",
    "items": [
      { "objectType": "entity", "objectId": "uuid", "publicId": "Q1", "op": "create" }
    ]
  }
}
```

Volitelně u `POST /v1/statements`:

```json
{ "data": { … }, "changeSet": { … }, "validation": { "entityId": "Q1", "findings": […], "summary": { … } } }
```

HTTP status: **201** create · **200** idempotency replay nebo `upsert: true`

### Reads

Resource **bez** obálky `data`:

```json
{ "id": "Q1", "labels": { "en": "…" }, … }
```

### Lists

```json
{ "items": [ … ], "nextCursor": "opaque-token" }
```

Default `limit` 50, max 200. Prázdný `nextCursor` = konec.

### List expand (phase-22)

`GET /v1/entities` volitelné query:

| Parametr | Význam |
|----------|--------|
| `include=effectiveClasses` | Každá položka má `effectiveClasses` |
| `include=statements` | Embedded statements subjektu; **vyžaduje** `properties=` |
| `properties=P1,P2` | Whitelist (public id nebo `iriLocal`) |

```http
GET /v1/entities?package=org&limit=50&include=effectiveClasses,statements&properties=actorKind
```

```http
GET /v1/entities/facets?package=org&groupBy=instanceOf
→ { "facets": [ { "classId": "C…", "count": 42 } ] }
```

```http
POST /v1/entities/batch-read
{ "ids": ["Q1","Q2"], "include": ["effectiveClasses","statements"], "properties": ["P…"] }
→ { "results": [ { "id": "Q1", "entity": {…}, "statements": […] }, { "id": "Q2", "error": "not_found" } ] }
```

Detail: [phase-22-list-expand-batch-read.md](../specs/phase-22-list-expand-batch-read.md).

---

## Chybové odpovědi

```json
{ "error": { "code": "invalid", "message": "labels.en is required" } }
```

| HTTP | `error.code` | Typické příčiny |
|------|--------------|-----------------|
| 400 | `invalid` | Chybějící pole, špatný formát |
| 401 | `unauthenticated` | Chybí/neplatná auth |
| 403 | `forbidden` | Default deny — chybí policy/role |
| 404 | `not_found` | Neexistující objekt |
| 409 | `conflict` | Idempotency conflict, immutable release, iriLocal konflikt |
| 422 | `validation` | Strict validation failed (+ top-level `validation` objekt) |
| 500 | `error` | Interní chyba |

422 response:

```json
{
  "error": { "code": "validation", "message": "…" },
  "validation": {
    "entityId": "Q42",
    "findings": [{ "severity": "error", "code": "…", "message": "…" }],
    "summary": { "errors": 1, "warnings": 0 }
  }
}
```

---

## Value typy (statement value)

Discriminator `type` + typované pole. `value.type` musí odpovídat `datatype` property.

| `type` | JSON pole | Příklad |
|--------|-----------|---------|
| `EntityReference` | `entityId` | `{ "type": "EntityReference", "entityId": "Q12" }` |
| `String` | `string` | `{ "type": "String", "string": "high" }` |
| `LocalizedString` | `langMap` | `{ "type": "LocalizedString", "langMap": { "en": "…", "cs": "…" } }` |
| `Boolean` | `bool` | `{ "type": "Boolean", "bool": true }` |
| `Integer` | `int64` | `{ "type": "Integer", "int64": 5432 }` |
| `Decimal` | `decimal` | `{ "type": "Decimal", "decimal": "12.50" }` |
| `Date` | `date` | `{ "type": "Date", "date": "2026-08-17" }` |
| `DateTime` | `dateTime` | `{ "type": "DateTime", "dateTime": "2026-08-17T08:00:00Z" }` |
| `URI` | `uri` | `{ "type": "URI", "uri": "https://…" }` |
| `ExternalIdentifier` | `scheme`, `value` | `{ "type": "ExternalIdentifier", "scheme": "doi", "value": "10.1234/…" }` |
| `Quantity` | `quantityValue`, `unitEntityId` | `{ "type": "Quantity", "quantityValue": "100", "unitEntityId": "Q5" }` |
| `Interval` | `from`, `to` | `{ "type": "Interval", "from": { … }, "to": { … } }` |

Qualifiers používají stejný Value formát: `{ "property": "P*", "value": { … } }`.

---

## Klíčové request/response typy

### Create Entity

```json
// POST /v1/entities
{
  "packageCode": "sys-crm",
  "labels": { "en": "CRM Backend" },
  "descriptions": { "en": "Optional description" },
  "iriLocal": "crm-backend"
}
```

Response `data`:

```json
{
  "id": "Q42",
  "canonicalId": "uuid",
  "status": "active",
  "kind": "entity",
  "revisionNo": 1,
  "labels": { "en": "CRM Backend" },
  "descriptions": { "en": "…" },
  "packageCode": "sys-crm",
  "iriLocal": "crm-backend",
  "iri": "https://example.org/systems/crm/crm-backend",
  "iriAliases": [],
  "createdAt": "…",
  "updatedAt": "…"
}
```

### Create Statement

```json
// POST /v1/statements
{
  "packageCode": "sys-crm",
  "subject": "Q42",
  "property": "P3",
  "value": { "type": "EntityReference", "entityId": "C5" },
  "qualifiers": [],
  "referenceIds": [],
  "validFrom": "2026-01-01T00:00:00Z",
  "validTo": null,
  "upsert": false
}
```

### Create Package

```json
// POST /v1/packages
{
  "code": "my-domain",
  "lifecycle": "released",
  "iriBase": "https://example.org/my-domain/",
  "labels": { "en": "My Domain" },
  "descriptions": { "en": "Optional package description (stored on package-root entity)" },
  "dependencies": [
    { "dependsOnCode": "other-pkg", "versionRange": "^1.0.0" }
  ]
}
```

With `iriBase`, the engine also creates a **package-root** entity (`publicId` = `iriBase`, `iriLocal` = `.package`). Response includes `rootEntityId`. After `kc-base` vocabulary is loaded, root is typed `instanceOf → Package` and gets `packageCode` statement.

### Create Class

```json
// POST /v1/classes
{
  "packageCode": "my-domain",
  "labels": { "en": "Application Component" },
  "iriLocal": "ApplicationComponent",
  "subClassOf": "C1"
}
```

### Create Property

```json
// POST /v1/properties
{
  "packageCode": "my-domain",
  "datatype": "EntityReference",
  "labels": { "en": "Source" },
  "iriLocal": "relSource",
  "constraints": {
    "domainClasses": ["C10"],
    "rangeClasses": ["C5"],
    "minCount": 1,
    "maxCount": 1
  }
}
```

### Schema Config

```json
// GET/PUT /v1/admin/schema-config (bez changeSet obálky)
{
  "instanceOfProperty": "P3",
  "modelProperties": ["P1", "P2"],
  "updatedAt": "2026-08-17T08:00:00Z"
}
```

---

## Lens API

### Registrace

```json
// POST /v1/lenses
{
  "code": "application",
  "labels": { "en": "Application" },
  "document": {
    "entitySelector": { "typeEntity": "uuid-of-class" },
    "key": { "property": "P-code-property" },
    "fields": {
      "name": { "property": "P-name", "type": "String", "cardinality": "one" },
      "owner": {
        "property": "P-owner",
        "type": "EntityReference",
        "cardinality": "zeroOrOne",
        "lens": "organization"
      },
      "tags": { "property": "P-tags", "type": "String", "cardinality": "many" }
    }
  }
}
```

Cardinality: `one` | `zeroOrOne` | `many`.

### Read instance

```http
GET /v1/lenses/application/instances/CRM-01
```

Response (bare field map, ne `{data}`):

```json
{ "name": "CRM", "owner": { "name": "IT Dept" }, "tags": ["critical"] }
```

### Patch instance

```json
// POST /v1/lenses/application/instances/CRM-01/patch
{
  "operations": [
    { "op": "set", "field": "name", "value": { "type": "String", "string": "CRM v2" } },
    { "op": "clear", "field": "owner" },
    { "op": "add", "field": "tags", "value": { "type": "String", "string": "legacy" } },
    { "op": "remove", "field": "tags", "value": { "type": "String", "string": "legacy" } }
  ]
}
```

Response: `{ "data": { …instance fields… } }` (bez changeSet).

### GraphQL

```http
POST /v1/graphql
Content-Type: application/json

{ "query": "{ application(code: \"CRM-01\") { name owner { name } } }" }
```

---

## Veřejná ID

| Prefix | Objekt |
|--------|--------|
| `Q*` | Entity |
| `P*` | Property |
| `C*` | Class |
| `S*` | Statement |
| `R*` | Reference |
| `C*` (changeset) | ChangeSet — stejný prefix jako class, jiný endpoint |

`labels.en` je **povinné** u create operací pojmenovaných objektů.

---

## Endpointy podle účelu

| Účel | Endpointy |
|------|-----------|
| Health | `GET /healthz` |
| Auth info | `GET /v1/me` |
| Packages | `GET/POST /v1/packages`, `GET /v1/packages/{code}`, releases, bundle, import |
| Schema | `GET/POST /v1/classes`, `GET/POST /v1/properties`, `PATCH /v1/properties/{pid}`, shapes, schema-config |
| Graph CRUD | `GET/POST /v1/entities`, `GET /v1/entities/facets`, `POST /v1/entities/batch-read`, `PATCH /v1/entities/{qid}`, `POST …/deprecate`, `POST …/delete`, statements, incoming, graph, move, iri-aliases |
| History | `GET …/history`, `GET/POST /v1/changesets`, `POST /v1/changesets/open`, `GET/PATCH /v1/changesets/{cid}`, `POST …/{cid}/commit|cancel` |
| Lenses | `GET/POST /v1/lenses`, instances, patch, GraphQL |
| Projections | `GET /v1/projections/search`, `/v1/projections/rdf`, rebuild endpoints |
| Auth policies | `GET/POST /v1/policies`, `GET/PUT/DELETE /v1/policies/{name}` |

Spustitelné příklady: [examples.http](examples.http).
