# Implementační návod pro klientské aplikace

Postup vytvoření doménového knowledge grafu nad Knowledge Core. Na konci je **ArchiMate Lite** jako referenční implementace stejného patternu.

Předpoklady: běžící KC instance, autentizace — viz [post-install.md](../ops/post-install.md).  
JSON kontrakty: [api-contract.md](api-contract.md). Spustitelné příklady: [examples.http](examples.http).

---

## Přehled kroků

```text
1. Package (metamodel)     → code, iriBase, labels
2. Classes + Properties    → C*, P*, constraints, iriLocal
3. Schema config           → instanceOfProperty (globální typing)
4. Shapes (volitelně)      → validace instancí
5. Instance packages       → závislost na metamodelu
6. Entity + Statements     → Q* + S* (instanceOf, atributy, vazby)
7. Dotazování              → statements, incoming, graph, search
8. Release (volitelně)     → bundle export/import
```

---

## 1. Vytvoř metamodel package

Metamodel = slovník domény (třídy, properties, tvary). Drž ho v jednom package.

```http
POST /v1/packages
Content-Type: application/json
Authorization: Bearer {{token}}
```

```json
{
  "code": "my-domain",
  "lifecycle": "released",
  "iriBase": "https://example.org/my-domain/",
  "labels": { "en": "My Domain Model" },
  "dependencies": []
}
```

Odpověď: `{ "data": { "code", "iriBase", "labels", … }, "changeSet": { … } }`

**Konvence:**

- `code` — krátký slug (`my-domain`, `archimate-lite`)
- `iriBase` — končí `/`; kanonické IRI = `iriBase` + `iriLocal`
- `lifecycle: released` — metamodel se publikuje releases; `continuous` pro instance packages

---

## 2. Definuj třídy a properties

### Třída (C*)

```json
{
  "packageCode": "my-domain",
  "labels": { "en": "Application Component" },
  "iriLocal": "ApplicationComponent",
  "subClassOf": "C1"
}
```

`POST /v1/classes` → `{ "data": { "id": "C5", "iriLocal", "iri", … }, "changeSet" }`

`subClassOf` je volitelné — pro hierarchii typů.

### Property (P*)

```json
{
  "packageCode": "my-domain",
  "datatype": "String",
  "labels": { "en": "Criticality" },
  "iriLocal": "criticality",
  "constraints": {
    "rangeClasses": [],
    "domainClasses": ["C5"]
  }
}
```

`POST /v1/properties`

Podporované datatypes: `EntityReference`, `String`, `LocalizedString`, `Boolean`, `Integer`, `Decimal`, `Date`, `DateTime`, `URI`, `ExternalIdentifier`, `Quantity`, `Interval`.

Constraints (`domainClasses`, `rangeClasses`, `minCount`, `maxCount`) lze později upravit:

```http
PATCH /v1/properties/P12
{ "constraints": { "rangeClasses": ["C3"] } }
```

### Resoluce slovníku

**Nikdy nehardcoduj** `C12` / `P7` z jiné instalace. Vždy resolvuj přes `iriLocal`:

```http
GET /v1/entities?package=my-domain&iriLocal=ApplicationComponent
GET /v1/entities?package=my-domain&kind=class&limit=200
GET /v1/entities?package=my-domain&kind=property&limit=200
```

---

## 3. Nastav schema config

Globální property pro typing instancí — typicky `instanceOf` z package `kc-base`:

```http
PUT /v1/admin/schema-config
```

```json
{
  "instanceOfProperty": "P3"
}
```

`P3` = public ID property s `iriLocal=instanceOf` v dané instalaci.

> Nástroj vždy čte `instanceOfProperty` ze schema-config, nikdy ho nehardcoduje.

Loader ArchiMate Lite to nastaví automaticky, pokud config byl prázdný.

---

## 4. Shapes (volitelná validace)

Shape definuje požadované properties pro instanci určité třídy:

```json
{
  "code": "app-component",
  "packageCode": "my-domain",
  "classId": "C5",
  "document": {
    "requiredProperties": ["P8", "P12"]
  }
}
```

`POST /v1/shapes` · `GET /v1/shapes?package=my-domain`

Shapes jsou součástí release bundle (`objectType: "shape"`).

---

## 5. Vytvoř instance package

Každý evidovaný systém / doménový kontext = vlastní package se závislostí na metamodelu:

```json
{
  "code": "sys-crm",
  "lifecycle": "continuous",
  "iriBase": "https://example.org/systems/crm/",
  "labels": { "en": "CRM System" },
  "dependencies": [
    { "dependsOnCode": "my-domain", "versionRange": "*" }
  ]
}
```

Sdílené závislosti (IdP, DNS, lokality) patří do společného package (`platform`, `network`).

---

## 6. Vytvoř entity a statementy

### Instance entity

```json
{
  "packageCode": "sys-crm",
  "labels": { "en": "CRM Backend" },
  "iriLocal": "crm-backend"
}
```

`POST /v1/entities` → `{ "data": { "id": "Q42", … }, "changeSet" }`

### Typ instance (instanceOf)

```json
{
  "packageCode": "sys-crm",
  "subject": "Q42",
  "property": "P3",
  "value": { "type": "EntityReference", "entityId": "C5" }
}
```

`P3` = `instanceOfProperty` ze schema-config, `C5` = třída z kroku 2.

### Atributy a vazby

```json
{
  "packageCode": "sys-crm",
  "subject": "Q42",
  "property": "P12",
  "value": { "type": "String", "string": "high" }
}
```

Vazba jako first-class entita (Wikibase pattern):

```text
Q:rel-1  instanceOf → C:Composition
Q:rel-1  relSource  → Q:crm
Q:rel-1  relTarget  → Q:backend
```

Opakovaný zápis stejné trojice bez duplicity:

```json
{ "subject": "Q42", "property": "P12", "value": { … }, "upsert": true }
```

→ existující statement, HTTP **200**.

### Aliasy pro round-trip s externími ID

```http
PUT /v1/entities/Q42/iri-aliases
```

```json
{
  "aliases": [
    { "iri": "https://external-tool/id-1234", "kind": "imported" }
  ]
}
```

`kind`: `imported` | `sameAs` | `canonical_export`

### Přesun mezi packages

```http
POST /v1/entities/Q42/move
{ "packageCode": "sys-crm" }
```

Přesune entitu a statementy subjektu ze stejného starého package.

---

## 7. Dotazování grafu

| Účel | Endpoint |
|------|----------|
| Odchozí tvrzení | `GET /v1/entities/{qid}/statements?property=P*` |
| Příchozí tvrzení | `GET /v1/entities/{qid}/incoming?property=P*` |
| Okolí (1–2 hops) | `GET /v1/entities/{qid}/graph?depth=1` |
| Instance třídy | `GET /v1/entities?instanceOf=C*&includeSubclasses=true` |
| Full-text | `GET /v1/projections/search?q=crm` |
| RDF dump | `GET /v1/projections/rdf` |

Stránkování seznamů: `{ "items": […], "nextCursor": "…" }` → další stránka `?cursor=…`.

---

## 8. Release a promotion

### Publikuj metamodel

```http
POST /v1/packages/my-domain/releases
{ "version": "1.0.0" }
```

Pokud package už má starší release, publish **odmítne breaking** změny (409 `compat_breaking`). Povolené jsou additive/metadata změny vůči předchozímu release.

### Export bundle

```http
GET /v1/packages/my-domain/releases/1.0.0/bundle
```

### Import do jiného prostředí

```http
POST /v1/releases/import
{ …bundle JSON… }
```

- První import (greenfield) — OK.
- Import **nové verze** téhož package — OK, pokud je zpětně kompatibilní; vyšší `revisionNo` se aplikují.
- Stejná verze znovu / breaking změny / downgrade revize — **409**.

Published release je **immutable** — mutace vrátí **409**.

Podrobnosti BC matice: [phase-package-upgrade-compat.md](../specs/phase-package-upgrade-compat.md).

---

## Volba API

| Přístup | Kdy použít | Příklad |
|---------|------------|---------|
| **Přímý graph API** | Model authoring, import/export, debugging, speciální integrace | `POST /v1/statements`, `GET …/graph` |
| **Lens REST** | Aplikace pracuje s domain field names, ne Q/P | `GET /v1/lenses/app/instances/CRM` |
| **GraphQL** | Jednoduché dotazy nad lens | `POST /v1/graphql` |

Doporučení:

- Nástroje pro modeláře a ETL → **graph API** + resoluce přes `iriLocal`
- Business aplikace s pevným domain modelem → **lens** (deklarativní mapování)
- Obojí může koexistovat nad stejnými daty

---

## Referenční příklad: ArchiMate Lite

Kompletní implementace stejného postupu pro doménu ArchiMate:

| Materiál | Popis |
|----------|-------|
| [archimate-lite-kc.md](../models/archimate-lite-kc.md) | Plný kontrakt pro nástroje (endpointy, vzory, validace) |
| [archimate-lite.md](../models/archimate-lite.md) | Doménový rozsah (granularita L0–L4) |
| [kc-base](../../models/kc-base/) | Foundation (`instanceOf`, usage anotace, `StringEnum`) |
| [catalog.json](../../models/archimate-lite/catalog.json) | Seed ArchiMate Lite v gitu |
| [load.py](../../models/archimate-lite/load.py) | Idempotentní bootstrap (`kc-base` + `archimate-lite`) |

```bash
export KC_BASE_URL=http://localhost:8080
export KC_TOKEN='…'
python3 models/archimate-lite/load.py
```

Loader demonstruje:

1. Package `kc-base`, pak `archimate-lite` s `iriBase` a závislostí
2. Třídy, properties, constraints (`rangeClasses`)
3. Schema config (`instanceOfProperty` z `kc-base`)
4. Shapes (`aml-element`, `aml-relationship`, …; `string-enum` v `kc-base`)
5. Policy data jako instance (`AllowedRelationship`, enum hodnoty, `exchange-spec`)
6. Resoluce přes `iriLocal`, ne hardcoded Q/P

Struktura instancí:

```text
kc-base/            foundation (typing, enums mechanismus)
archimate-lite/     metamodel
platform/           sdílené služby (IdP, DNS)
sys-crm/            jeden systém = jeden package
```

Vzory z ArchiMate Lite přenes na vlastní doménu — nahraď třídy, properties a policy data, workflow zůstává stejné.

---

## Checklist před produkcí

- [ ] Metamodel má stabilní `iriLocal` pro všechny třídy a properties
- [ ] Klientský kód resolvuje `C*`/`P*` dynamicky, ne z konstant
- [ ] `instanceOfProperty` čten ze schema-config
- [ ] Instance packages mají `dependencies` na metamodel
- [ ] Auth policies definují role pro read/write (default deny)
- [ ] Zápisy používají `Idempotency-Key` pro retry-safe operace
- [ ] Metamodel publikován jako release pro promotion

Další reference: [api-contract.md](api-contract.md) · [examples.http](examples.http)
