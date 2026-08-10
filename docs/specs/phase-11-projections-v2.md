# Zadání — Fáze 11: Projections v2

**Status:** done

## Cíl

Dokončit projekce: ACL-aware search (ADR 38.7), RDF export, background outbox worker.

## Scope

**In:**

- ACL filtr search hitů před výsledkem (entity `discover`, statement `read`)
- RDF projection (`projection_rdf`) + rebuild + N-Triples export
- Outbox CLI: `knowledge-core outbox process`
- Helm CronJob `outboxWorker`
- Acceptance: SearchACL, RDFProjection

## Acceptance

| ID | Scénář | Status |
|----|--------|--------|
| SearchACL | Search neprozrazuje entity bez discover / property bez read | done |
| RDF | Rebuild RDF z canonical + export N-Triples | done |
