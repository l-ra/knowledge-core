# Knowledge Core v1 — technický návrh

**Stav dokumentu:** návrh pro implementaci první verze jádra  
**Účel:** definovat konzistentní architekturu, datový model, invarianty, verzování, authorization, lens mechanismus a provozní kontrakty systému inspirovaného Wikibase, ale navrženého jako lehké all-in-one řešení.

---

## 1. Účel a kontext

Cílem systému je vytvořit lehké knowledge-management jádro s vlastnostmi podobnými Wikibase:

- otevřený, rozšiřitelný datový model typu entity–property–value,
- first-class statements s metadata a provenance,
- jednoduché a stabilní veřejné identifikátory,
- historie a audit změn,
- verzovatelný model a řízené promotion mezi prostředími,
- možnost vytvářet zjednodušené projekce pro efektivní dotazování,
- obecné Wikibase-like UI,
- doménové pohledy a mikroaplikace nad stejnými daty,
- centrální řízení přístupu na úrovni modelu i konkrétních dat,
- domain-oriented API bez nutnosti pracovat v běžných aplikacích přímo s Q/P identifikátory.

Návrh záměrně odděluje:

1. **knowledge graph** — otevřená doménová data,
2. **model repository** — definice properties, lenses, policies, packages a releases,
3. **software** — vlastní executable kód platformy a případné custom domain hooks,
4. **projekce** — odvozené reprezentace určené pro dotazování, vyhledávání nebo integrace.

Systém nemá být implementací Wikibase ani plným RDF/OWL triplestorem. Inspiruje se jejími osvědčenými principy, ale odstraňuje vazbu na MediaWiki, globální směšování modelu a dat a komplikovaný promotion/deployment model.

---

# 2. Cíle v1

Knowledge Core v1 MUSÍ podporovat:

- open-world definování nových properties bez změny databázového schématu,
- entity, properties a statements,
- qualifiers a references,
- explicitní systémové identifikátory,
- povinné `label.en` pro uživatelsky viditelné pojmenované objekty,
- historii prostřednictvím immutable revisions a changesetů,
- oddělení provenance tvrzení od auditu změny,
- transaction time a volitelný valid time,
- verzované modelové packages,
- immutable releases,
- deployment model umožňující jiné verze modelu v DEV / TEST / PROD,
- deklarativní domain lenses,
- domain-oriented read a write API,
- centrální authorization s kombinací RBAC a omezeného ABAC,
- filtrování výsledků podle identity a oprávnění subjektu,
- synchronní current-state projekci,
- export/import verzovatelných modelových bundles,
- optimistic concurrency a idempotentní write API,
- all-in-one provoz s jedním aplikačním procesem a PostgreSQL jako jedinou povinnou infrastrukturní komponentou.

---

# 3. Explicitní non-goals v1

V první verzi nejsou cílem:

- plný SPARQL endpoint,
- OWL reasoning nebo obecný inference engine,
- externí RDF store jako povinná součást runtime,
- Elasticsearch / OpenSearch jako povinná infrastruktura,
- globální historické dotazování typu `AS OF timestamp` nad celým graph-em,
- workflow/BPM engine,
- obecný uživatelský scriptovací jazyk,
- arbitrary executable code uvnitř lens definic,
- dynamické datatypes definované uživatelem,
- multi-master nebo distribuovaný datastore,
- Kafka jako nutná součást architektury,
- dynamicky generované GraphQL schema per user,
- univerzální datová migrace při každé změně modelu,
- plná statement-level ACL, pokud nevznikne konkrétní use case,
- současný běh více nekompatibilních model releases nad stejným runtime datasetem.

---

# 4. Základní architektonické principy

## 4.1 Jeden canonical source of truth

Canonical data MUSÍ být uložena v Knowledge Core datastore.

Externí projekce, RDF exporty, search indexy ani klientské cache NESMÍ být source of truth.

## 4.2 Open-world doménový model, controlled-world platforma

Doménový model je otevřený:

- nové Entity lze zakládat kdykoliv,
- nové Property lze zakládat kdykoliv,
- nové vazby mezi entitami lze přidávat bez SQL migrace.

Platformní typový systém je uzavřený:

- datatypes jsou definovány verzí Knowledge Core software,
- nový datatype je změna platformy, nikoliv běžná změna doménového modelu.

## 4.3 Oddělení systémového modelu a knowledge graphu

Systém NESMÍ modelovat všechny své vlastní interní objekty jako běžné Q/P entity.

Musí existovat jasné oddělení:

```text
SYSTEM / MODEL REPOSITORY
-------------------------
Package
PropertyDefinition
LensDefinition
Policy
Release
ChangeSet
ReferenceDefinition / metadata
...

DOMAIN GRAPH
------------
Entity
Statement
Qualifier
Reference
```

Obě vrstvy mohou být fyzicky uloženy ve stejné PostgreSQL databázi, ale mají rozdílné invarianty, lifecycle a API.

## 4.4 Statement je first-class objekt

Knowledge graph není pouze sada RDF-like trojic.

Každé tvrzení má vlastní identitu a může mít:

- qualifiers,
- references,
- valid time,
- vlastní historii,
- package ownership,
- authorization metadata.

## 4.5 Revision není release

Historická revision je interní auditní jednotka změny konkrétního objektu.

Release je immutable, konzistentní a explicitně publikovaný celek určený pro promotion/runtime.

## 4.6 Lens je deklarativní modelový artefakt

Lens:

- není kopie dat,
- není doménová ontologie,
- není povinně source code,
- je deklarativní mapování mezi otevřeným graphem a doménovým kontraktem.

Lens je verzované **modelové datum** uložené v model repository.

## 4.7 Authorization je centrální

Žádné API, UI ani projekce nesmí být samostatnou bezpečnostní hranicí.

Veškeré čtení a zápis přes:

- generic graph API,
- lens API,
- GraphQL,
- mikroaplikace,
- exporty

musí používat stejné authorization rozhodování.

---

# 5. Core invariants

Následující invarianty jsou pro v1 normativní.

## ID-01 — Stabilní canonical identita

Každý objekt s vlastní identitou MUSÍ mít interní immutable canonical ID, doporučeně UUID.

## ID-02 — Veřejné Q/P identifikátory

Entity a Property MUSÍ mít stabilní jednoduchý veřejný identifikátor:

- `Q<n>` pro Entity,
- `P<n>` pro Property.

Tyto identifikátory se NIKDY nerecyklují.

## ID-03 — Oddělení identity od labelu

Label ani žádný doménový atribut NESMÍ být použit jako canonical identita.

## LABEL-01 — Povinný anglický label

Každý uživatelsky viditelný **pojmenovaný** modelový nebo graph objekt MUSÍ při vytvoření obsahovat neprázdný `label.en`.

Minimálně se to týká:

- Entity,
- Property,
- Lens,
- Package,
- Policy,
- Release, pokud je zobrazován uživateli.

Statement a interní revision nemusí mít vlastní label.

## LABEL-02 — Label je systémové prezentační metadata

`label` NESMÍ být reprezentován jako běžný domain Statement.

Doménové názvy, oficiální názvy, tituly a podobné údaje se modelují jako ordinary Properties, pokud mají vlastní doménovou sémantiku.

## LABEL-03 — Label nemusí být unikátní

Unikátnost je dána identitou, nikoliv labelem.

## CHANGE-01 — Každá mutace je součástí ChangeSetu

Žádná změna canonical modelu nebo canonical graphu nesmí vzniknout mimo ChangeSet.

## CHANGE-02 — Atomicita ChangeSetu

ChangeSet je atomická transakční jednotka.

Buď se aplikují všechny jeho změny, nebo žádná.

## CHANGE-03 — Immutable revisions

Revision objektu je immutable.

Změna vytváří novou revision.

## PROV-01 — Provenance != audit

Reference/evidence tvrzení a audit toho, kdo změnu provedl, jsou dvě oddělené struktury.

## TIME-01 — Transaction time

Každá revision MUSÍ mít serverem generovaný transaction timestamp.

## TIME-02 — Valid time

Statement MŮŽE mít doménový valid time.

Valid time nesmí být zaměňován s transaction time.

## MODEL-01 — Stabilní význam Property

Identita Property reprezentuje stabilní význam.

Nekompatibilní změna významu nebo hodnotového typu MUSÍ vytvořit novou Property.

## MODEL-02 — Dynamic Property creation

Novou Property lze vytvořit runtime jako data bez SQL migrace nebo redeploymentu platformy.

## TYPE-01 — Controlled datatype set

Property MUSÍ používat datatype z uzavřené množiny podporované danou verzí Knowledge Core.

## PACKAGE-01 — Explicitní ownership

Entity, Property, Statement, Lens a Policy musí mít jasné package ownership nebo explicitní systémový ownership.

## PACKAGE-02 — Cross-package reference je povolena

Package může přidávat Statements o Entity vlastněné jiným package.

Statement patří package, který tento statement zavádí.

## RELEASE-01 — Release je immutable

Po publikaci se obsah release nesmí měnit.

## RELEASE-02 — Promotion nepřenáší authoring historii

Produkční promotion modelu má pracovat s release bundle, nikoliv s kopií celé authoring databáze.

## LENS-01 — Lens není security boundary

Lens definuje datový kontrakt, nikoliv oprávnění.

## LENS-02 — Explicitní write semantics

Lens write operace musí být explicitní delta (`set`, `clear`, `add`, `remove`, případně další přesně definované operace).

Implicitní whole-object replacement není v1 povolen.

## AUTH-01 — Default deny

Pokud není přístup výslovně povolen efektivní policy, výsledek je `DENY`.

## AUTH-02 — Security metadata nejsou běžná domain data

ACL/policies musí být uloženy v privilegované systémové vrstvě.

Mohou odkazovat na knowledge data, ale nesmí být obyčejnými statements ve stejném datovém prostoru.

## AUTH-03 — Authorization before mutation

Celá výsledná delta ChangeSetu musí být autorizována před commitem.

## PROJ-01 — Current state je synchronní

Current-state reprezentace musí být aktualizována ve stejné databázové transakci jako canonical změna.

## PROJ-02 — Externí projekce jsou rebuildable

Asynchronní projekce musí být odvoditelné z canonical dat a znovu sestavitelné.

## CONC-01 — Optimistic concurrency

Update klienta musí být schopen odmítnout změnu založenou na zastaralé očekávané revision.

## IDEMP-01 — Idempotentní mutation retries

Write API musí podporovat idempotency key nebo ekvivalentní mechanismus pro bezpečné retry.

---

# 6. Terminologie

| Termín | Význam |
|---|---|
| **Entity** | Identifikovaný doménový objekt typu Q |
| **Property** | Definice významu a datového typu vztahu/atributu typu P |
| **Statement** | First-class tvrzení: subject + property + value |
| **Qualifier** | Dodatečná kvalifikace konkrétního statementu |
| **Reference** | Evidence/provenance podporující statement |
| **Revision** | Immutable historická podoba identifikovaného objektu |
| **ChangeSet** | Atomická sada změn provedená jedním logickým operation |
| **Package** | Jednotka ownership, dependency a release lifecycle |
| **Release** | Immutable publikovaný snapshot package/dependency closure |
| **Dataset** | Logická skupina dat s lifecycle `released` nebo `continuous` |
| **Lens** | Deklarativní mapování graph ↔ domain representation |
| **Policy** | Authorization pravidlo |
| **Projection** | Odvozený model optimalizovaný pro čtení/integraci |
| **Deployment Release** | Konkrétní kombinace software + package releases + datasets |

---

# 7. Identita a veřejné identifikátory

## 7.1 Interní identita

Doporučený primární identifikátor:

```text
UUID
```

Interní FK vazby mají používat UUID nebo jiný immutable interní key.

## 7.2 Veřejné identifikátory

Entity:

```text
Q1
Q15
Q3812
```

Property:

```text
P1
P37
P812
```

Veřejné ID slouží:

- lidem,
- generic UI,
- debugování,
- integracím, kde je přímá graph identita žádoucí.

Běžné domain API nemusí Q/P identifikátory klientovi vystavovat jako primární kontrakt.

## 7.3 Další identity

Entity může mít libovolný počet doménových identifikátorů modelovaných pomocí Properties, např.:

- IČO,
- application code,
- external system ID,
- EUID,
- URI jiného registru.

Tyto identity nejsou canonical identitou Knowledge Core.

## 7.4 Lifecycle Entity

Minimální stav:

```text
active
deprecated
redirected
deleted
```

`deleted` znamená logické odstranění; historie zůstává zachována.

`redirected` odkazuje na canonical replacement Entity.

---

# 8. Label, description a aliases

Každá Entity a Property má systémová multilingual metadata:

```yaml
label:
  en: "Customer Relationship Management"
  cs: "Řízení vztahů se zákazníky"

description:
  en: "Application supporting CRM processes."

aliases:
  en:
    - "CRM"
```

Povinné je pouze:

```yaml
label:
  en: "..."
```

Modelová metadata nejsou běžné Statements.

Domain property typu `officialName`, `businessName` nebo `title` může existovat nezávisle.

---

# 9. Datatype systém v1

## 9.1 Povinně podporované typy

Knowledge Core v1 podporuje minimálně:

```text
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

## 9.2 Obecné pravidlo

Novou Property lze založit kdykoliv nad existujícím datatype.

Nový datatype vyžaduje změnu Knowledge Core software a release platformy.

## 9.3 EntityReference

Odkaz na canonical Entity.

```json
{
  "entityId": "Q123"
}
```

Interně se ukládá canonical UUID.

## 9.4 String

Unicode string bez jazykového tagu.

## 9.5 LocalizedString

Mapa jazyk → hodnota.

Např.:

```json
{
  "en": "Customer",
  "cs": "Zákazník"
}
```

Implementace MUSÍ definovat normalizaci language tagů, doporučeně BCP 47.

## 9.6 Boolean

`true` / `false`.

## 9.7 Integer

Signed integer s jasně definovaným rozsahem.

Doporučení: 64-bit signed integer.

## 9.8 Decimal

Přesná desetinná hodnota.

Nesmí být ukládána jako binary floating point.

Je nutné specifikovat maximální precision/scale.

## 9.9 Date

Kalendářní datum bez timezone.

```text
YYYY-MM-DD
```

## 9.10 DateTime

Časový okamžik.

Canonical persistence má být normalizována na UTC; API musí pracovat s ISO 8601 / RFC 3339 reprezentací.

## 9.11 URI

Validovaná URI hodnota.

## 9.12 ExternalIdentifier

Identifikátor v explicitně daném namespace/systemu.

Doporučená struktura:

```json
{
  "scheme": "ico",
  "value": "12345678"
}
```

`scheme` může být řízený identifikátor definovaný modelovým artefaktem.

## 9.13 Quantity

Strukturovaná kvantitativní hodnota:

```json
{
  "value": "12.5",
  "unit": "U17"
}
```

kde:

- `value` je Decimal,
- `unit` je reference na řízenou definici jednotky.

Doporučení v1:

- jednotka není volný string,
- core nemusí v1 provádět automatické převody jednotek,
- core musí zachovat jednotku a umožnit validační constraints na povolené jednotky.

Budoucí rozšíření může doplnit:

- dimension,
- normalization unit,
- conversions,
- uncertainty.

## 9.14 Interval

Interval je doménová hodnota, nikoliv valid time Statementu.

Má definovat:

```json
{
  "from": "...",
  "to": "...",
  "fromInclusive": true,
  "toInclusive": false
}
```

V1 má podporovat interval nad kompatibilními skalárními typy minimálně:

```text
Interval<Integer>
Interval<Decimal>
Interval<Date>
Interval<DateTime>
Interval<Quantity>
```

Je nutné definovat:

- zda `from` nebo `to` mohou být neomezené (`null`),
- zda musí `from <= to`,
- kompatibilitu jednotek u `Interval<Quantity>`.

Doporučení:

- povolit otevřený interval na jedné straně,
- požadovat kompatibilní typy obou mezí,
- u Quantity požadovat shodnou jednotku ve v1.

---

# 10. PropertyDefinition

Property je first-class modelový objekt.

Minimální struktura:

```yaml
id: P37
canonicalId: <uuid>

label:
  en: "Owner"

description:
  en: "Organization responsible for the entity."

datatype:
  kind: EntityReference

package: architecture-core

status: active
```

Volitelné:

- aliases,
- constraints,
- deprecation metadata,
- supersededBy,
- allowed target classes,
- cardinality hints,
- indexing hints.

## 10.1 Property evolution

Kompatibilní změny:

- label,
- description,
- aliases,
- soft constraints,
- status → deprecated.

Nekompatibilní změny:

- změna datatype,
- změna významu,
- změna reprezentující jiný doménový vztah.

Nekompatibilní změna vyžaduje novou Property.

---

# 11. Entity

Minimální struktura Entity:

```yaml
id: Q851
canonicalId: <uuid>

label:
  en: "CRM"

description:
  en: "CRM application"

package: application-inventory

status: active
```

Entity sama o sobě nemusí mít pevný class typ.

Typing je modelován pomocí Statements, např.:

```text
Q851 -- P31(instanceOf) --> Q10(Application)
```

Tím zůstává zachován open-world model.

---

# 12. Statement model

## 12.1 Logická struktura

```text
Statement
---------
id
canonical_id
subject_entity_id
property_id
value
package_id
status
valid_from
valid_to
current_revision_id
```

Hodnota je typed union podle Property datatype.

## 12.2 Statement identity

Statement má stabilní identitu, např.:

```text
S190
```

Je nutné rozlišovat:

- změnu revision stejného logického tvrzení,
- ukončení jednoho Statementu a vytvoření nového.

Core to nesmí automaticky odhadovat z hodnoty.

Write operation musí explicitně určit požadovanou sémantiku.

## 12.3 Duplicitní S/P/O

Systém MUSÍ dovolit existenci více Statementů se stejným:

```text
subject
property
value
```

Důvody:

- rozdílná provenance,
- rozdílné qualifiers,
- rozdílný valid time,
- rozdílný package ownership.

Unikátní constraint na `(subject, property, value)` se nesmí zavést globálně.

## 12.4 Statement status

Minimálně:

```text
active
deprecated
deleted
```

---

# 13. Qualifiers

Qualifier je metadata konkrétního Statementu.

Příklad:

```text
Q100 -- P37(owner) --> Q200
  qualifier:
    validRole: "business-owner"
```

Qualifier používá:

- PropertyDefinition,
- typed value.

V1 nemusí mít vlastní veřejné Q/S-like ID pro každý qualifier, pokud to není nutné pro audit.

Qualifier změny jsou verzovány jako součást revision Statementu.

---

# 14. References a provenance

## 14.1 Reference

Reference reprezentuje důkaz/zdroj podporující Statement.

Reference může obsahovat více typed hodnot, např.:

```yaml
sourceUrl: https://...
documentId: DOC-123
retrievedAt: 2026-08-10T08:00:00Z
```

Reference může být sdílena více Statements, pokud je to užitečné.

## 14.2 Oddělení provenance a auditu

Reference odpovídá na:

> Z čeho tato informace pochází?

ChangeSet odpovídá na:

> Kdo, kdy a jak změnu v systému provedl?

Tyto informace se nesmí směšovat.

---

# 15. Časový model

## 15.1 Transaction time

Každá revision má:

```text
created_at
created_by / actor
change_set_id
```

Transaction time generuje server.

## 15.2 Valid time

Statement může mít:

```text
valid_from
valid_to
```

Valid time je doménová temporal metadata.

V1 nemusí poskytovat plnou bitemporal query algebru.

## 15.3 Interval datatype != valid time

Property může mít hodnotu:

```text
P123 = Interval<Date>
```

To je běžná doménová hodnota a je nezávislá na `statement.valid_from` / `valid_to`.

---

# 16. Revision model

Každý verzovaný objekt má immutable revisions.

Minimálně se verzuje:

- Entity metadata,
- PropertyDefinition,
- Statement,
- LensDefinition,
- Policy,
- Package metadata.

Revision obsahuje:

```text
revision_id
object_id
revision_no
payload / structured fields
change_set_id
created_at
actor_id
```

Current objekt odkazuje na current revision.

Historické revision se nikdy nepřepisují.

---

# 17. ChangeSet

## 17.1 Úloha

ChangeSet je základní auditní a transakční jednotka.

Příklad:

```yaml
id: C1821
actor: U17
timestamp: 2026-08-10T08:15:00Z

operation:
  kind: AssignApplicationOwner

changes:
  - updateStatement: S17 r3 -> r4
  - createStatement: S91
```

## 17.2 Metadata

ChangeSet má obsahovat minimálně:

- ID,
- actor identity,
- timestamp,
- operation type / source API,
- optional user comment,
- idempotency key,
- correlation/request ID,
- seznam změněných objektů.

Doporučeně lze auditovat také:

- relevantní authentication context,
- použitou policy release/version,
- client application ID.

## 17.3 Atomicita

Celý ChangeSet je realizován v jedné PostgreSQL transakci.

---

# 18. Packages

Package je základní jednotka:

- ownership,
- dependencies,
- model lifecycle,
- release,
- promotion.

## 18.1 Package ownership

Package může vlastnit:

- Entity,
- Property,
- Statement,
- Lens,
- Policy,
- případně reference data.

Příklad:

```text
architecture-core
  owns:
    P37 Owner
    P82 Supports process
    Q10 Application
    Lens Application
```

Jiný package může přidat:

```text
project-x
  owns:
    S901: Q851 -- P900 --> Q999
```

i když Q851 vlastní jiný package.

## 18.2 Dependencies

Package může deklarovat dependencies:

```yaml
dependencies:
  core-model: ">=2.0 <3.0"
  organization-model: "^4.1"
```

Konkrétní release MUSÍ mít deterministicky vyřešenou dependency closure.

## 18.3 Package lifecycle

Package/dataset má lifecycle například:

```text
released
continuous
```

`released`:

- změny se publishují v immutable releases.

`continuous`:

- current data se mění průběžně prostřednictvím ChangeSetů.

---

# 19. Releases a verzování

## 19.1 Čtyři osy verzování

Systém rozlišuje:

### Software version

Git commit / build / container image.

### Object revisions

Historie jednotlivých modelových a doménových objektů.

### Package release

Immutable publikovaná verze modelového nebo reference-data package.

### Deployment release

Kombinace software a aktivních package releases/datasets.

## 19.2 Package release

Příklad:

```text
architecture-model@3.4.0
```

Release odkazuje na konkrétní revisions všech zahrnutých objektů a dependency closure.

Po publish je immutable.

## 19.3 Continuous data

Běžný aplikační inventory může běžet:

```text
application-inventory@current
```

bez vytváření release při každém edit operation.

## 19.4 Deployment manifest

Příklad:

```yaml
release: production-2026.08.10

software:
  image: knowledge-platform:1.0.0

packages:
  core-model: 2.1.0
  architecture-model: 3.4.0
  countries: 2026.1

datasets:
  application-inventory: current
```

---

# 20. Authoring vs runtime

## 20.1 Authoring prostředí

Obsahuje:

- všechny revisions,
- draft changesets,
- unpublished model změny,
- historické releases,
- experimentální model objects.

## 20.2 Production runtime

Nemusí obsahovat celou authoring historii model repository.

Production promotion může importovat immutable release bundle obsahující pouze runtime potřebné artefakty.

Cíl:

```text
AUTHORING
   |
   | publish
   v
MODEL BUNDLE
   |
   | promote
   v
PRODUCTION
```

Tím se zabrání nutnosti přenášet celý Q/P prostor nebo všechna historická modelová data mezi prostředími.

---

# 21. Domain Lens

## 21.1 Definice

Lens je deklarativní mapování:

```text
open knowledge graph <-> domain object
```

Stejný graph může mít více lenses.

Například:

```text
ApplicationSummary
ApplicationArchitecture
ApplicationEditor
```

mohou reprezentovat různé řezy téže Entity.

## 21.2 Lens jako data

Lens je verzovaný objekt v model repository.

YAML je pouze import/export serializace, nikoliv source of truth.

Příklad:

```yaml
id: application
version: 4

label:
  en: "Application"

entity:
  selector:
    instanceOf: Q10

key:
  property: P12

fields:
  code:
    property: P12
    type: String
    cardinality: one
    required: true

  name:
    property: P1
    type: LocalizedString

  owner:
    property: P37
    type: EntityReference
    lens: organization
    cardinality: zeroOrOne

  supportedProcesses:
    property: P82
    type: EntityReference
    lens: business-process
    cardinality: many
```

## 21.3 Lens není class

Ontologický koncept `Application` a Lens `ApplicationEditor` jsou rozdílné objekty.

Jeden concept může mít více lenses.

## 21.4 Read semantics

Lens engine:

1. vybere Entity podle selectoru/identity,
2. načte relevantní Statements,
3. aplikuje authorization,
4. mapuje Properties do fields,
5. aplikuje cardinality/type pravidla,
6. vytvoří domain representation.

Read musí být deterministický.

## 21.5 Write semantics

V1 nepovoluje implicitní PUT celé domain entity.

Mutation pracuje s explicitními operacemi:

```text
set
clear
add
remove
```

Např.:

```json
{
  "target": {
    "lens": "application",
    "key": "CRM"
  },
  "operations": [
    {
      "op": "set",
      "field": "name",
      "value": {
        "en": "CRM Platform"
      }
    }
  ]
}
```

Lens engine:

1. resolvuje target Entity,
2. mapuje field na Property,
3. vytvoří explicitní Statement delta,
4. validuje constraints,
5. authorization engine zkontroluje celou delta,
6. vytvoří ChangeSet,
7. commitne revision/current state atomicky.

## 21.6 Proč není povolen whole-object replacement

Kvůli:

- ACL filtered fields,
- partial reads,
- concurrency,
- více Statements stejné Property,
- rozdílné provenance.

Chybějící field v read representation nikdy automaticky neznamená delete.

## 21.7 Custom behavior

Lens může odkazovat na předem registrované software hooks, například:

```yaml
computedFields:
  riskLevel:
    resolver: architecture.calculateRisk
```

Arbitrary source code ani skript uvnitř lens definice není v1 povolen.

---

# 22. GraphQL a domain API

## 22.1 Princip

GraphQL je adapter nad lens engine, nikoliv primární interní datový model.

Běžný klient má pracovat například s:

```graphql
application(code: "CRM") {
  name
  owner {
    name
  }
  supportedProcesses {
    name
  }
}
```

nikoliv:

```graphql
entity(id: "Q851") {
  statements(property: "P37") { ... }
}
```

## 22.2 Generic Graph API

Core současně poskytuje generic API pro:

- generic explorer,
- model authoring,
- debugging,
- import/export,
- special integrations.

Např.:

```text
GET /entities/Q851
GET /statements/S17
GET /entities/Q851/history
```

## 22.3 Domain API

Domain-oriented mutations mají preferovat význam operace:

```text
assignApplicationOwner
decommissionApplication
```

nebo obecné Lens patch API.

Komplexní domain command může být implementován software kódem, pokud změna není pouze mechanické mapování lens fields.

---

# 23. Authorization model

## 23.1 Request security context

Každý request nese minimálně:

```json
{
  "subject": {
    "id": "U123",
    "roles": ["architect"],
    "attributes": {
      "organization": "ORG17"
    }
  }
}
```

Authentication může poskytovat externí OIDC provider.

## 23.2 Authorization operations

V1 minimálně:

```text
discover
read
create
update
delete
manage
```

`discover` řídí, zda je dovoleno odhalit samotnou existenci Entity/objektu.

## 23.3 Granularita v1

Authorization musí minimálně podporovat policy na:

- package,
- entity type / selector,
- Property,
- konkrétní Entity,
- kombinaci Entity + Property.

Statement-level ACL není povinné pro v1, pokud nevznikne konkrétní use case.

## 23.4 RBAC + omezený ABAC

Policy může pracovat s:

- rolemi,
- atributy subjectu,
- atributy cílového objektu,
- omezenými graph relations.

Např.:

```text
ALLOW update Application
IF
  subject.role contains "architecture-admin"
  OR
  subject.organization == entity.ownerOrganization
```

## 23.5 Policy precedence

V1 musí mít deterministický decision model.

Doporučení:

```text
default = DENY
explicit DENY > explicit ALLOW > inherited ALLOW
```

Přesná inheritance pravidla musí být součástí implementační specifikace.

## 23.6 Privilegovaný policy evaluation

Authorization engine musí mít interní privilegovaný přístup k security-relevant canonical datům.

Nesmí se dostat do rekurze typu:

```text
potřebuji ACL k přečtení ACL
```

## 23.7 Read filtering

Authorization musí být aplikována před vytvořením uživatelského výsledku.

Musí být definována leak-prevention semantics pro:

- existence Entity,
- field visibility,
- counts,
- aggregations,
- sorting,
- search,
- relation traversal.

Pokud subject nemá `discover`, API se má typicky chovat stejně jako pro neexistující objekt.

## 23.8 Write authorization

Domain nebo generic mutation:

1. vytvoří plánovanou delta,
2. authorization engine zkontroluje každý affected object/property,
3. mutation se commitne pouze pokud je autorizována celá požadovaná atomická operace.

---

# 24. Current-state model a projections

## 24.1 Canonical history

Canonical persistence uchovává:

- revisions,
- changesets,
- current revision pointers.

## 24.2 Synchronous current projection

Current state určený pro běžné dotazy se aktualizuje v téže SQL transakci.

Např.:

```text
statement_current
```

obsahuje pouze aktuální stav Statements.

Po úspěšném commitu musí následující read vidět nový stav.

## 24.3 Externí projekce

Mohou být asynchronní:

- RDF export/store,
- search index,
- analytics model,
- data warehouse.

Aktualizace se publikuje přes transactional outbox.

## 24.4 Rebuild

Externí projekce musí být možné kompletně rebuildnout z canonical datastore.

---

# 25. Query model v1

Core v1 má podporovat:

## Generic current-state query

- entity lookup,
- property lookup,
- statement filtering,
- traversal přes EntityReference,
- omezené filter/sort/page.

## Lens query

Domain-oriented read přes deklarativní Lens.

## History query

Minimálně:

```text
history(Entity)
history(Property)
history(Statement)
history(Lens)
history(Policy)
history(ChangeSet)

get object at revision
```

Globální arbitrary graph query `AS OF timestamp` není v1 požadována.

## Search

V1 preferuje PostgreSQL:

- full text,
- trigram,
- indexed labels/aliases/selected string values.

---

# 26. Consistency a concurrency

## 26.1 Transaction boundary

Jedna mutation / jeden ChangeSet = jedna SQL transakce.

## 26.2 Optimistic locking

Klient může poslat:

```text
expectedRevision
```

Pokud current revision neodpovídá, server vrátí conflict.

## 26.3 Idempotency

Write API přijímá:

```text
Idempotency-Key
```

nebo ekvivalent v request body.

Opakovaný request se stejným key a stejnou operací nesmí vytvořit duplicitní ChangeSet.

## 26.4 Referential integrity

Canonical datastore musí zabránit vytvoření invalidního EntityReference na neexistující canonical Entity, s výjimkou explicitně podporovaného import/draft režimu.

---

# 27. Constraints a validation

## 27.1 Hard validation

Vždy enforced:

- datatype,
- structural validity,
- required `label.en`,
- canonical identity rules,
- broken references,
- Lens DSL validity,
- package dependency validity.

## 27.2 Domain constraints

Property/model může definovat:

- cardinality,
- allowed target class,
- regex/pattern,
- allowed units,
- numeric ranges,
- required Property combination.

Constraint má severity, např.:

```text
error
warning
info
```

`error` blokuje publish/write podle definovaného kontextu.

## 27.3 Open-world princip

Absence Property nemá automaticky znamenat invaliditu Entity, pokud konkrétní Lens nebo Constraint výslovně neříká opak.

---

# 28. Import/export a bundle format

## 28.1 Canonical bundle

V1 musí mít standardní portable bundle format pro modelové releases.

Příklad obsahu:

```text
manifest.yaml
entities.jsonl
properties.jsonl
lenses.jsonl
policies.jsonl
statements.jsonl
references.jsonl
dependencies.lock
checksums.txt
```

Konkrétní serializace může být JSON/JSONL/YAML, ale formát musí být:

- deterministický,
- verzovaný,
- validovatelný,
- nezávislý na interních DB PK.

## 28.2 Import

Import:

1. ověří format/schema version,
2. ověří checksums,
3. resolve dependencies,
4. ověří identity collisions,
5. ověří model constraints,
6. vytvoří/importuje immutable release.

## 28.3 Export

Export release nesmí vyžadovat:

- kompletní DB dump,
- export všech Q/P z authoring prostředí,
- kopírování nesouvisejících packages.

---

# 29. Promotion DEV → TEST → PROD

Příklad:

```text
DEV
architecture-model@3.5.0-draft
       |
       | publish
       v
architecture-model@3.5.0-rc1
       |
       | promote immutable bundle
       v
TEST
       |
       | approve
       v
architecture-model@3.5.0
       |
       | promote
       v
PROD
```

Promotion přenáší explicitní package release bundle + dependency closure.

Continuous datasets se mohou řídit samostatným mechanismem migrace/synchronizace podle use case; nemají být implicitně součástí každého model release.

---

# 30. Audit a observability

Každý write request musí být dohledatelný přes:

- request/correlation ID,
- actor identity,
- ChangeSet ID,
- changed objects,
- timestamp,
- source API / command,
- idempotency key,
- authorization decision metadata.

Audit history je immutable z pohledu běžných uživatelů.

---

# 31. Doporučená logická architektura

```text
                         External Identity Provider
                                  |
                              OIDC context
                                  |
                                  v
+----------------------------------------------------------------+
|                      Knowledge Core Service                    |
|                                                                |
|  +-------------------+      +-------------------------------+  |
|  | Generic Graph API |      | Domain / Lens API / GraphQL  |  |
|  +---------+---------+      +---------------+---------------+  |
|            |                                |                  |
|            +---------------+----------------+                  |
|                            v                                   |
|                    Authorization Engine                        |
|                            |                                   |
|                 +----------+----------+                        |
|                 |                     |                        |
|                 v                     v                        |
|           Knowledge Engine       Lens Engine                   |
|                 |                     |                        |
|                 +----------+----------+                        |
|                            v                                   |
|                   ChangeSet / Validation                       |
|                            |                                   |
|                            v                                   |
|                     PostgreSQL Store                           |
|                            |                                   |
|                   Transactional Outbox                         |
+----------------------------+-----------------------------------+
                             |
           +-----------------+-------------------+
           |                 |                   |
           v                 v                   v
        RDF export        Search            Analytics
        projection        projection        projection
       (optional)        (optional)         (optional)
```

---

# 32. PostgreSQL — doporučené persistence principy

Tato část není finální SQL schema; stanovuje směr implementace.

Doporučené tabulkové skupiny:

```text
identity / user-visible ids
---------------------------
entity
property_definition
statement

revision/history
----------------
entity_revision
property_revision
statement_revision
lens_revision
policy_revision
change_set
change_set_item

metadata
--------
entity_label
entity_description
property_label
...

statement extensions
--------------------
qualifier
reference
reference_value

model repository
----------------
package
package_dependency
release
release_object
lens
policy

runtime
-------
statement_current
outbox_event
idempotency_record
```

Typed values mohou být implementovány například:

- separátními typed sloupci/tabulkami,
- discriminated union strukturou,
- kontrolovaným JSONB payload + explicitní typed indexy.

Doporučení: nepoužívat jeden nevalidovaný `value JSONB` bez typed constraints.

Fyzický návrh je nutné optimalizovat až podle očekávaného scale envelope.

---

# 33. Nefunkční obálka, kterou je nutné před implementací doplnit

Před finálním fyzickým DB návrhem je nutné stanovit alespoň řádově:

- maximální počet Entity,
- maximální počet current Statements,
- očekávaný počet Statement revisions / rok,
- počet users,
- concurrent requests,
- read/write ratio,
- typická hloubka graph traversal,
- maximální velikost ChangeSetu,
- maximální počet fields v Lens,
- požadovaná P95 latency,
- retention historie,
- maximální velikost release bundle,
- požadované RPO/RTO,
- HA požadavky.

Výchozí v1 předpoklad:

```text
single application deployment
single PostgreSQL cluster
no sharding
no distributed transaction
```

---

# 34. Bezpečnostní požadavky v1

- autentizace externím OIDC providerem,
- žádný anonymní write,
- authorization na každém read/write path,
- default deny,
- privileged system/admin bootstrap role mimo běžný policy graph,
- logování bezpečnostně relevantních změn,
- deterministic policy evaluation,
- žádné arbitrary scripts v policies,
- žádné arbitrary scripts v lenses,
- protection proti enumeration tam, kde subject nemá `discover`,
- export musí respektovat authorization nebo explicitně vyžadovat privileged export capability.

---

# 35. Bootstrap systému

Je nutné explicitně definovat první start systému.

Doporučený bootstrap:

1. Knowledge Core software založí systémové tabulky.
2. Vytvoří built-in administrator principal / vazbu na configured OIDC subject.
3. Nahraje built-in minimal core model:
   - základní system package,
   - nutné built-in Properties,
   - případné unit registry minimum.
4. Administrator může vytvořit první user package.
5. Další model už vzniká standardně přes model API a ChangeSets.

Bootstrap data musí být verzována verzí Knowledge Core software.

---

# 36. Domain microapplications

Mikroaplikace nejsou součástí core v1.

Používají:

- Lens API,
- GraphQL adapter,
- případně domain commands.

Nemají:

- přímý DB přístup,
- vlastní ACL mechanismus,
- přímou závislost na P/Q IDs, pokud to není záměr,
- vlastní canonical kopii Knowledge Core dat.

---

# 37. Acceptance scénáře pro v1

## A1 — Runtime rozšíření modelu

1. Modelář vytvoří `P812`.
2. `label.en = "Recovery time objective"`.
3. datatype = `Quantity`.
4. Neproběhne SQL migrace ani redeploy.
5. Property je okamžitě dostupná generic API a model authoringu.

**Pass condition:** systém podporuje změnu doménového modelu runtime.

---

## A2 — Povinný label

Pokus vytvořit Entity bez `label.en`.

**Expected:** rejected před commitem.

---

## A3 — Domain lens bez Q/P identity v klientovi

Client zavolá:

```text
application(code="CRM")
```

a získá:

```json
{
  "name": "...",
  "owner": "..."
}
```

Klient nemusí znát Q/P IDs.

---

## A4 — ACL filtered update nesmí smazat skrytá data

Entity obsahuje:

```text
name
owner
secretClassification
```

User smí číst/update pouze `name` a `owner`.

User změní `name`.

**Expected:** `secretClassification` zůstane beze změny.

---

## A5 — Property-level access

User smí Entity discover/read, ale nemá read na `P_secret`.

**Expected:** výsledky, search, count a Lens representation neprozradí zakázanou hodnotu podle definované filtering semantics.

---

## A6 — Entity discover protection

User nemá `discover` k Q999.

**Expected:** direct lookup se chová ekvivalentně jako neexistující Entity.

---

## A7 — Atomic ChangeSet

Domain command mění tři Statements.

Authorization třetí změnu zakáže.

**Expected:** žádná ze tří změn není committed.

---

## A8 — Optimistic concurrency

Client A načte revision 5.

Client B vytvoří revision 6.

Client A pošle mutation s `expectedRevision=5`.

**Expected:** conflict; žádná tichá overwrite.

---

## A9 — Idempotent retry

Stejná mutation se stejným idempotency key je odeslána dvakrát.

**Expected:** existuje právě jeden ChangeSet.

---

## A10 — Provenance vs audit

Statement má Reference na externí dokument.

Editor změní qualifier.

**Expected:**

- reference/evidence zůstává doménovou provenance,
- ChangeSet audit samostatně zachytí editora a čas.

---

## A11 — Package cross-reference

Package B vytvoří Statement o Entity vlastněné Package A.

Package B se následně odstraní z release.

**Expected:** Entity A zůstane; Statement B zmizí z výsledného release graphu.

---

## A12 — Model promotion bez celé databáze

DEV obsahuje více experimentálních packages.

Publikuje se `architecture-model@3.4.0`.

**Expected:** TEST/PROD dostane pouze explicitní release + dependency closure, nikoliv celý authoring Q/P prostor.

---

## A13 — Immutable release

Po publish `architecture-model@3.4.0` se někdo pokusí měnit jeho obsah.

**Expected:** rejected; musí vzniknout nový release.

---

## A14 — Synchronous current state

Mutation commitne nový owner.

Bez čekání na async worker následuje read.

**Expected:** read vrací nový owner.

---

## A15 — Projection rebuild

Externí search/RDF projection se smaže.

**Expected:** lze ji znovu sestavit pouze z canonical datastore bez ztráty informací potřebných pro danou projection.

---

# 38. Otevřené otázky před detailní implementační specifikací

Následující body nejsou v tomto dokumentu definitivně uzavřeny a musí se rozhodnout před implementací příslušné části.

## 38.1 Fyzická reprezentace typed values

Varianty:

- typed columns,
- value tables per datatype,
- constrained JSONB union,
- hybrid.

Rozhodnutí závisí na očekávaném scale a query profile.

## 38.2 Quantity unit model

Je třeba definovat:

- built-in vs user-defined units,
- canonical identity jednotek,
- dimension model,
- zda v1 podporuje conversions.

Doporučení v1: stabilní unit registry, bez automatických conversions.

## 38.3 Interval detail

Rozhodnout:

- open-ended interval,
- empty interval,
- invalid range handling,
- Quantity unit compatibility.

## 38.4 Reference model

Rozhodnout, zda:

- Reference je samostatný first-class objekt s vlastním ID,
- nebo embedded struktura deduplikovaná pouze interně.

## 38.5 Statement replacement semantics

Je třeba přesně definovat API rozdíl mezi:

```text
revise existing Statement
```

a:

```text
end/delete old Statement + create new Statement
```

Core nesmí tuto doménovou sémantiku hádat.

## 38.6 Policy language

Definovat malý bezpečný ABAC DSL:

- dostupné operátory,
- dostupné subject attributes,
- povolené graph lookups,
- precedence,
- caching,
- deterministic evaluation.

## 38.7 Search leakage semantics

Přesně stanovit:

- count,
- existence,
- autocomplete,
- fulltext score,
- sorting,
- facet counts

při ACL-filtered datech.

## 38.8 Lens DSL v1

Definovat normativní schema:

- selectors,
- identity/key,
- field mapping,
- cardinality,
- nested lenses,
- computed fields,
- write capabilities,
- constraints,
- API naming/versioning.

## 38.9 Package dependency syntax

Rozhodnout konkrétní version range semantics, např. SemVer.

## 38.10 Release content granularity

Rozhodnout, zda release explicitně vypisuje každou object revision, nebo referencuje immutable snapshot/bundle index.

---

# 39. Doporučené pořadí implementace

## Fáze 1 — Canonical graph

- Entity,
- Property,
- datatype validation,
- Statement,
- label/description,
- basic generic CRUD,
- PostgreSQL current state.

## Fáze 2 — History

- revisions,
- ChangeSets,
- audit,
- optimistic locking,
- idempotency.

## Fáze 3 — Provenance

- qualifiers,
- references,
- valid time.

## Fáze 4 — Model lifecycle

- Packages,
- dependencies,
- releases,
- bundle export/import,
- promotion.

## Fáze 5 — Authorization

- OIDC subject,
- RBAC,
- property/entity ACL,
- limited ABAC,
- read filtering,
- write authorization.

## Fáze 6 — Lenses

- Lens repository,
- read engine,
- explicit patch write engine,
- validation,
- GraphQL adapter.

## Fáze 7 — Projections/integration

- transactional outbox,
- optional RDF export,
- optional search projection,
- rebuild tooling.

Toto pořadí je implementační doporučení; veřejné v1 release může vyžadovat všechny výše uvedené fáze před označením za hotové.

---

# 40. Shrnutí cílového v1

Knowledge Core v1 má být malý, ale přesně definovaný systém:

```text
Open-world knowledge graph
        +
first-class Statements
        +
immutable history / ChangeSets
        +
package/release lifecycle
        +
declarative domain lenses
        +
central authorization
        +
synchronous current projection
        +
portable model bundles
```

Zásadní rozdíl proti původnímu použití Wikibase je:

1. model a doménová data nejsou nerozlišitelně promíchána,
2. promotion nevyžaduje přenos celého knowledge space,
3. běžná SW integrace nemusí pracovat přímo s Q/P identitami,
4. změny jsou atomické a auditovatelné jako ChangeSets,
5. lenses jsou verzovaná modelová data,
6. authorization je centrální součást core,
7. current query model je součást jedné transakčně konzistentní persistence,
8. externí RDF/search reprezentace jsou pouze odvozené projekce,
9. přidání nové Property je runtime změna modelu, zatímco přidání nového datatype je změna platformy,
10. `label.en` je povinné systémové prezentační metadata pro pojmenované uživatelsky viditelné objekty.

Tento dokument má sloužit jako výchozí architektonická specifikace. Dalším krokem má být uzavření otevřených bodů z kapitoly 38 a následné vytvoření:

- normativního logického datového modelu,
- Lens DSL v1 schema,
- Authorization DSL/decision specification,
- REST/GraphQL API kontraktu,
- fyzického PostgreSQL schema,
- testovací specifikace odvozené z acceptance scénářů.
