# Knowledge Core — integrační balíček

Tento balíček je určen pro **architekty** a **vývojáře klientských aplikací**, kteří nad Knowledge Core staví doménové modely a nástroje. Neobsahuje interní zadání fází ani ADR — ty zůstávají v [docs/README.md](../README.md).

## Co je Knowledge Core

Lehké knowledge-graph jádro (Wikibase-inspired): entity–property–value model, historie změn, verzované packages, deklarativní lenses a centrální authorization. Klientské aplikace pracují přes HTTP API `/v1/*`; doménová logika a typy **nejsou** součástí Go služby — žijí v **packages** (datech v KC).

## Pořadí čtení

| # | Dokument | Úroveň | Pro koho |
|---|----------|--------|----------|
| 1 | [concepts.md](concepts.md) | Přehled | Architekt, vývojář |
| 2 | [client-guide.md](client-guide.md) | Implementace | Vývojář |
| 3 | [api-contract.md](api-contract.md) | Implementace | Vývojář |
| 4 | [../ops/post-install.md](../ops/post-install.md) | Provoz | DevOps, první zápis |
| 5 | [../../../knowledge-models/docs/archimate-lite-kc.md](../../../knowledge-models/docs/archimate-lite-kc.md) | Referenční příklad | Vývojář doménového nástroje |

## Artefakty pro integraci

| Artefakt | Cesta | Účel |
|----------|-------|------|
| OpenAPI | [`api/openapi.yaml`](../../api/openapi.yaml) | Katalog endpointů + schémata request/response |
| HTTP příklady | [`examples.http`](examples.http) | Spustitelné requesty (REST Client / IntelliJ / VS Code) |
| Referenční loader | [`archimate-lite/load.py`](../../../knowledge-models/archimate-lite/load.py) in knowledge-models | Idempotentní bootstrap metamodelu z JSON katalogu |
| Seed katalog | [`archimate-lite/catalog.json`](../../../knowledge-models/archimate-lite/catalog.json) | Ukázka struktury doménového slovníku |
| Post-install | [../ops/post-install.md](../ops/post-install.md) | Auth, bootstrap admin, OIDC, první oprávnění |

## Dvouúrovňová struktura

```text
Úroveň 1 — Přehled (architekt)
  concepts.md
    → co je graf, package, statement, lens, release
    → co KC neřeší (ArchiMate typy, doménová pravidla)

Úroveň 2 — Implementace (vývojář klienta)
  client-guide.md + api-contract.md + examples.http + openapi.yaml
    → postup vytvoření vlastního package
    → JSON kontrakty, auth, chyby, value typy
    → referenční implementace: archimate-lite
```

## Rychlý start

```bash
# 1. Spusť KC (viz root README.md)
export KC_BASE_URL=http://localhost:8080
export KC_TOKEN='<bootstrap-heslo>'

# 2. Ověř auth
curl -s -H "Authorization: Bearer $KC_TOKEN" "$KC_BASE_URL/v1/me"

# 3. Nahraj referenční metamodel (volitelně — ukázka celého workflow)
python3 ../knowledge-models/archimate-lite/load.py

# 4. Prozkoumej API příklady
#    docs/integration/examples.http
```

## Hranice odpovědností

| Vrstva | Kde | Příklad |
|--------|-----|---------|
| **Jádro KC** | Go služba, `/v1/*` | Entity, statement, package, shape, lens engine |
| **Metamodel package** | data v KC | Třídy `C*`, properties `P*`, tvary, policy data |
| **Instance packages** | data v KC | Konkrétní systémy, vazby, views |
| **Klientský nástroj** | mimo KC | Editor, export XML, validace doménových pravidel |

ArchiMate Lite ilustruje všechny čtyři vrstvy — viz [archimate-lite-kc.md](../../../knowledge-models/docs/archimate-lite-kc.md).

## Související interní dokumentace

Pro hlubší pochopení implementace jádra (není nutné pro integraci):

- [concepts/data-model.md](../concepts/data-model.md) — ER diagram PostgreSQL
- [design/knowledge_core_v1_technicky_navrh.md](../design/knowledge_core_v1_technicky_navrh.md) — plný technický návrh
