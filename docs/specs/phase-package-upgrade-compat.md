# Zadání — Package upgrade + zpětná kompatibilita

**Status:** done (import apply vyšších revizí, BC gate na publish i import, `internal/pkgcompat`).

## Cíl

Umožnit promotion nové verze package DEV → TEST/PROD přes release bundle, aniž by breaking změny rozbily závislá instance data.

## Chování

### Import (`POST /v1/releases/import`)

1. Pokud cíl už má release (nebo live objekty) téhož package → `pkgcompat.Diff(baseline, bundle)`.
2. Jakýkoli `breaking` finding → **409** `compat_breaking` + `findings[]`.
3. Jinak apply:
   - nové public ID → insert
   - stejné ID, stejná revize, shodný obsah → no-op
   - stejné ID, `bundle.rev > current` → zapsat cílovou revizi (upgrade)
   - `bundle.rev < current` → 409 downgrade
   - stejná revize, jiný obsah → 409 identity collision
4. Objekty chybějící v bundlu se **nemažou** (absence ve schématu = breaking ve diffu).
5. Duplicate version release → 409 immutable.

### Publish (`POST /v1/packages/{code}/releases`)

Pokud už existuje prior release → Diff(prior export, live snapshot); breaking → 409 `compat_breaking`.

## BC matice (zkráceně)

| Změna | Kind |
|-------|------|
| Nový class/property/statement/shape | additive |
| Labels / descriptions / usage anotace | metadata |
| catalogVersion a podobné info statementy | metadata |
| Datatype change, zúžení domain/range, ↑minCount, ↓maxCount | breaking |
| Nová required property ve shape, closed true | breaking |
| Absence objektu z předchozího release | breaking |
| Odebrání allowedValue / AllowedRelationship řádku | breaking |

Major SemVer **neobchází** BC, pokud v prostředí už je release.

## API chyby

```json
{
  "error": { "code": "compat_breaking", "message": "…" },
  "findings": [
    {
      "kind": "breaking",
      "reasonCode": "constraint_tightened",
      "objectType": "property",
      "objectPublicId": "…",
      "detail": "minCount 0 → 1"
    }
  ]
}
```

## Seed bundly

`models/archimate-lite/build_bundle.py` při rebuildu bere baseline předchozího bundle (pokud existuje) a u změněného obsahu **bumpne `revisionNo`**.

## Acceptance

- `TestAcceptancePackageUpgradeCompat` — 1.0 import → 1.1 additive OK; breaking 409; duplicate 409
- `TestAcceptancePublishCompatBreaking` — odebrání entity z package blokuje publish; additive OK

## Mimorozsah

- `force` breaking import
- Diff UI / preview endpoint
- Plná historie constraints v `entity_revision` (export constraintů z pinuté revize je dnes z current `property_profile`)
