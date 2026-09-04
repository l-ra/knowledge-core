# Models moved

Domain model packages (catalogs, release bundles, loaders) live in the sibling repository:

**[`../knowledge-models`](../../knowledge-models)** (`/home/rasekl/src/knowledge-models`)

Checkout next to this repo:

```text
src/knowledge-core/
src/knowledge-models/
```

Override path for tests/CI with `KNOWLEDGE_MODELS_PATH`.

Import order (new installs):

1. `kc-base` 1.1.0
2. `archimate-lite` 3.0.0
3. `archimate-ui-traversal` 1.0.0

See knowledge-models `README.md` and `migrations/` for UI IRI remaps from archimate-lite 2.3.1.
