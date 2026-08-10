# Zadání — Fáze 4: Model lifecycle (Packages / Releases)

**Status:** hotovo včetně importu bundle (A12 promotion scénář v testu `TestAcceptanceImportPromotion`).

## Cíl

Packages s ownership, SemVer dependencies, immutable releases, portable bundle export, promotion bez celé DB.

## Scope

**In (done):**

- `package`, `package_dependency`, `package_id` na entity/property/statement
- `release`, `release_object`, `release_dependency`
- SemVer ranges (`exact`, `^`, `~`, `>= … < …`)
- `POST /v1/packages`, publish release, get release, export bundle
- `POST /v1/releases/import` — promotion immutable bundle do cílového prostředí
- `POST /v1/packages/{code}/releases/{version}/mutate` — explicit reject immutable release
- Acceptance A11, A12, A13 + import promotion test

**Deferred:**

- (none)

## Acceptance

| ID | Scénář | Status |
|----|--------|--------|
| A11 | Release package A neobsahuje statements package B | done |
| A12 | Export release obsahuje jen package + dependency closure | done |
| A13 | Mutace published release rejected | done |
