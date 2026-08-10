### User

Chtel bych pro svoje potreby navrhnout system logikou podobny wikibazi - tedy v podstate triplety s dodatecnymi daty property. Rad bych mel verzovani, provenance a historii a zaroven lehky model s vybranym rezem dat pro dotazovani. Nad modelem bych rad me UI ve stylu zakladni wikibase a zaroven pohledy pro custom editace dat jako "mikroaplikace" pro konkretni struktury. Wikibase mi prijde slozita na provoz, proto bych rad vlastni system, ktery by byl all-in-one.  Posud kriticky tuto ideu a rekni mi, zda bys doporucil pouzit primo wikibazi.

### Assistant

Assistant included image: GitHub - 

Za těchto požadavků bych**Wikibase přímo nepoužil jako základ systému**. Použil bych ji ale velmi výrazně jako inspiraci pro datový model. Podle mě máš docela dobrý případ pro vlastní „Wikibase-lite“ nad PostgreSQL.

### Kde přesně je problém s Wikibase

Wikibase řeší skoro přesně správnou abstrakci. Není to ve skutečnosti jen trojice`subject–predicate–object`; statement je samostatný objekt, ke kterému patří qualifiers, references a rank. References jsou přímo určené k zachycení provenance tvrzení.

Například:
```
Q123 -- P17 --> Q213
        |
        +-- qualifier P580 = 1993-01-01
        +-- qualifier P582 = 2004-05-01
        +-- reference = dokument XYZ
```

To je podle mě**mnohem lepší model než čistá RDF triple tabulka**.

Jenže Wikibase je historicky komponenta MediaWiki. Oficiální dokumentace přímo říká, že bez MediaWiki Wikibase nemůže fungovat a entity jsou ukládány jako stránky MediaWiki.To je pro systém, jehož primární účel není wiki, dost významný architectural baggage.

A pokud chceš query vrstvu podobnou Wikidata, přidáváš další synchronizovaný datastore. Aktuální Wikibase Suite používá samostatný SPARQL Query Service, updater a volitelně Elasticsearch. Oficiální Docker Compose dnes obsahuje vedle samotného Wikibase také MariaDB, jobrunner, Blazegraph/WDQS updater, Elasticsearch, QuickStatements a Traefik; dokumentace uvádí minimum 8 GB RAM pro podporovaný deployment.

Takže místo:
```
app
 └── PostgreSQL
```

se dostáváš někam k:
```
MediaWiki/Wikibase
 ├── MariaDB
 ├── jobrunner
 ├── Elasticsearch
 ├── WDQS / Blazegraph
 ├── WDQS updater
 ├── QuickStatements
 └── proxy
```

Pro velký veřejný collaborative knowledge graph je to obhajitelné. Pro osobní nebo interní knowledge model je to podle mě zbytečně těžké.

## Co bych postavil místo toho

Základ bych udělal jako**statement store nad PostgreSQL**, ne jako klasický RDF triplestore.

Klíčové je především to, že`statement`musí být first-class objekt:
```
Entity
  id
  type
  label
  description

Property
  id
  datatype
  ...

Statement
  id
  subject_id
  property_id
  value
  rank/status

Qualifier
  statement_id
  property_id
  value

Reference
  id

ReferenceValue
  reference_id
  property_id
  value

StatementReference
  statement_id
  reference_id
```

Tím prakticky dostaneš stejnou expresivitu základního Wikibase modelu.

### Ale udělal bych jednu věc lépe než Wikibase

**Oddělil bych čtyři různé koncepty, které se často házejí do jednoho pytle jako provenance/history:**
informacepříkladprovenance tvrzení„tento údaj pochází z dokumentu X“audit změny„Luděk změnil hodnotu 10. 8. 2026“valid time„hodnota platila od 2024 do 2025“transaction time„do systému jsme ji zapsali 10. 8. 2026“
To je důležité. Reference ve Wikibase zachycují provenance samotného tvrzení, zatímco historie editací pochází z MediaWiki revisions.

Ve vlastním modelu bych to explicitně oddělil.

Například:
```
change_set
---------
id
timestamp
actor
source
comment

statement_version
-----------------
statement_id
change_set_id
operation
data
```

A samotný statement může mít třeba:
```
valid_from
valid_to
```

Pro provenance importu:
```
change_set.source = "import:ares-2026-08-10"
```

zatímco provenance informace:
```
statement -> reference -> "ARES API"
```

jsou jiná věc.

## Tvoje myšlenka „lehkého řezu“ je podle mě správná

Dokonce bych z ní udělal hlavní princip systému:
```
                 canonical model
                      │
                append-only history
                      │
                ┌─────▼─────┐
                │ PostgreSQL │
                └─────┬─────┘
                      │
             projection builder
         ┌────────────┼────────────┐
         ▼            ▼            ▼
    current graph   search      domain view
    projection      projection  projection
```

Canonical data mohou být bohatá a verzovaná.

Ale běžný dotaz:
```
firma → provozuje → aplikace
aplikace → podporuje → proces
```

nemá důvod rekonstruovat historii.

Máš například tabulku:
```
current_statement
-----------------
statement_id
subject_id
property_id
object_entity_id
value_json
```

nad kterou děláš normální SQL:
```
SELECT ...
FROM current_statement
WHERE subject_id = ...
```

Historický model se používá jen při:
```
show history
as-of 2025-01-01
diff revision A/B
```

To je**CQRS/event-sourcing-light**, aniž bys musel vybudovat event-sourcingový framework.

A hlavně: zpočátku k tomu vůbec nepotřebuješ Neo4j ani Blazegraph. PostgreSQL to zvládne velmi dobře.

## Pro tvoje „mikroaplikace“ je vlastní řešení dokonce výhodnější

Udělil bych systému dvě UI vrstvy:
```
              Knowledge Core
                    │
          ┌─────────┴──────────┐
          │                    │
    generic explorer      domain apps
          │                    │
       Wikibase-ish       custom forms
       item editor        custom workflows
       history            validations
       references         dashboards
```

Generic UI:
```
Q152 – Application XYZ

instance of
    Application

owner
    Department ABC

supports process
    ├── Registration
    └── Payment

uses data
    └── Customer
```

Vedle toho třeba URL:
```
/apps/application-editor/Q152
```

která místo obecného grafového editoru ukáže:
```
Application
───────────────────
Name:        [...]
Owner:       [....]
Processes:   [...]
Interfaces:  [...]
Data:        [...]
Technology:  [...]
```

ale zapisuje pořád**stejné statements do stejného knowledge graphu**.

Tohle je podle mě velmi silný koncept.

Wikibase sice dnes poskytuje REST API pro čtení a editaci entit a statements, takže vlastní UI nad ním možné je.Jenže pak platíš provozní a datově-modelovou cenu MediaWiki za backend, jehož UI vlastně postupně obcházíš.

## Kdy bych naopak Wikibase doporučil
požadavekmoje volbaveřejná wiki s mnoha editory**Wikibase**kompatibilita s Wikidata toolingem**Wikibase**multilingual labels/descriptions jako zásadní featurespíš**Wikibase**SPARQL/RDF jako hlavní veřejné rozhraníspíš**Wikibase**osobní/interní knowledge graph**vlastní core**jednoduchý all-in-one deployment**vlastní core**custom domain UI**vlastní core**atomické změny více entit**vlastní core**vlastní provenance/version model**vlastní core**jemné access control**vlastní core**
Mimochodem poslední bod je další potenciální problém: Wikibase aktuálně nemá nativní možnost skrýt jednotlivé items/properties; jeho FAQ pro takový případ doporučuje oddělené instance.

# Co bych tedy doporučil konkrétně

**Nedělal bych „novou Wikibase“. Udělal bych malý knowledge-store se syntaxí inspirovanou Wikibase.**

Architekturu bych držel zhruba takto:
```
┌────────────────────────────────────────────┐
│                Web application             │
│                                            │
│  generic entity UI     microapplications   │
└────────────────────┬───────────────────────┘
                     │
                REST / RPC API
                     │
┌────────────────────▼───────────────────────┐
│               Knowledge Core               │
│                                            │
│ Entity                                     │
│ Property                                   │
│ Statement                                  │
│ Qualifier                                  │
│ Reference                                  │
│ ChangeSet / Revision                       │
│ Schema / Constraints                       │
└────────────────────┬───────────────────────┘
                     │
                PostgreSQL
                     │
       ┌─────────────┴────────────┐
       │                          │
 canonical/history          projections
```

Deployment:
```
docker compose up

app
postgres
```

Případně později:
```
+ Meilisearch/OpenSearch
```

ale pouze pokud to bude skutečně potřeba.

A přidal bych**RDF jako interchange format, nikoli jako interní storage model**:
```
GET /entity/Q123
GET /entity/Q123/history
GET /statement/S123

GET /export/Q123.ttl

POST /query
```

Tak získáš možnost jednoho dne připojit SPARQL/triplestore, aniž by sis jím komplikoval základ systému.

### Jedna věc, kterou bych ještě změnil v původní představě

Neříkal bych tomu interně „triplety s properties“.

Spíš:

**Entity + first-class Statements**

tj.:
```
Entity -- Statement --> Value
             │
             ├── qualifiers
             ├── references
             ├── validity
             └── metadata
```

To je zásadní rozdíl. Jakmile statement dostane vlastní ID, verzování, provenance, poznámky a validitu, přestává čistý RDF triple model být přirozeným primárním modelem.

**Moje volba pro tebe tedy vychází poměrně jednoznačně: PostgreSQL + vlastní malý Wikibase-like core.**Wikibase bych si ponechal jako referenční model: velmi důkladně bych okopíroval princip`Entity / Property / Statement / Qualifier / Reference`, ale nikoliv MediaWiki infrastrukturu.

Pokud budeš chtít pokračovat, jako další krok bych navrhl**konkrétní datový model v PostgreSQL včetně verzování**. Tam je několik zajímavých rozhodnutí — zejména zda verzovat jednotlivé statementy, celé entity, nebo zavést globální`changeset`; já bych se přikláněl ke globálnímu changesetu + immutable statement versions.

### Assistant

Ano. Pro v1 bych tedy rozšířil základní sadu hodnotových typů na:
```
EntityReference
String
LocalizedString
Boolean
Integer
Decimal
Date
DateTime
URI
ExternalIdentifier
Quantity
Interval
```

U`Quantity`bych minimálně definoval:
```
value
unit
```

např.:
```
{
  "value": 12.5,
  "unit": "km"
}
```

a ideálně už od začátku rozhodnout, zda`unit`je jen string/kód, nebo reference na entitu typu jednotka. Přikláněl bych se k referenci na řízený identifikátor jednotky, protože pak lze později bezpečně dělat převody, kontroly dimenzí apod.

U`Interval`bych si dal pozor, protože může znamenat více věcí. Pro v1 bych jej chápal jako obecný interval nad kompatibilním skalárním typem:
```
{
  "from": 10,
  "to": 20,
  "fromInclusive": true,
  "toInclusive": false
}
```

nebo:
```
{
  "from": "2026-01-01",
  "to": "2026-12-31"
}
```

Tedy`Interval<Integer>`,`Interval<Decimal>`,`Interval<Date>`,`Interval<DateTime>`a případně`Interval<Quantity>`. Nedával bych mu význam „platnost statementu“ — to je jiná věc.`valid_from/valid_to`statementu jsou systémově podporovaná temporální metadata, zatímco`Interval`je normální hodnota doménové property.

### Co jsem myslel tím „new property anytime“

Myslel jsem tím rozdíl mezi**otevřeností datového modelu**a**otevřeností typu hodnoty**.

Chceme, aby uživatel mohl kdykoliv bez změny databázového schématu založit novou property:
```
P101 = délka kabelu
datatype = Quantity

P102 = provozní období
datatype = Interval<Date>

P103 = odpovědná organizace
datatype = EntityReference
```

To je ono „anytime“:

Novou**Property**lze přidat za běhu jako data systému, bez změny kódu, SQL migrace nebo deploymentu.

Například dnes model obsahuje:
```
Application
 ├─ owner
 ├─ name
 └─ status
```

a zítra modelář přidá:
```
P812 = recoveryTimeObjective
datatype = Quantity
```

Core to okamžitě umí uložit, validovat a dotazovat, protože`Quantity`už je známý systémový datatype.

Naopak nechci, aby modelář mohl za běhu říct:
```
P813
datatype = MySpecialNetworkAddressRangeObject
```

a vložit libovolný JSON.

To by znamenalo, že:

- query engine nezná sémantiku hodnoty,
- neumí ji správně indexovat,
- neumí porovnávat hodnoty,
- neumí validovat,
- GraphQL neví, jaký typ vystavit,
- import/export nezná reprezentaci,
- lens engine neví, co s ní,
- databáze se postupně mění v JSON document store.

Proto bych měl**uzavřenou množinu datatype definovanou verzí knowledge-core software**, ale**otevřenou množinu Properties definovanou uživatelskými daty**.

Schematicky:
```
              Knowledge Core
                   │
          podporované datatypes
                   │
       ┌───────────┴───────────┐
       │                       │
     String                 Quantity
     Date                   Interval
     EntityRef              ...
       │
       ▼
────────────────────────────────────────
          uživatelský model
────────────────────────────────────────
 P1 name:String
 P2 owner:EntityRef
 P3 weight:Quantity
 P4 activePeriod:Interval<Date>
 P5 ...
 P6 ...
 P10000 ...
```

Počet Properties je prakticky neomezený a může se měnit dynamicky. Množina základních datatype je naopak kontrolovaná.

### A ani datatype nemusí být navždy zamrzlé

Jen jejich přidání je**změna core platformy**, nikoli změna doménového modelu.

Například ve v2 zjistíme, že opravdu potřebujeme:
```
GeoPoint
```

Pak release core:
```
knowledge-core 1.3
```

přidá nový nativní datatype`GeoPoint`.

Od té chvíle může modelář zase „anytime“ vytvářet:
```
P967 = headquartersLocation
datatype = GeoPoint
```

bez dalšího programování.

Takže jsou vlastně dvě rychlosti změny:
```
Domain evolution
──────────────────────────
nová Property
nová Entity class
nový Lens
nový Constraint

→ běžná práce s modelem
→ bez deploymentu software

Core evolution
──────────────────────────
nový datatype
nový druh constraintu
nová lens primitive

→ změna software
→ release knowledge core
```

Tohle oddělení považuji za důležité, protože zachovává tu vlastnost Wikibase, kterou jste oceňovali —**model lze rozšiřovat za běhu bez předělávání aplikace**— ale současně nedovolí, aby se vlastní persistence postupně stala nedefinovaným pytlem JSON objektů.

### User

Ad 11 - ano .. dokonce bych povazoval za povinne vzdy pridat label/en. Name a dalsi property pak muze byt uz v modelu, pokud ma dalsi vyznam.

### Assistant

Souhlas. Tohle bych už formuloval jako**core invariant**, ne jen doporučení:

Každá Entity, Property, Lens, Package a další uživatelsky viditelný modelový objekt musí mít při vzniku alespoň`label.en`.

Tedy například:
```
Entity Q123

system metadata:
  label:
    en: "Customer Relationship Management"
    cs: "Řízení vztahů se zákazníky"   # optional

  description:
    en: "Application supporting customer relationship management."  # optional
```

`label`bych považoval za**identifikační/prezentační metadata systému**, nikoli za běžný statement.

To je důležité právě kvůli tomu, aby generic UI vždy umělo zobrazit:
```
Q123 — Customer Relationship Management
P37  — Owner
L15  — Application
```

bez znalosti konkrétního doménového modelu.

Naopak pokud doména potřebuje skutečné jméno jako údaj, vytvoří se normální Property, například:
```
P101 — Official application name
datatype: LocalizedString
```

a pak může být:
```
Q123.label.en
    = "CRM"

Q123 -- P101 -->
    en: "Customer Relationship Management System"
    cs: "Systém řízení vztahů se zákazníky"
```

Tyto dvě hodnoty se klidně mohou lišit, protože mají jinou sémantiku.

Tohle bych nedělal:
```
label = automaticky hodnota P101
```

jako pevné pravidlo. Lens nebo UI může říct, že preferovaný display label má být odvozen z nějaké property, ale**canonical system label musí pořád existovat samostatně**. Jinak by generic prostředí přestalo být spolehlivé při změně modelu nebo ACL.

Je tu ještě jeden zajímavý důsledek pro ACL: pokud má uživatel právo vůbec**discover**entitu, pak bych`id + label`považoval za základní discovery metadata. Pokud nemá ani discover právo, nedostane samozřejmě ani label.

Pro specifikaci v1 bych tedy zapsal něco jako:
```
Invariant LABEL-01
------------------
Every user-visible model or graph object that has an independent identity
MUST contain a non-empty English label (`label.en`) at creation time.

The label is system metadata and MUST NOT be represented as a domain Statement.

Additional localized labels MAY be provided.

Domain-specific names, official names, titles and similar values MUST be
modeled as ordinary Properties when they carry domain semantics.
```

A ještě bych doporučil, aby`label.en`nebyl unikátní.**Identitu nese Q/P/L ID, nikoliv label.**Dvě entity tedy mohou mít úplně stejný label.

### User

Vytvor mi konzistentni technicky navrh podle tebou navrhovane struktury. Vytvor ho jako MD dokument ke stazeni.

### Assistant

Hotovo. Připravil jsem konzistentní technický návrh Knowledge Core v1 v Markdownu, včetně invariantů, datového modelu, verzování, packages/releases, lenses, authorization, projekcí, API principů, import/exportu, acceptance scénářů a otevřených rozhodnutí.

knowledge_core_v1_technicky_navrh.md

