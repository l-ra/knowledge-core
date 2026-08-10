# ADR 0004 — SQL vrstva zůstává ruční pgx

**Status:** Accepted  
**Datum:** 2026-08-10  
**Kontext:** Fáze 12; ADR 0001 zmiňoval `sqlc`

## Rozhodnutí

Nepřecházet na `sqlc` v1. Store vrstva zůstává u ručního SQL přes `pgx/v5`.

## Důvody

1. Transakční mutace (ChangeSet + outbox + revisions) jsou silně procedurální.
2. Hybrid typed values a dynamické sloupcové mapování se hůře generují.
3. Náklady migrace na sqlc nepřeváží přínos při aktuální velikosti codebase.
4. Acceptance pokrytí je silnější než typovaný SQL codegen.

## Důsledky

- ADR 0001: Driver/SQL = `pgx/v5` (ruční).
- sqlc zůstává volitelný kandidát pro budoucí major refaktor.
