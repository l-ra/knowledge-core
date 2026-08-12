# Zadání — Fáze 18: RDF N-Triples import

**Status:** implemented

## Cíl

Import RDF (N-Triples) do package jako ChangeSet: dry-run + commit.

## Pravidla v1

- Formát: N-Triples; blank nodes → reject
- Cíl: `POST /v1/packages/{code}/rdf/import` `{ ntriples, dryRun }`
- Match: `iri_base+iri_local` nebo `entity_iri_alias`
- Kind: `rdf:Property` → P*, `rdfs:Class`/`owl:Class` → C*, jinak Q*; predicate bez type → property; type object bez type → class
- Structural: `rdf:type`, `rdfs:label`, `owl:sameAs`, `rdfs:subClassOf`, `rdfs:domain` (warning only), `rdfs:range`
- Ostatní triple → statement
- Konflikty: existující objekty neupdatovat; duplikátní statement skip; datatype mismatch skip + warning
- Datatype nových P*: z `rdfs:range` + pozorovaných literálů; konflikt / `rdfs:Literal` → **`Any`**
- `Any`: hodnota nese konkrétní `value.type`

## UI

Package detail: upload `.nt` → dry-run preview → Commit
