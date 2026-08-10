# Zadání — Fáze 10: Lens a GraphQL

**Status:** done

## Cíl

Nested lens read, patch `add`/`remove` pro `many`, ADR pro lightweight GraphQL.

## Scope

**In:**

- Nested lens (`fields.*.lens`) při read
- Patch `add` / `remove` / `clear` pro many (deprecate statement)
- ADR 0003: zachovat lightweight GraphQL adapter (ne gqlgen)
- Acceptance: NestedLensAndMany

## Acceptance

| ID | Scénář | Status |
|----|--------|--------|
| NestedLensAndMany | Nested owner view + add/remove tags | done |
