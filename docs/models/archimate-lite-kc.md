# ArchiMate Lite — package a API pro nástroje

> Obecný integrační návod: [integration/client-guide.md](../integration/client-guide.md).  
> Tento dokument je **doménově specifický kontrakt** pro nástroje nad ArchiMate Lite.

Tento dokument je kontrakt pro **samostatný nástroj** (export/import Open Exchange, validační CLI, editor), který pracuje **jen přes HTTP API knowledge-core**. Jádro KC neobsahuje ArchiMate typy; generické mezery P0–P2 jsou ve [fázi 19](../specs/phase-19-graph-api.md).

Rozsah modelování (L0–L4): [archimate-lite.md](archimate-lite.md).  
Datový katalog (seed v gitu): [`models/archimate-lite/catalog.json`](../../models/archimate-lite/catalog.json).  
Release bundle (UI import): [`models/archimate-lite/releases/archimate-lite-1.2.0.bundle.json`](../../models/archimate-lite/releases/archimate-lite-1.2.0.bundle.json).  
Nahrání přes API: [`models/archimate-lite/load.py`](../../models/archimate-lite/load.py). Po nahrání/importu je **zdroj pravdy pro tool package `archimate-lite` v KC**, ne JSON v gitu.

## Hranice

| Vrstva | Kde | ArchiMate? |
|--------|-----|------------|
| knowledge-core | `internal/`, migrace, `/v1/*` | Ne. Obecný graf (IRI entity/property/class, statement, package, shape, lens). |
| Package `archimate-lite` | data v KC | Ano. Třídy, vlastnosti, tvary, anotace tříd, lite matice, enumy, exchange poznámky. |
| Instance packages | data v KC | Ano. Konkrétní systémy, sdílené služby, views. |
| Exchange XML nástroj | mimo KC | Ano. Čte/zapisuje `/v1`, emituje ArchiMate XML. |

**Stabilní public ID** = plná IRI (`iriBase` + `iriLocal`), např. `https://knowledge-core.local/archimate-lite/ApplicationComponent`.  
Krátké zobrazení v UI: `archimate-lite:ApplicationComponent`. Fallback bez zadaného `iriLocal`: `e_<snowflake>` / `p_<snowflake>` / …

Default `iriBase`: `https://knowledge-core.local/archimate-lite/` (není well-known RDF namespace jádra).

## Autentizace

Všechny cesty pod `/v1` vyžadují identitu ([post-install](../ops/post-install.md)).

- Bootstrap: `Authorization: Bearer <heslo>` nebo `X-Admin-Password`
- Dev: `X-Subject` + `X-Roles: admin`
- OIDC: `Authorization: Bearer <access_token>`

Zápisy: `Content-Type: application/json`. Volitelně `X-Validation-Mode: relaxed` (default) / `strict` / `off`.  
Odpovědi create: `{ "data": { ... }, "changeSet": { ... } }`. GET entity/package obvykle **bez** obálky `data`.

## Bootstrap metamodelu

### Import release bundle (UI / promotion)

```bash
# soubor: models/archimate-lite/releases/archimate-lite-1.2.0.bundle.json
# UI: Packages → Import release bundle
# nebo:
curl -X POST "$KC_BASE_URL/v1/releases/import" \
  -H "Authorization: Bearer $KC_TOKEN" \
  -H "Content-Type: application/json" \
  --data-binary @models/archimate-lite/releases/archimate-lite-1.2.0.bundle.json
```

Bundle obsahuje třídy, properties (včetně constraints), shapes, policy entity a statementy.  
Po importu: pokud je `instanceOfProperty` v schema-config prázdné, nastavte ho na  
`https://knowledge-core.local/archimate-lite/instanceOf`.

Přegenerování: `python3 models/archimate-lite/build_bundle.py`.

### Load přes API (authoring)

```bash
export KC_BASE_URL=http://localhost:8080
export KC_TOKEN='<bootstrap-or-oidc>'
python3 models/archimate-lite/load.py
```

Loader je idempotentní (`iriLocal` v package, `code` u shapes, `upsert` u policy statementů). Existující properties znovu zapíše constraints (`PATCH`, včetně `rangeClasses`). Tvary vytváří s `packageCode=archimate-lite`. Druhá vrstva catalogu (matice, enumy, exchange, `layer`/`overlay`/`exchangeType`) se uloží jako data v tomtéž package.

Po nahrání:

1. `GET /v1/packages/archimate-lite`
2. `GET /v1/entities?package=archimate-lite&iriLocal=ApplicationComponent` — resoluce slovníku
3. `GET /v1/classes/{id}` / `GET /v1/properties/{id}` — id je plná IRI (v URL path-encode); obsahují `iriLocal`, `iri`, `packageCode`
4. `GET /v1/admin/schema-config` — `instanceOfProperty` musí být neprázdné. Loader ho nastaví, jen pokud bylo prázdné (`iriLocal=instanceOf` v tomto package).
5. `GET /v1/shapes?package=archimate-lite` — `aml-element`, `aml-relationship`, `aml-view-connection`, `aml-allowed-relationship`, `aml-string-enum`
6. Policy data viz [níže](#policy-data-v-kc) (`AllowedRelationship`, `StringEnum`, `exchange-spec`)

`instanceOf` **není** ArchiMate predikát. Je to globální typing KC. Nástroj ho vždy čte ze schema-config, nikdy ho nehardcoduje.

## Resoluce slovníku

Přímý lookup (preferovaný):

```text
GET /v1/entities?package=archimate-lite&iriLocal=relSource
GET /v1/entities?iri=https://knowledge-core.local/archimate-lite/ApplicationComponent
GET /v1/entities?package=archimate-lite&kind=class&limit=200
GET /v1/entities?package=archimate-lite&kind=property&limit=200
```

`iri` matchuje kanonické IRI (`iriBase` + `iriLocal`) nebo `entity_iri_alias`.  
Stránkování: `nextCursor` → `?cursor=`.

`GET /v1/entities/{id}` — `{id}` je plná IRI (path-encoded). Vrací `effectiveClasses` (instanceOf + předci).  
`GET /v1/classes/{id}` vrací `iriLocal`, `iri`, `packageCode`, `effectiveClasses` (self + předci).  
`GET /v1/properties/{id}` vrací `iriLocal`, `iri`, `packageCode`.

Tvary patří do package (a do release bundle, `objectType: "shape"`). Kódy: `aml-element`, `aml-relationship`, `aml-view-connection`, `aml-allowed-relationship`, `aml-string-enum`. `document.requiredProperties` jsou IRI properties.

## Policy data v KC

KC matici a enumy **nevynucuje**. Tool si je načte z package a implementuje vlastní kontroly / XML mapování.

Public ID jsou stabilní IRI; pro lookup stačí `iriLocal` (ne hardcoduj cizí instalaci).

### Anotace tříd

Statementy na třídě prvku nebo vazby:

| `iriLocal` property | Význam |
|---------------------|--------|
| `archiLayer` | `business` / `application` / `technology` / … |
| `overlay` | např. `risk-and-security` u `Risk` |
| `exchangeType` | Open Exchange `xsi:type`; chybí u `DeployedOn` |
| `usageGuidance` | textový návod, kdy a jak prvek/vazbu/property použít |
| `usageExamples` | konkrétní příklady použití |

```text
GET /v1/entities?package=archimate-lite&iriLocal=ApplicationComponent
GET /v1/entities/{classIri}/statements?property={archiLayerIri}
GET /v1/entities/{classOrPropertyIri}/statements?property={usageGuidanceIri}
```

`usageGuidance` / `usageExamples` jsou i na **property** entitách (nejen na třídách prvků a vazeb).

### Lite matice vazeb

Instance třídy `AllowedRelationship` (`iriLocal` tvaru `allowed/{type}/{source}/{target}`):

| property | hodnota |
|----------|---------|
| `allowedRelType` | IRI class podtřídy `ArchiMateRelationship` |
| `allowedSourceClass` | IRI class podtřídy `ArchiMateElement` |
| `allowedTargetClass` | IRI class podtřídy `ArchiMateElement` |

```text
GET /v1/entities?package=archimate-lite&instanceOf={AllowedRelationshipIri}
GET /v1/entities/{entityIri}/statements
```

### Enumy

Instance `StringEnum` (`iriLocal` `enum/{propertyIriLocal}`):

- `enumeratesProperty` → IRI property
- `allowedValue` — n× String

```text
GET /v1/entities?package=archimate-lite&iriLocal=enum/modelingDepth
```

### Exchange poznámky

Jedna entita `iriLocal=exchange-spec` (`instanceOf` `ExchangeSpec`): `catalogVersion`, `exchangeFormat`, `exchangeElementXsiType`, `exchangeRelationshipXsiType`, `exchangeDeployedOn`, `exchangeRisk`, `exchangeViews`, `exchangeIdentifier`.

```text
GET /v1/entities?package=archimate-lite&iriLocal=exchange-spec
```

`catalog.json` / release bundle v gitu jsou seed. Runtime tool **nečte** JSON z disku, pokud má přístup k nahranému package.
## Konvence instancí

| Package | Účel |
|---------|------|
| `archimate-lite` | Jen metamodel |
| např. `platform`, `network`, `locations` | Sdílené závislosti (IdP, DNS, DC, WAN) |
| jeden package na evidovaný systém | L0 `ApplicationComponent` = systém jako celek |
| volitelně landscape package | `DiagramView` napříč systémy |

Založení systémového package (závislost na metamodelu). JSON tagy jsou camelCase:

```http
POST /v1/packages
```

```json
{
  "code": "sys-crm",
  "lifecycle": "continuous",
  "iriBase": "https://example.org/systems/crm/",
  "labels": { "en": "CRM" },
  "dependencies": [
    { "dependsOnCode": "archimate-lite", "versionRange": "*" }
  ]
}
```

GET package vrací závislosti stejně (`dependsOnCode` / `versionRange`). Cross-package odkazy (Keycloak z `platform`) jsou v KC povolené.

`iriLocal` instance: stabilní slug (`crm`, `crm-backend`). Pro round-trip z Archi ulož původní identifier:

```http
PUT /v1/entities/{entityIri}/iri-aliases
```

```json
{ "aliases": [ { "iri": "https://archi.example/id-1234", "kind": "imported" } ] }
```

`kind`: `imported` | `sameAs` | `canonical_export`. IRI musí být absolutní `http(s)`.

Přesun do jiného package:

```http
POST /v1/entities/{entityIri}/move
{ "packageCode": "sys-crm" }
```

Přesune entitu a statementy subjektu, které byly ve stejném starém package. `409` při konfliktu `iriLocal` v cíli.

## Typing a statementy

Každý prvek, vazba i view uzel:

```http
POST /v1/entities
POST /v1/statements
```

```json
{
  "packageCode": "sys-crm",
  "labels": { "en": "CRM" },
  "iriLocal": "crm"
}
```

```json
{
  "packageCode": "sys-crm",
  "subject": "https://example.org/systems/crm/crm",
  "property": "https://knowledge-core.local/archimate-lite/instanceOf",
  "value": {
    "type": "EntityReference",
    "entityId": "https://knowledge-core.local/archimate-lite/ApplicationComponent"
  }
}
```

`property` = `instanceOfProperty` ze schema-config (typicky IRI `…/instanceOf`), `entityId` = IRI class `ApplicationComponent`.

Opakovaný zápis stejné trojice (subject + property + value) s `"upsert": true` vrátí existující statement (`200`), bez duplicity.

Hodnoty statementů:

| datatype | JSON value |
|----------|------------|
| EntityReference | `{ "type": "EntityReference", "entityId": "<IRI>" }` |
| String | `{ "type": "String", "string": "…" }` |
| Integer | `{ "type": "Integer", "int64": 5432 }` |
| Boolean | `{ "type": "Boolean", "bool": true }` |
| URI | `{ "type": "URI", "uri": "https://…" }` |
### Čtení grafu

| Účel | Endpoint |
|------|----------|
| Odchozí tvrzení | `GET /v1/entities/{iri}/statements?property={propertyIri}` |
| Příchozí tvrzení (kdo ukazuje na X) | `GET /v1/entities/{iri}/incoming?property={propertyIri}` |
| Okolí (outgoing + incoming + neighbors) | `GET /v1/entities/{iri}/graph?depth=1` (`depth` 1 nebo 2) |
| Instance třídy | `GET /v1/entities?instanceOf={classIri}&includeSubclasses=true` |

Dopadová analýza: `GET .../incoming` s property `relSource` nebo `relTarget` (IRI z resoluce slovníku). RDF dump (`GET /v1/projections/rdf`) zůstává volitelný.

## Vzory objektů

### Prvek

Entita + `instanceOf` → listová třída (`ApplicationComponent`, `Node`, …).  
Atributy = další statementy (`modelingDepth`, `criticality`, …).  
Popis = `descriptions` na entitě, ne statement.

`modelingDepth`: `catalog` | `dr_minimum` | `application_detail` | `infrastructure_detail` | `network_detail` (L0–L4). Je to vlastnost **prvku**, ne package.

### Vazba (first-class entita)

Entita + `instanceOf` → `Composition` / `Flow` / …  
Povinné: `relSource`, `relTarget` (EntityReference na prvky).  
Identita vazby = IRI entity (v XML `relationship/@identifier` typicky z `iriLocal` nebo aliasu).

```text
…/crm  instanceOf ApplicationComponent
…/fe   instanceOf ApplicationComponent
…/c1   instanceOf Composition
…/c1   relSource → …/crm
…/c1   relTarget → …/fe
```

`rangeClasses` na `relSource`/`relTarget` **nastavte** na `ArchiMateElement` (loader/bundle to dělá z catalog `range`). Validace range v KC bere `instanceOf` cíle a expanduje `subClassOf`. Lite matici vynucuje tool nad instancemi `AllowedRelationship` v KC.

`PATCH /v1/properties/{propertyIri}` `{ "constraints": { ... } }` upraví constraints po vytvoření.

`DeployedOn` je lite vztah; XML nástroj ho expanduje na `Assignment`.  
`Risk` je overlay; XML: overlay typ nebo `Assessment` + property.

### View

Architektura není ve view. View jen vybírá a kreslí.

- `DiagramView` — `viewpoint` volitelně
- `ViewNode` — `nodeKind` (`element`|`container`|`label`), `elementRef` (range `ArchiMateElement`), `boundsX/Y/W/H`, `parentNode` (range `ViewNode`), `style` (JSON string)
- `ViewConnection` — `relationshipRef` (range `ArchiMateRelationship`), `sourceNode`/`targetNode` (range `ViewNode`), `bendpoints` (JSON string)

## Endpointy, které nástroj potřebuje

| Metoda | Cesta | Účel |
|--------|-------|------|
| GET | `/healthz` | liveness |
| GET/POST | `/v1/packages`, `/v1/packages/{code}` | packages (`dependsOnCode` / `versionRange`) |
| PATCH | `/v1/packages/{code}` | `iriBase`, labels |
| GET | `/v1/entities` | `?package=&kind=&iriLocal=&iri=&instanceOf=&includeSubclasses=&q=&cursor=&limit=` |
| POST | `/v1/entities` | prvek / vazba / view |
| GET/PATCH | `/v1/entities/{id}` | id = IRI (path-encoded); GET: `effectiveClasses` |
| POST | `/v1/entities/{id}/move` | `{ packageCode }` |
| PUT | `/v1/entities/{id}/iri-aliases` | Archi identifier |
| GET | `/v1/entities/{id}/statements` | odchozí; `?property=` |
| GET | `/v1/entities/{id}/incoming` | příchozí; `?property=` |
| GET | `/v1/entities/{id}/graph` | `?depth=1\|2` |
| POST | `/v1/statements` | atributy, instanceOf, relSource…; `"upsert": true` |
| GET | `/v1/statements/{id}` | |
| POST | `/v1/statements/{id}/revise` | změna hodnoty |
| GET/POST | `/v1/classes`, `/v1/properties`, `/v1/shapes` | metamodel |
| GET | `/v1/classes/{id}`, `/v1/properties/{id}` | `iriLocal`, `iri` |
| PATCH | `/v1/properties/{id}` | `constraints` |
| GET | `/v1/shapes?package=` | tvary package |
| GET/PUT | `/v1/admin/schema-config` | `instanceOfProperty` |
| GET | `/v1/entities/{id}/validation` | shapes / domain / range |
| POST | `/v1/validation/reports` | dávka |
| GET | `/v1/projections/rdf` | dump pro analýzu mimo KC |
| POST | `/v1/packages/{code}/releases` | snapshot metamodelu (včetně shapes) |
| GET | `/v1/packages/{code}/releases/{version}/bundle` | přenositelný bundle |
| POST | `/v1/releases/import` | promotion bundle |

OpenAPI: [`api/openapi.yaml`](../../api/openapi.yaml) (0.2.1).

Lenses (`POST /v1/lenses`, `GET/PATCH .../instances/{key}`) jsou volitelné; nástroj může jít přímo na entity/statementy.

## Open Exchange — povinnosti nástroje (ne jádra)

- prvek → `<element identifier xsi:type="{iriLocal}">`
- vazba → `<relationship identifier xsi:type="{iriLocal}" source= target=>` (`relSource` / `relTarget`)
- literály properties → `<properties>`
- `DeployedOn` → `Assignment` řetězec
- `Risk` → overlay nebo Assessment
- View* → `<views><diagrams><view>`
- `<organizations>` z packages
- RDF projekce KC **není** ArchiMate XML

## Validace

KC `relaxed`: zápis projde, findings v odpovědi / `GET .../validation`.  
Shape `aml-relationship` vyžaduje `relSource`+`relTarget` (error).  
Shape `aml-element` varuje bez `modelingDepth`.  
Range `ArchiMateElement` na koncích vazby = jádro.  
Lite matice a „Flow jen mezi komponentami“ = logika nástroje nad instancemi `AllowedRelationship` (seed v catalog `allowedRelationships`). Enumy = instance `StringEnum`.

## Co nástroj nesmí dělat

- Přidávat ArchiMate typy do Go jádra, migrací nebo well-known RDF vocab
- Hardcodovat IRI z jiné instalace s jiným `iriBase`
- Modelovat L5 (pody, všechny IP, firewall rules) jako ArchiMate prvky
