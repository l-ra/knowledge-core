# Zadání — Fáze 4: Model lifecycle (Packages / Releases)

**Status:** hotovo včetně importu bundle (A12), classes v release, UI package detail / publish / export / import, **upgrade import + BC** ([phase-package-upgrade-compat.md](phase-package-upgrade-compat.md)).

## Cíl

Packages s ownership, SemVer dependencies, immutable releases, portable bundle export, promotion bez celé DB.

## Scope

**In (done):**

- `package`, `package_dependency`, `package_id` na entity/property/class/statement
- `release`, `release_object`, `release_dependency`
- SemVer ranges (`exact`, `^`, `~`, `>= … < …`)
- `POST /v1/packages`, publish release, list/get release, export bundle
- `POST /v1/releases/import` — promotion immutable bundle do cílového prostředí; **upgrade** vyšší verze s apply revizí + BC gate
- `POST /v1/packages/{code}/releases` — BC gate vůči předchozímu release
- `POST /v1/packages/{code}/releases/{version}/mutate` — explicit reject immutable release
- Classes (`C*`) v publish/export/import jako `objectType: "class"`
- UI: package detail, owned objects, publish, export/import
- Acceptance A11, A12, A13 + import promotion + class-in-release + package upgrade compat

**Deferred:**

- Package move API polish (přeřazení je dostupné; full productization)
- Draft release / release diff preview API
- `force` breaking import

## Acceptance

| ID | Scénář | Status |
|----|--------|--------|
| A11 | Release package A neobsahuje statements package B | done |
| A12 | Export release obsahuje jen package + dependency closure | done |
| A13 | Mutace published release rejected | done |
| — | Class entities included in release + bundle | done |
