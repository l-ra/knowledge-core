# ArchiMate Lite (data, ne jádro)

Metamodel pro popis systémů v knowledge-core. **Není** součástí Go služby.

Po změně identity v KC jsou public ID **plné IRI** (`iriBase` + `iriLocal`), ne `Q*`/`C*`/`P*`.

| Soubor | Účel |
|--------|------|
| [catalog.json](catalog.json) | Seed slovníku v gitu (třídy, properties, tvary, matice, enumy, exchange) |
| [build_bundle.py](build_bundle.py) | Sestaví portable release bundle z catalogu |
| [releases/archimate-lite-1.2.0.bundle.json](releases/archimate-lite-1.2.0.bundle.json) | Bundle pro UI import (`Packages → Import release bundle`) |
| [load.py](load.py) | Alternativa: nahraje catalog přes API (authoring); po loadu je SoT v KC |
| [docs/models/archimate-lite.md](../../docs/models/archimate-lite.md) | Granularita L0–L4 |
| [docs/models/archimate-lite-kc.md](../../docs/models/archimate-lite-kc.md) | Kontrakt pro nástroje nad API |

## Import přes UI (doporučeno)

1. V Admin UI: **Packages → Import release bundle**
2. Nahrajte `releases/archimate-lite-1.2.0.bundle.json`
3. Pokud je `instanceOfProperty` v schema-config prázdné, nastavte ho na IRI property `instanceOf` z tohoto package (`https://knowledge-core.local/archimate-lite/instanceOf`)

Přegenerování bundle:

```bash
python3 models/archimate-lite/build_bundle.py
```

Skript automaticky najde starší bundle v `releases/` a u změněného obsahu zvýší `revisionNo` (nutné pro upgrade import DEV→PROD).

## Load přes API (authoring / DEV)

```bash
export KC_BASE_URL=http://localhost:8080
export KC_TOKEN='…'
python3 models/archimate-lite/load.py
# volitelně publish + export:
# POST /v1/packages/archimate-lite/releases  {"version":"1.2.0"}
# GET  /v1/packages/archimate-lite/releases/1.2.0/bundle
```
