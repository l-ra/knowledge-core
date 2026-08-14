# ArchiMate Lite — package a API pro nástroje

Tento dokument je kontrakt pro **samostatný nástroj** (export/import Open Exchange, validační CLI, editor), který pracuje **jen přes HTTP API knowledge-core**. Jádro KC neobsahuje ArchiMate typy; generické mezery P0–P2 jsou ve [fázi 19](../specs/phase-19-graph-api.md).

Rozsah modelování (L0–L4): [archimate-lite.md](archimate-lite.md).  
Datový katalog: [`models/archimate-lite/catalog.json`](../../models/archimate-lite/catalog.json).  
Nahrání metamodelu: [`models/archimate-lite/load.py`](../../models/archimate-lite/load.py).

## Hranice

| Vrstva | Kde | ArchiMate? |
|--------|-----|------------|
| knowledge-core | `internal/`, migrace, `/v1/*` | Ne. Obecný graf (Q/P/C, statement, package, shape, lens). |
| Package `archimate-lite` | data v KC | Ano. Třídy, vlastnosti, tvary. |
| Instance packages | data v KC | Ano. Konkrétní systémy, sdílené služby, views. |
| Exchange XML nástroj | mimo KC | Ano. Čte/zapisuje `/v1`, emituje ArchiMate XML. |

Stabilní identita ve slovníku je **`iriLocal`** (např. `ApplicationComponent`), ne `C12` / `P7`. Public ID se liší mezi instalacemi.

Kanonické IRI = `package.iriBase` + `iriLocal`.  
Default `iriBase`: `https://knowledge-core.local/archimate-lite/` (není well-known RDF namespace jádra).

## Autentizace

Všechny cesty pod `/v1` vyžadují identitu ([post-install](../ops/post-install.md)).

- Bootstrap: `Authorization: Bearer <heslo>` nebo `X-Admin-Password`
- Dev: `X-Subject` + `X-Roles: admin`
- OIDC: `Authorization: Bearer <access_token>`

Zápisy: `Content-Type: application/json`. Volitelně `X-Validation-Mode: relaxed` (default) / `strict` / `off`.  
Odpovědi create: `{ "data": { ... }, "changeSet": { ... } }`. GET entity/package obvykle **bez** obálky `data`.

## Bootstrap metamodelu

```bash
export KC_BASE_URL=http://localhost:8080
export KC_TOKEN='<bootstrap-or-oidc>'
python3 models/archimate-lite/load.py
```

Loader je idempotentní (`iriLocal` v package, `code` u shapes). Existující properties znovu zapíše constraints (`PATCH`, včetně `rangeClasses`). Tvary vytváří s `packageCode=archimate-lite`.

Po nahrání:

1. `GET /v1/packages/archimate-lite`
2. `GET /v1/entities?package=archimate-lite&iriLocal=ApplicationComponent` — resoluce slovníku
3. `GET /v1/classes/{cid}` / `GET /v1/properties/{pid}` — obsahují `iriLocal`, `iri`, `packageCode`
4. `GET /v1/admin/schema-config` — `instanceOfProperty` musí být neprázdné. Loader ho nastaví, jen pokud bylo prázdné (`iriLocal=instanceOf` v tomto package).
5. `GET /v1/shapes?package=archimate-lite` — `aml-element`, `aml-relationship`, `aml-view-connection`

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

`GET /v1/entities/{id}` funguje pro Q/P/C a vrací `effectiveClasses` (instanceOf + předci).  
`GET /v1/classes/{cid}` vrací `iriLocal`, `iri`, `packageCode`, `effectiveClasses` (self + předci).  
`GET /v1/properties/{pid}` vrací `iriLocal`, `iri`, `packageCode`.

Tvary patří do package (a do release bundle, `objectType: "shape"`). Kódy: `aml-element`, `aml-relationship`, `aml-view-connection`. `document.requiredProperties` jsou `P*` dané instalace.

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
PUT /v1/entities/{qid}/iri-aliases
```

```json
{ "aliases": [ { "iri": "https://archi.example/id-1234", "kind": "imported" } ] }
```

`kind`: `imported` | `sameAs` | `canonical_export`. IRI musí být absolutní `http(s)`.

Přesun do jiného package:

```http
POST /v1/entities/{id}/move
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
  "subject": "Q10",
  "property": "P1",
  "value": { "type": "EntityReference", "entityId": "C22" }
}
```

`P1` = `instanceOfProperty` ze schema-config, `C22` = třída `ApplicationComponent` v této instalaci.

Opakovaný zápis stejné trojice (subject + property + value) s `"upsert": true` vrátí existující statement (`200`), bez duplicity.

Hodnoty statementů:

| datatype | JSON value |
|----------|------------|
| EntityReference | `{ "type": "EntityReference", "entityId": "Q…" }` |
| String | `{ "type": "String", "string": "…" }` |
| Integer | `{ "type": "Integer", "int64": 5432 }` |
| Boolean | `{ "type": "Boolean", "bool": true }` |
| URI | `{ "type": "URI", "uri": "https://…" }` |

### Čtení grafu

| Účel | Endpoint |
|------|----------|
| Odchozí tvrzení | `GET /v1/entities/{qid}/statements?property=P*` |
| Příchozí tvrzení (kdo ukazuje na X) | `GET /v1/entities/{qid}/incoming?property=P*` |
| Okolí (outgoing + incoming + neighbors) | `GET /v1/entities/{qid}/graph?depth=1` (`depth` 1 nebo 2) |
| Instance třídy | `GET /v1/entities?instanceOf=C*&includeSubclasses=true` |

Dopadová analýza: `GET .../incoming` s property `relSource` nebo `relTarget` (public id z resoluce slovníku). RDF dump (`GET /v1/projections/rdf`) zůstává volitelný.

## Vzory objektů

### Prvek

`Q*` + `instanceOf` → listová třída (`ApplicationComponent`, `Node`, …).  
Atributy = další statementy (`modelingDepth`, `criticality`, …).  
Popis = `descriptions` na entitě, ne statement.

`modelingDepth`: `catalog` | `dr_minimum` | `application_detail` | `infrastructure_detail` | `network_detail` (L0–L4). Je to vlastnost **prvku**, ne package.

### Vazba (first-class entita)

`Q*` + `instanceOf` → `Composition` / `Flow` / …  
Povinné: `relSource`, `relTarget` (EntityReference na prvky).  
Identita vazby = `Q*` (v XML `relationship/@identifier`).

```text
Q:crm  instanceOf ApplicationComponent
Q:fe   instanceOf ApplicationComponent
Q:c1   instanceOf Composition
Q:c1   relSource → Q:crm
Q:c1   relTarget → Q:fe
```

`rangeClasses` na `relSource`/`relTarget` **nastavte** na `ArchiMateElement` (loader to dělá z catalog `range`). Validace range v KC bere `instanceOf` cílového `Q*` a expanduje `subClassOf`. Lite matici (`allowedRelationships` v catalog.json — konkrétní páry typů) vynucuje nástroj, ne jádro.

`PATCH /v1/properties/{pid}` `{ "constraints": { ... } }` upraví constraints po vytvoření.

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
| GET/PATCH | `/v1/entities/{id}` | včetně P*/C*; GET: `effectiveClasses` |
| POST | `/v1/entities/{id}/move` | `{ packageCode }` |
| PUT | `/v1/entities/{id}/iri-aliases` | Archi identifier |
| GET | `/v1/entities/{id}/statements` | odchozí; `?property=` |
| GET | `/v1/entities/{id}/incoming` | příchozí; `?property=` |
| GET | `/v1/entities/{id}/graph` | `?depth=1\|2` |
| POST | `/v1/statements` | atributy, instanceOf, relSource…; `"upsert": true` |
| GET | `/v1/statements/{sid}` | |
| POST | `/v1/statements/{sid}/revise` | změna hodnoty |
| GET/POST | `/v1/classes`, `/v1/properties`, `/v1/shapes` | metamodel |
| GET | `/v1/classes/{cid}`, `/v1/properties/{pid}` | `iriLocal`, `iri` |
| PATCH | `/v1/properties/{pid}` | `constraints` |
| GET | `/v1/shapes?package=` | tvary package |
| GET/PUT | `/v1/admin/schema-config` | `instanceOfProperty` |
| GET | `/v1/entities/{id}/validation` | shapes / domain / range |
| POST | `/v1/validation/reports` | dávka |
| GET | `/v1/projections/rdf` | dump pro analýzu mimo KC |
| POST | `/v1/packages/{code}/releases` | snapshot metamodelu (včetně shapes) |
| GET | `/v1/packages/{code}/releases/{version}/bundle` | přenositelný bundle |
| POST | `/v1/releases/import` | promotion bundle |

OpenAPI: [`api/openapi.yaml`](../../api/openapi.yaml) (0.2.0).

Lenses (`POST /v1/lenses`, `GET/PATCH .../instances/{key}`) jsou volitelné; nástroj může jít přímo na entity/statementy.

## Open Exchange — povinnosti nástroje (ne jádra)

- `Q*` prvek → `<element identifier xsi:type="{iriLocal}">`
- `Q*` vazba → `<relationship identifier xsi:type="{iriLocal}" source= target=>` (`relSource` / `relTarget`)
- literály P* → `<properties>`
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
Lite matice a „Flow jen mezi komponentami“ = logika nástroje nad `catalog.json` → `allowedRelationships`.

## Co nástroj nesmí dělat

- Přidávat ArchiMate typy do Go jádra, migrací nebo well-known RDF vocab
- Hardcodovat `C*` / `P*` z jiné instalace
- Modelovat L5 (pody, všechny IP, firewall rules) jako ArchiMate prvky
