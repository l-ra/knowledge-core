# Zadání — Fáze 21: Unified batch writes (SoT) + REST wrappers

**Status:** implemented  

SoT zápisu batch/open je `ApplyChangeSet`. Grafové REST mutátory v engine jsou thin 1-op wrappers nad stejným entrypointem (committed i open overlay).
**Repo:** `knowledge-core`  
**Souvisí:** [phase-02-history.md](phase-02-history.md) (batch CS), [phase-20-open-changeset.md](phase-20-open-changeset.md) (open overlay), [phase-17-iri-rdf.md](phase-17-iri-rdf.md) (IRI aliases)  
**Klient follow-up:** IT Map — `docs/zadani-open-exchange-batch-import.md` (v `knowledge-itmap`)

---

## Cíl

1. **`PUT /v1/entities/{id}/iri-aliases` vždy tvoří ChangeSet** (committed nebo open overlay) — žádný tichý zápis mimo historii.
2. **Interní SoT zápisu = batch pipeline** (`operations[]`). Každý grafový zápis (včetně „jedné operace“) jde stejnou cestou.
3. **Veřejné REST mutátory zůstanou** jako **tenké wrappery** = sestaví batch o 1 op a zavolají stejný engine (žádná vlastní store logika).
4. **Batch limity** (hard) dle doporučení níže; dokumentovat + enforcement.
5. **Open ChangeSet:** `POST /v1/changesets` s headerem `X-Knowledge-Changeset` **appenduje do overlay** (ne 400); bez headeru = okamžitý committed CS (dnešní chování batch).
6. **Revize dokumentace** (api-contract, phase-02/20, OpenAPI, client-guide, ROADMAP).

---

## Kontext / problém

Dnes (před fází 21) existovaly **tři** write cesty:

| Cesta | Chování (tehdy) |
|-------|-----------------|
| REST mutátory bez headeru | committed write + nový CS |
| REST + `X-Knowledge-Changeset` | overlay (per endpoint) |
| `POST /v1/changesets` | atomický batch → hned committed; s open headerem **400** |

Důsledky: dual-path drift, chatty klienti (Open Exchange ~10³ HTTP), `PUT iri-aliases` bez CS mimo historii, batch nelze použít pro open CS.

**Verdikt (schválený směr):** API konceptuálně „vždy batch“; jednooperační batch je validní; REST zůstává DX wrapper. Po implementaci: jeden SoT (`ApplyChangeSet`), open batch appenduje overlay, REST = 1-op wrappers.
---

## Designová rozhodnutí (doporučená výchozí)

| ID | Rozhodnutí | Poznámka |
|----|------------|----------|
| D1 | **Jeden store entrypoint** `ApplyOperations(ctx, meta, ops)` | Committed i open; cíl určuje `meta.OpenChangeSetID` |
| D2 | REST wrapper = `ops: [{…}]` délky 1 | Stejná authz, idempotency, response `data` + `changeSet` |
| D3 | Open CS + batch = **append overlay**, CS zůstane `open` | Commit/cancel beze změny lifecycle |
| D4 | Hard limit **500 ops / request** | Soft doporučení klientům 200–500; OE chunkuje |
| D5 | Hard limit **body 4 MiB** | 413 při překročení |
| D6 | All-or-nothing **v rámci jednoho HTTP requestu** | Open CS: fail → žádná změna overlay z *této* dávky (Tx rollback); dřívější úspěšné dávky zůstanou |
| D7 | `setEntityIRIAliases` je řádná **batch op** + REST wrapper | Committed path musí volat `beginChangeSetTx` / `finalizeChangeSet` |
| D8 | Veřejné REST cesty **nezrušit** v této fázi | Deprecation timeline = out of scope (volitelné později) |
| D9 | Packages / releases / RDF / shapes / lens-create / schema-config **zůstávají mimo** batch graph ops | S open CS headerem dál 400 `unsupported_in_open_changeset` |

### Rozhodnutí (potvrzeno)

| # | Otázka | Rozhodnutí |
|---|--------|------------|
| Q1 | HTTP kód při překročení limitu ops | **400** `batch_limit_exceeded` (ne 413) — 413 jen na body size |
| Q2 | Response batch do open CS | `{ data: { results: [...] }, changeSet: { id, status: "open", … } }` bez materializace `items` jako u commit |
| Q3 | Idempotency u open-CS batch | Stejný `Idempotency-Key` + hash → replay response; klíč scoped na open CS id |

---

## Cílová architektura

```text
HTTP REST wrapper (1 op) ──┐
                           ├──► Engine.ApplyOperations(meta, ops)
HTTP POST /v1/changesets ──┘         │
                                     ├─ meta.OpenChangeSetID == "" → committed Tx (*InTx / unified)
                                     └─ meta.OpenChangeSetID != "" → overlay claim + upsert (open Tx)
```

**Zakázáno:** store metody volané přímo z HTTP handlerů mimo `ApplyOperations` / lifecycle open/commit/cancel / non-graph admin.

---

## Batch kontrakt

### Endpoint

`POST /v1/changesets`

```json
{
  "operationType": "openExchangeImport",
  "comment": "optional",
  "operations": [ /* 1..N */ ]
}
```

Headers:

| Header | Význam |
|--------|--------|
| *(none open)* | Okamžitý committed ChangeSet (1 CS, status committed) |
| `X-Knowledge-Changeset: <openId>` | Append ops do overlay daného open CS; status zůstane `open` |
| `Idempotency-Key` | Povinné doporučit u klientů; server podporuje jako dnes |

**Odstranit** gate `rejectIfOpenChangeSet` z `applyChangeSet` (nahradit open větví). Ostatní non-graph mutátory gate ponechat.

### Operations (minimální množina pro parity s REST grafem)

Existující (ponechat / sjednotit na ApplyOperations):

- `createEntity`, `updateEntity`, `deprecateEntity`, `deleteEntity`
- `createProperty`, `createClass`
- `createStatement`, `reviseStatement`, `deprecateStatement`

**Nové / doplnit:**

| `op` | Payload (hlavní pole) | Poznámka |
|------|----------------------|----------|
| `setEntityIRIAliases` | `entity`, `aliases: [{iri,kind}]` | Replace-all; validace jako dnes |
| `updateProperty` | `entity` (pid), `constraints`, `expectedRevision?` | Parita `PATCH /properties/{pid}` |
| `moveEntity` | `entity`, `packageCode`, `expectedRevision?` | Parita `POST …/move` |

`clientKey` / `$…` remapping: zachovat a dokumentovat (včetně alias/move subjectů).

### Limity (hard)

| Limit | Hodnota | Error |
|-------|---------|--------|
| `len(operations)` | **1..500** | 400 `batch_limit_exceeded` |
| Request body | **≤ 4 MiB** | 413 `payload_too_large` |
| Prázdné `operations` | zakázáno | 400 |

Dokumentovat doporučení: chunk **200–500** ops pro velké importy; po sérii chunků do open CS jeden `commit`.

### Chyby

- První fatální op → rollback celé dávky (request Tx).
- Response: stávající styl `error.code` + `message`; volitelně `error.opIndex` / `error.op` (doporučeno pro DX).
- Conflict claim / revision → 409 (open i committed).

---

## REST wrappers

Každý grafový mutátor:

1. Sestaví `[]ChangeOperation` délky 1.
2. Zavolá `ApplyOperations` se stejným `WriteMeta` (včetně open CS headeru a idempotency).
3. Namapuje `results[0]` zpět na dnešní response tvar (`data` = resource DTO, `changeSet`).

**Seznam wrapperů (min.):**

- `POST/PATCH` entities, deprecate/delete entity  
- `POST` properties, classes, statements; revise/deprecate statement  
- `PATCH` properties  
- `POST` entities/{id}/move  
- **`PUT` entities/{id}/iri-aliases** ← musí jít přes batch op `setEntityIRIAliases`

Response kompatibilita: klienti čekající `data` + `changeSet` u aliases **nově dostanou `changeSet`** (breaking jen ve smyslu „dříve chyběl“ — additive).

---

## Oprava `PUT …/iri-aliases` (samostatně testovatelný milník)

I před dokončením celého sjednocení:

1. Committed: `beginChangeSetTx` + `replaceEntityIRIAliasesTx` + revision/item + `finalizeChangeSet` + RDF project (outbox).
2. Open: stávající overlay větev (ponechat / přesunout pod ApplyOperations).
3. Acceptance: aliases bez headeru → CS v historii; s headerem → overlay + commit materializuje `entity_iri_alias`.

---

## Implementační pořadí (KC)

1. **Spec + OpenAPI draft** limitů a nových ops (tento dokument).
2. **Aliases → ChangeSet** (committed) + acceptance.
3. Extrahovat / zavést `ApplyOperations` (refaktor `ApplyChangeSet` + open větve z `*InOpenChangeSet`).
4. Napojit `POST /v1/changesets` na open overlay (zrušit 400).
5. Přepsat REST handlery na wrappers (postupně, začít aliases + entity/statement create).
6. Hard limity middleware / validace na vstupu batch.
7. Docs: api-contract, phase-02, phase-20 (write tabulka), client-guide, ROADMAP, OpenAPI.
8. Acceptance matice (níže) + regrese open CS ordering / OE-like bulk.

**Nedělat v této fázi:** mazání REST cest; package/RDF v batch; rebase; cross-CS alias sharing.

---

## Dokumentace (povinná revize)

| Dokument | Úprava |
|----------|--------|
| [api-contract.md](../integration/api-contract.md) | Batch jako SoT; REST = wrappers; limity; open+batch; aliases vždy CS |
| [phase-02-history.md](phase-02-history.md) | Odkaz na fázi 21; batch limity; open append |
| [phase-20-open-changeset.md](phase-20-open-changeset.md) | Update write tabulky: batch **podporován** do overlay; ne 400 |
| [phase-17-iri-rdf.md](phase-17-iri-rdf.md) | Aliases součást historie / CS |
| [client-guide.md](../integration/client-guide.md) | Příklad chunkovaného importu; jedno-op batch |
| `api/openapi.yaml` | Ops enum + limity + error codes |
| [ROADMAP.md](../ROADMAP.md) + [specs/README.md](README.md) | Fáze 21 planned/done |
| examples.http | Batch 1 op + open batch chunk |

---

## Akceptace (KC)

| ID | Scénář | Očekávání |
|----|--------|-----------|
| B1 | `PUT iri-aliases` bez open headeru | 200 + `changeSet.status=committed`; alias v DB; CS v historii |
| B2 | Open: create entity + `setEntityIRIAliases` op (REST nebo batch) + commit | Aliasy vidět před commit (read header) i po |
| B3 | `POST /v1/changesets` s 1× `createEntity` | Stejný výsledek jako `POST /v1/entities` (modulo ids) |
| B4 | `POST /v1/entities` po refaktoru | Stále funguje; interně batch 1 |
| B5 | `POST /v1/changesets` + `X-Knowledge-Changeset` + N createEntity | Overlay N entit; CS open; commit OK |
| B6 | Batch 501 ops | 400 `batch_limit_exceeded`; CS/overlay beze změny z requestu |
| B7 | Body > 4 MiB | 413 |
| B8 | Batch open: create statement odkazující na entity ze **stejné** dávky (`clientKey`) | OK |
| B9 | `POST /v1/packages` + open header | stále 400 unsupported |
| B10 | Regrese phase-20 ordering | entity before statement na commit |
| B11 | Idempotency committed batch | replay |
| B12 | Open batch fail uprostřed | žádné částečné claimy z daného requestu |

---

## Odhad náročnosti (orientační)

| Celek | Odhad |
|-------|-------|
| Aliases→CS + testy | 0.5–1 den |
| ApplyOperations + open batch | 3–6 dní |
| REST wrappers migrace | 2–4 dny |
| Limity + OpenAPI + docs | 1–2 dny |
| **Celkem KC** | **~1.5–2.5 týdne** |

---

## Follow-up mimo scope

- IT Map Open Exchange batch import — samostatné zadání v `knowledge-itmap`
- Volitelná deprecace REST mutátorů (Sunset header)
- Server-side XML import endpoint
