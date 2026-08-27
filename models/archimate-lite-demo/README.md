# ArchiMate Lite 2.2.0 — demo instance

Akceptační seed (Compliance + síť) zapsatelný **výhradně** proti `archimate-lite` 2.2.0.

```bash
export KC_BASE_URL=http://localhost:8080
export KC_TOKEN='…'
python3 models/archimate-lite-demo/load.py
```

Loader nejdřív nahraje `kc-base` a `archimate-lite` 2.2.0.
