# KC Base (data, ne jádro)

Znovupoužitelný foundation package pro metamodely v knowledge-core. **Není** součástí Go služby.

| Soubor | Účel |
|--------|------|
| [catalog.json](catalog.json) | Seed: `instanceOf`, `usageGuidance` / `usageExamples`, `StringEnum` + shape |
| [build_bundle.py](build_bundle.py) | Sestaví portable release bundle |
| [releases/kc-base-1.0.0.bundle.json](releases/kc-base-1.0.0.bundle.json) | Bundle pro UI import |
| [load.py](load.py) | Nahraje catalog přes API |

## Obsah

- **`instanceOf`** — globální typing property pro `schema-config.instanceOfProperty`
- **`usageGuidance` / `usageExamples`** — anotace na třídách a properties libovolného slovníku
- **`StringEnum`** + `enumeratesProperty` / `allowedValue` + shape `string-enum` — mechanismus enumů (konkrétní hodnoty zůstávají v doménovém package)

## Import

1. Admin UI: **Packages → Import release bundle**
2. Nahrajte `releases/kc-base-1.0.0.bundle.json`
3. Pokud je `instanceOfProperty` prázdné, nastavte ho na `https://knowledge-core.local/kc-base/instanceOf`

```bash
python3 models/kc-base/build_bundle.py
export KC_BASE_URL=http://localhost:8080
export KC_TOKEN='…'
python3 models/kc-base/load.py
```
