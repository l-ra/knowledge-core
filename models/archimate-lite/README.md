# ArchiMate Lite (data, ne jádro)

Metamodel pro popis systémů v knowledge-core. **Není** součástí Go služby.  
Závisí na package **`kc-base`** (typing, usage anotace, StringEnum mechanismus).

Po změně identity v KC jsou public ID **plné IRI** (`iriBase` + `iriLocal`), ne `Q*`/`C*`/`P*`.

| Soubor | Účel |
|--------|------|
| [catalog.json](catalog.json) | Seed slovníku v gitu (třídy, properties, tvary, matice, enum hodnoty, exchange) |
| [build_bundle.py](build_bundle.py) | Sestaví portable release bundle z catalogu (resolvuje IRI z kc-base) |
| [releases/archimate-lite-2.2.0.bundle.json](releases/archimate-lite-2.2.0.bundle.json) | Bundle pro UI import (aktuální) |
| [releases/archimate-lite-2.1.0.bundle.json](releases/archimate-lite-2.1.0.bundle.json) | Předchozí release |
| [releases/archimate-lite-2.0.0.bundle.json](releases/archimate-lite-2.0.0.bundle.json) | Baseline pro additive upgrade |
| [load.py](load.py) | Nahraje catalog + nejdřív `kc-base` přes API |
| [docs/models/archimate-lite.md](../../docs/models/archimate-lite.md) | Granularita L0–L4 |
| [docs/models/archimate-lite-kc.md](../../docs/models/archimate-lite-kc.md) | Kontrakt pro nástroje nad API |
| [../archimate-lite-demo/](../archimate-lite-demo/) | Compliance + network demo instance (2.2.0) |
| [../kc-base/](../kc-base/) | Foundation package |

## Import přes UI (doporučeno)

1. Nejdřív importujte [`kc-base` 1.1.0](../kc-base/releases/kc-base-1.1.0.bundle.json)
2. Pak **Packages → Import release bundle** → `releases/archimate-lite-2.2.0.bundle.json`
3. Pokud je `instanceOfProperty` v schema-config prázdné, nastavte ho na IRI z **kc-base**:  
   `https://knowledge-core.local/kc-base/instanceOf`

Přegenerování bundle:

```bash
python3 models/kc-base/build_bundle.py
python3 models/archimate-lite/build_bundle.py
```

## Load přes API (authoring / DEV)

Loader ArchiMate Lite automaticky nahraje i `kc-base`:

```bash
export KC_BASE_URL=http://localhost:8080
export KC_TOKEN='…'
python3 models/archimate-lite/load.py
```
