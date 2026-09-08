# Zadání — Fáze 20: Open ChangeSet v DB (nahrazuje draft buffer)

**Status:** implemented

## Cíl

Zrušit per-user JSON draft (`user_changeset_draft` / `/v1/me/changeset-draft*`).  
Rozpracovaný ChangeSet žije v databázi jako **relativní delta** vůči committed stavu. Klient s nastaveným open ChangeSetem (nebo sadou open ChangeSetů) při čtení vidí už zapsané změny **bez vlastní lokální fronty ops**.

Committed historie zůstává: finální data = posloupnost **potvrzených** ChangeSetů. Potvrzení je jedna SQL transakce s optimistic lock; rebase v této fázi není.

## Kontext / nahrazuje

| Dnes (Fáze 16) | Nově (Fáze 20) |
|----------------|----------------|
| `user_changeset_draft` = JSON seznam `ChangeOperation` | Open `change_set` + materializovaný overlay |
| Čtení vždy jen committed | Čtení committed ⊕ opt-in overlay |
| Konflikt `expectedRevision` až při commit draftu | Exclusive claim při zápisu + revision check při commit |
| 1 draft na actora | Více open ChangeSetů na actora i globálně (disjunktní objekty) |

Související: [phase-16-package-changeset-ui.md](phase-16-package-changeset-ui.md) (draft část **obsolete** po dokončení této fáze), [phase-02-history.md](phase-02-history.md), [integration/concepts.md](../integration/concepts.md) (identita IRI).

---

## Identita (korekce vůči starším Q*/P*/C* textům)

SoT identity objektů grafu:

| Vrstva | Význam |
|--------|--------|
| **Interní ID** | UUID (`entity.id`, `statement.id`, …) — immutable, FK, claim key |
| **Veřejná identita** | plná **IRI** (kanonicky `iriBase` + `iriLocal`; package-root = `iriBase`) |

API path parametry mohou stále přijímat IRI (a případně legacy alias), ale **nový kód open ChangeSetu nesmí zavádět ani předpokládat alokaci spečiálních public ID tvaru `Q*` / `P*` / `C*` / `S*`**.  
Fallback `iriLocal` (`e_<snowflake>` atd.) zůstává podle stávajícího create path — to není Wikibase-style public id, jen generovaný local segment.

ChangeSet: interní UUID; stávající `change_set.public_id` (pokud ještě existuje v API) slouží jen jako handle v URL — není IRI objektu grafu.

**Odkud byla chybná informace v návrhu:** starší jádrové texty (`docs/concepts/overview.md`, Fáze 17) stále popisují `Q*`/`P*`/`C*` jako veřejné ID. Integrační model už je IRI-first ([integration/concepts.md](../integration/concepts.md)). Tato fáze se drží IRI + UUID.

---

## Designová rozhodnutí (schváleno)

| ID | Rozhodnutí |
|----|------------|
| D1 | Baseline **lazy per-object**: při prvním zápisu na objekt do open CS uložit `base_revision_no` (= committed `current_revision_no`; `0` = create). |
| D2 | Konflikt mezi open CS: **exclusive claim** na objekt (UUID) při zápisu → 409. Commit znovu ověří claim + revision. |
| D3 | Čtení **opt-in** přes header `X-Knowledge-Changesets`; zápis cílí na jeden CS přes `X-Knowledge-Changeset`. |
| D4 | Při create v open CS **ihned** alokovat interní **UUID** a veřejnou **IRI** (stejná pravidla jako ordinary create: `iriLocal` / fallback snowflake + package `iriBase`). Žádné `Q*`/`S*` counters pro nový tok. |
| D5 | Projekce / search / RDF **ignorují** open overlay (jen committed). |
| D6 | Po conflict na commit CS zůstane **open** (overlay beze změny); klient cancelne nebo opraví data jinak. Rebase = out. |
| D7 | Qualifiers (a references) v open CS: **JSON blob** v statement overlay. |
| D8 | Default bez open CS beze změny: 1 mutace = 1 committed ChangeSet. |
| D9 | Více open CS na actora **povoleno**; zápis/commit/cancel jen owner (actor) nebo admin. |
| D10 | `change_set_item` řádky materializovat **až při commit** z overlay; open fáze = overlay + claimy. |
| D11 | **Souběh s committed tokem:** po `open` smí committed svět volně pokračovat (jiné immediate ChangeSety i commity jiných open CS). Open CS **neizoluje** DB a **nesleduje** globální watermark. Konflikt se vyhodnocuje **jen vůči claimnutým objektům** tohoto CS (D1): pokud při `commit` žádný claim nemá stale revision / IRI srážku s committed, CS se **zařadí jako další** do běžné posloupnosti committed ChangeSetů (`status=committed`, revisions + items jako u ordinary write). Jinak commit selže **409** a CS **zůstane open** (D6). Změny committed dat mimo claimnuté objekty commit **neblokují**. |

---

## Scope

### In

- `change_set.status`: `open` \| `committed` \| `cancelled`
- Tabulky overlay + `changeset_object_claim`
- `POST /v1/changesets/open`, `…/commit`, `…/cancel`
- Write path: při `X-Knowledge-Changeset` zapisovat do overlay (ne do committed current)
- Read path: při `X-Knowledge-Changesets` merge committed ⊕ overlay
- Exclusive claim + commit-time optimistic lock
- Zrušení `user_changeset_draft` + `/v1/me/changeset-draft*`
- UI: aktivní open CS místo JSON draft bufferu
- OpenAPI + integration docs (headers, lifecycle)
- Testy (viz Acceptance)

### Out

- Rebase open CS na nový baseline
- Revert / reorder jednotlivých ops uvnitř open CS (jen cancel celého CS)
- Globální DB watermark při open (v1.1 kandidát)
- Open overlay ve search / RDF / projections
- Package/release lifecycle, RDF import uvnitř open CS (tyto zůstávají immediate committed, nebo 400 pokud header přítomen)
- Multi-CS write v jednom requestu (jeden target CS)

---

## Datový model

### `change_set` rozšíření

```text
status        TEXT NOT NULL  -- open | committed | cancelled
                             -- existující řádky → committed
opened_at     TIMESTAMPTZ    -- NOT NULL pro status=open
committed_at  TIMESTAMPTZ    -- NULL dokud open/cancelled
```

List API: default filtr `status=committed` (zpětná kompatibilita); `?status=open` pro rozpracované.

### `changeset_object_claim`

```text
changeset_id      UUID FK → change_set
object_type       TEXT     -- entity | statement
object_id         UUID     -- interní ID (u create = UUID přidělené do overlay)
canonical_iri     TEXT     -- denormalizovaná veřejná IRI (pro diagnostiku / IRI conflict)
base_revision_no  INT      -- committed revision při claim; 0 = objekt v committed neexistoval
op_kind           TEXT     -- create | update | deprecate | delete
```

Constraint:

- Partial **UNIQUE (`object_id`)** WHERE příslušný `change_set.status = 'open'`
- Partial **UNIQUE (`canonical_iri`)** WHERE open a `canonical_iri <> ''` — srážka IRI mezi open CS (a kontrola proti committed při zápisu)

### Overlay

`changeset_entity_overlay` — snapshot entitních polí potřebných pro merge/commit  
(status, labels, descriptions, package_id, iri_local, property/class profile payload, …)  
PK / UK: `(changeset_id, object_id)`

`changeset_statement_overlay` — value columns + `qualifiers_json` / `references_json`  
PK / UK: `(changeset_id, object_id)`

Cancel = DELETE overlay + claimy, `status=cancelled`.  
Commit = apply + DELETE overlay + claimy, `status=committed`.

---

## Semantika

### Open

`POST /v1/changesets/open`

```json
{ "comment": "optional", "operationType": "optional" }
```

→ `{ "changeSet": { "id", "status": "open", "actor", "openedAt", … } }`

Žádný celý snapshot DB. Actor = volající.

### Write (header `X-Knowledge-Changeset: <cs-id>`)

Platí pro **grafové** mutace:

| Podporováno (overlay) | S headerem **400** `unsupported_in_open_changeset` |
|------------------------|-----------------------------------------------------|
| Entity/statement create, update/revise, deprecate, delete | Packages CRUD |
| Property/class create; **PATCH property** (constraints) | Releases publish/mutate/import |
| **PUT iri-aliases** | RDF import |
| **POST …/move** | Shapes / lens definitions create; schema-config |
| | Batch `POST /v1/changesets`; reference create |

Open/commit/cancel CS = lifecycle (header se na ně nepoužívá jako cíl zápisu).

V transakci:

1. Ověřit CS existuje, `status=open`, actor smí zapisovat
2. Vyřešit cílový objekt (UUID / IRI)
3. **Claim** — konflikt jiný open CS nebo IRI kolize → **409**
4. Při prvním claimu na existující committed objekt uložit `base_revision_no = current_revision_no`
5. Upsert overlay (create: nový UUID + IRI dle create pravidel); u aliasů `iri_aliases_json` (replace-all)
6. Committed `entity` / `statement` / `statement_current` **neměnit**

Bez headeru: chování jako dnes (okamžitý committed ChangeSet).
**Nesmí** dojít k tichému bypassu — pokud operace overlay neumí a header je přítomen → **400**, ne committed write.

### Read (header `X-Knowledge-Changesets: <id>[,<id>…]`)

- Bez headeru: jen committed (jako dnes)
- S headerem: výsledek = committed stav **přepsaný** overlay řádky ze jmenovaných open CS
  - delete/deprecate v overlay musí být ve výsledku vidět (zmizet ze seznamů / status)
  - create jen v overlay musí být ve výsledku vidět
- Pokud dva jmenované CS claimují stejný `object_id` → 400 (nemělo nastat při D2)
- Neznámé / non-open ID v headeru → 400

Dotčené read cesty v1 (minimum): GET entity, list entities, GET/list statements, incoming, graph neighborhood, GET property/class.  
Search/RDF/projection: **bez** merge (D5).

### Cancel

`POST /v1/changesets/{id}/cancel`

→ smaže overlay + claimy, `status=cancelled`. Idempotentní, pokud už cancelled.

### Souběh open CS × committed změny (D11)

Po otevření ChangeSetu **neprobíhá freeze** committed dat. Jiné zápisy (bez open-CS headeru nebo commit jiného open CS) normálně přidávají další committed ChangeSety.

| Situace při `commit` | Výsledek |
|----------------------|----------|
| Žádný claimnutý objekt neměnil committed revision od claimu (a create nemá IRI/UUID kolizi) | CS se **commitne** = další prvek v běžném committed toku |
| Alespoň jeden claim má `current_revision_no ≠ base_revision_no`, nebo create koliduje s nově committed objektem stejné IRI/UUID | **409**, CS **zůstane open** |
| Committed změna jen na objektech, které tento CS **neclaimnul** | Commit **projde** (nezajímá nás) |

Open CS tedy není větev s rebase; je to delta s optimistic lock na sadě claimů. Úspěšný commit = append do lineární committed historie.

### Commit

`POST /v1/changesets/{id}/commit`

Jedna transakce:

1. Lock claimů `FOR UPDATE`
2. Pro každý claim (definice „konfliktu“ vůči committed):
   - `create` (`base_revision_no=0`): objekt nesmí existovat v committed pod stejným UUID/IRI
   - jinak: `current_revision_no == base_revision_no`
3. Při úspěchu všech checků: apply overlay → committed current + `*_revision` + `statement_current` (CS se stává dalším committed ChangeSetem v historii)
4. Zapsat `change_set_item` z aplikovaných změn
5. `status=committed`, `committed_at=now()`
6. Smazat overlay + claimy
7. Outbox / projekční eventy jako u dnešního finalize

Při selhání revision/IRI → **409**, CS zůstane `open`, overlay beze změny (D6, D11). Rebase / auto-merge s novým committed stavem v této fázi není.

---

## API

| Metoda | Cesta | Poznámka |
|--------|-------|----------|
| `POST` | `/v1/changesets/open` | Nový open CS |
| `POST` | `/v1/changesets/{id}/commit` | Potvrzení |
| `POST` | `/v1/changesets/{id}/cancel` | Zrušení |
| `GET` | `/v1/changesets` | `status`, stávající filtry |
| `GET` | `/v1/changesets/{id}` | Včetně `status`; u open volitelně souhrn claimů |
| `POST` | `/v1/changesets` | Okamžitý committed batch (beze změny); **ne** zápis do open CS |

**Headers**

| Header | Směr | Význam |
|--------|------|--------|
| `X-Knowledge-Changeset` | write | Jeden open CS — cíl zápisu |
| `X-Knowledge-Changesets` | read | Čárkou oddělené open CS — merge do odpovědi |

**Odstranit**

- Tabulka `user_changeset_draft`
- `GET/PUT/DELETE /v1/me/changeset-draft`, `POST /v1/me/changeset-draft/commit`

---

## UI

- Místo Open draft / queue / Complete: **Open ChangeSet** → kontext drží CS id → všechny write i reloady posílají headers → **Commit** / **Cancel**
- Po create v open CS reload seznamu/detailu hned ukáže objekt (v kontextu CS)
- ChangesetsPage: filtr `open` / `committed` / `cancelled`
- Odstranit client-side op buffer (`queueOp` / PUT draft document)

---

## Migrace a deliverables

- [x] Migrace: `change_set.status|opened_at`, claim + overlay tabulky; drop `user_changeset_draft`
- [x] Store: open/commit/cancel; write větev overlay; read merge helper
- [x] Engine + authz (owner/admin)
- [x] HTTP handlery + OpenAPI
- [x] Smazat draft handlery / `internal/store/draft.go`
- [x] UI `changeset.tsx` + Layout + i18n
- [x] Docs: `integration/api-contract.md`; poznámka v `concepts/data-model.md`
- [x] Acceptance testy (vyžadují `KC_DATABASE_URL`)

---

## Acceptance

| # | Scénář | Očekávání |
|---|--------|-----------|
| 1 | Open → create entity (s header) → GET bez headeru | Entita **není** vidět |
| 2 | Stejný create → GET s `X-Knowledge-Changesets` | Entita **je** vidět (UUID + IRI) |
| 3 | Commit → GET bez headeru | Entita je committed; CS `committed`; overlay prázdný |
| 4 | Open → write → Cancel → GET s headerem | Entita zmizí; CS `cancelled` |
| 5 | CS-A claimne entitu; CS-B zapíše tutéž | **409** při zápisu B |
| 6 | Open → claim entity rev=N; jiný committed write na entitu; Commit | **409**; CS zůstane open |
| 7 | Bez headeru create | Okamžitý committed CS (regrese) |
| 8 | Dva open CS na stejného actora, různé objekty | OK |
| 9 | Draft endpoints | **404** / odstraněny |
| 10 | Commit je atomický | Partial apply nepřipadá v úvahu (fail → overlay intact) |

---

## Poznámky k implementaci

- Merge na list/query: preferovat SQL `(committed EXCEPT claimed) UNION overlay` nebo ekvivalent v Go po načtení claim setu pro aktivní CS — držet jednoduchost, optimalizace později.
- IRI unresolved / package bez `iriBase`: stejná fallback pravidla jako ordinary create (`urn:kc:…` / snowflake local).
- `Idempotency-Key`: pro zápisy do open CS v1 stačí ordinary request semantics (bez replay body na change_set), nebo reuse stávajícího mechanismu jen pro commit — zvolit jednodušší: **idempotency povinné jen u commit a u immediate writes**; open-CS writes bez idempotency replay.
- Phase-16 UI acceptance „draft queue → commit“ nahradit scénáři 1–4 výše.
