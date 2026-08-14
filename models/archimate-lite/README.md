# ArchiMate Lite (data, ne jádro)

Metamodel pro popis systémů v knowledge-core. **Není** součástí Go služby.

| Soubor | Účel |
|--------|------|
| [catalog.json](catalog.json) | SoT tříd, vlastností, tvarů, lite matice vazeb |
| [load.py](load.py) | Nahraje catalog do běžícího KC přes `/v1` |
| [docs/models/archimate-lite.md](../../docs/models/archimate-lite.md) | Granularita L0–L4 |
| [docs/models/archimate-lite-kc.md](../../docs/models/archimate-lite-kc.md) | Kontrakt pro nástroje nad API |

```bash
export KC_BASE_URL=http://localhost:8080
export KC_TOKEN='…'
python3 models/archimate-lite/load.py
```
