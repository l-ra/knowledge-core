# RDF well-known vocabulary (import / export)

**Status:** living reference (SoT: `internal/rdf/infer.go`, `internal/rdf/prefixes.go`, `internal/store/rdf_import.go`, `internal/store/rdf.go`)

## Proč existuje

Knowledge Core má vlastní SoT identity (`Q*` / `P*` / `C*`). RDF je **projekce a výměnný formát**, ne vlastnictví W3C slovníků.

Well-known IRI (RDF / RDFS / OWL / XSD / KC ontology) proto:

1. **nejsou kandidáti na package** při analyze / `@prefix` extrakci — z `rdf:` / `rdfs:` / `owl:` se nezakládá package;
2. **neimportují se jako entity** — `rdfs:Class`, `rdf:Property`, `owl:Thing` … se nevytváří jako Q\*/P\*/C\*;
3. vybrané predikáty a typy se **mapují strukturalně** na model KC (labels, kind, aliases, class profile, datatype inference).

Doménové properties z vaší ontologie (`https://example.org/…`) se naopak importují/exportují jako běžné P\* a statementy.

---

## Strukturalní predikáty

Tyto predikáty **nejsou** statementy přes P\*. Při importu se zpracují speciálně; při exportu je projection zapisuje tam, kde má KC odpovídající metadata.

| Predikát | IRI | Import | Export |
|----------|-----|--------|--------|
| `rdf:type` | `http://www.w3.org/1999/02/22-rdf-syntax-ns#type` | Klasifikace uzlu + evidence kind (viz typy níže) | Q\* → `kc:Entity`; P\* → `rdf:Property`; C\* → `rdfs:Class`; statement reifikace → `kc:Statement` |
| `rdfs:label` | `http://www.w3.org/2000/01/rdf-schema#label` | Literal → `entity_label` (bez `@lang` → `en`) | Jen `label.en` → plain literal |
| `owl:sameAs` | `http://www.w3.org/2002/07/owl#sameAs` | Object IRI → `entity_iri_alias` (`imported` / match) | Alias kind `sameAs` nebo `imported` |
| `rdfs:subClassOf` | `http://www.w3.org/2000/01/rdf-schema#subClassOf` | První odkaz → `class_profile.subClassOf` (pro nové C\*) | `class_profile.subClassOf` → parent IRI |
| `rdfs:range` | `http://www.w3.org/2000/01/rdf-schema#range` | Evidence pro datatype nových P\* (viz XSD) | *(zatím neexportuje se jako triple na property)* |
| `rdfs:domain` | `http://www.w3.org/2000/01/rdf-schema#domain` | Jen **warning** (neuloží se do modelu) | — |

Ostatní predikáty v N-Triples = kandidáti na **statement** (subject × property × value), pokud property existuje nebo se při importu vytvoří.

---

## Typové objekty (`rdf:type` object)

| Typ | IRI | Import | Export |
|-----|-----|--------|--------|
| `rdf:Property` | `…/22-rdf-syntax-ns#Property` | Uzel → P\* | P\* má `rdf:type rdf:Property` |
| `rdfs:Class` | `…/rdf-schema#Class` | Uzel → C\* | C\* má `rdf:type rdfs:Class` |
| `owl:Class` | `…/owl#Class` | Stejně jako `rdfs:Class` | — (export používá `rdfs:Class`) |
| *(žádný / jiný)* | | Default → Q\* | Q\* má `rdf:type kc:Entity` |
| `rdfs:Literal` / `rdfs:Resource` / `owl:Thing` | | Nepoužívají se jako kind entity; `rdfs:Literal` v `rdfs:range` → datatype `Any` | — |

Well-known type IRI **samy o sobě** se jako entity nevytváří (`IsWellKnownVocabIRI`).

---

## XSD / literály (hodnoty statementů)

| Datatype IRI / tvar | Import (literal → KC datatype) | Export (KC → literal) |
|---------------------|--------------------------------|------------------------|
| `xsd:string` / plain / `rdf:langString` | String | String → plain `"…"` |
| `xsd:boolean` | Boolean | `"true"/"false"^^xsd:boolean` |
| `xsd:integer` / `xsd:long` | Integer | `"n"^^xsd:integer` |
| `xsd:decimal` | Decimal | `"…"^^xsd:decimal` |
| `xsd:date` | Date | `"…"^^xsd:date` |
| `xsd:dateTime` | DateTime | *(export statement literálu `DateTime` zatím nemá vlastní větev v `formatRDFObject`)* |
| `xsd:anyURI` | URI | URI jako plain/string literal |
| Object IRI | EntityReference | `<canonical-iri>` |

`rdfs:range` na nové property + pozorované literály → inference datatype; konflikt nebo `rdfs:Literal` → `Any`.

---

## Namespace přeskočené jako package kandidáti

`IsWellKnownVocabIRI` — analyze / Turtle `@prefix` / discover **nevytváří** assignment na tyto base:

| Prefix / base | Účel |
|---------------|------|
| `http://www.w3.org/1999/02/22-rdf-syntax-ns#` | RDF |
| `http://www.w3.org/2000/01/rdf-schema#` | RDFS |
| `http://www.w3.org/2002/07/owl#` | OWL |
| `http://www.w3.org/2001/XMLSchema#` | XSD |
| `https://knowledge-core.local/ontology/` | KC ontology (`Entity`, `Statement`, …) |
| `https://knowledge-core.local/entity/` | fallback entity IRI |
| `https://knowledge-core.local/property/` | fallback property IRI |
| `https://knowledge-core.local/statement/` | statement reifikace v RDF |

To **neznamená**, že se `rdfs:label` nedá použít v N-Triples — naopak se používá strukturalně. Znamená to jen, že z těchto namespaceů nevznikne package „rdfs“.

---

## Shrnutí pravidel

| Otázka | Odpověď |
|--------|---------|
| Mám v Turtle `@prefix rdfs:`? | Ano, pro čitelnost; analyze ho přeskočí jako package. |
| Importuje se `rdfs:label`? | Ano → systémové labels, ne P\*. |
| Mám založit package na `…/rdf-schema#`? | Ne — label/type jsou vestavěné mapování. |
| Co je „moje“ ontologie? | IRI pod `package.iri_base` (a další ne-well-known base). |

Související: [phase-17-iri-rdf.md](phase-17-iri-rdf.md), [phase-18-rdf-import.md](phase-18-rdf-import.md).
