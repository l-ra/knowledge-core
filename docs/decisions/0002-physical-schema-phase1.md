# ADR 0002 — Fyzické PostgreSQL schema (Fáze 1)

**Status:** Accepted  
**Datum:** 2026-08-10

## Princip

- UUID jako canonical PK
- Veřejné ID (`Q`/`P`/`S`) unikátní, monotónní přes `id_counter`
- Typed value hybrid dle ADR 0001
- `statement_current` aktualizován ve stejné transakci jako `statement`

## Tabulky Fáze 1

### `id_counter`

```sql
CREATE TABLE id_counter (
  kind TEXT PRIMARY KEY, -- entity | property | statement | reference
  last_value BIGINT NOT NULL DEFAULT 0
);
```

### `entity`

```sql
id UUID PK,
public_id TEXT UNIQUE NOT NULL, -- Q1
status TEXT NOT NULL, -- active|deprecated|redirected|deleted
created_at TIMESTAMPTZ NOT NULL,
updated_at TIMESTAMPTZ NOT NULL
```

### `property_definition`

```sql
id UUID PK,
public_id TEXT UNIQUE NOT NULL, -- P1
datatype TEXT NOT NULL,
status TEXT NOT NULL,
created_at, updated_at
```

### Labels / descriptions

Oddělené tabulky `(owner_id, lang, text)` s UNIQUE `(owner_id, lang)`.

### `statement`

```sql
id UUID PK,
public_id TEXT UNIQUE NOT NULL,
subject_id UUID NOT NULL REFERENCES entity(id),
property_id UUID NOT NULL REFERENCES property_definition(id),
status TEXT NOT NULL DEFAULT 'active',
value_type TEXT NOT NULL,
value_bool BOOLEAN,
value_int64 BIGINT,
value_numeric NUMERIC,
value_date DATE,
value_timestamptz TIMESTAMPTZ,
value_text TEXT,
value_entity_id UUID REFERENCES entity(id),
value_json JSONB,
valid_from TIMESTAMPTZ,
valid_to TIMESTAMPTZ,
created_at, updated_at
```

CHECK: právě jedna value „větev“ odpovídá `value_type`.

### `statement_current`

Stejná value projekce pro `status = active` statements; mazání/update sync s mutací.

### `change_set` / `change_set_item`

Minimální stub: id, actor (nullable do Fáze 5), committed_at, idempotency_key; items odkazují object_type + object_id + op.
