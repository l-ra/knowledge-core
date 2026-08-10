# Po instalaci — jak začít zadávat data

Knowledge Core je **default deny**. Bez oprávnění dostaneš `403 forbidden` na create/list/update.
Tento dokument popisuje, co udělat hned po instalaci, aby šlo pracovat s daty.

## Rychlá mapa

| Stav | Kdo může zapisovat data |
|------|-------------------------|
| `KC_AUTH_MODE=bootstrap` | Bootstrap admin (heslo) — plná práva |
| `KC_AUTH_MODE=dev` | Subject s rolí `admin` v `X-Roles`, nebo subject = `KC_BOOTSTRAP_ADMIN_SUBJECT` |
| `KC_AUTH_MODE=oidc` | JWT subject s rolí `admin`, **nebo** subject ID = `KC_BOOTSTRAP_ADMIN_SUBJECT`, **nebo** explicitní `auth_policy` |

Vestavěná policy `bootstrap-admin` povoluje jen roli **`admin`**. Obyčejný OIDC uživatel z Pocket ID tuto roli typicky **nemá**.

---

## A) Compose / lokální start (doporučený postup)

### 1. Spusť stack

```bash
podman-compose -f deploy/docker-compose.yml up -d --build
# UI:        http://localhost:8080/ui/
# Pocket ID: http://pocket-id.localhost:1411
```

App startuje v režimu **bootstrap**. Heslo najdeš v logu při prvním startu:

```bash
podman logs knowledge-core_app_1 2>&1 | grep -i bootstrap
```

Soubor: `/data/admin.password` v kontejneru.

### 2. Přihlášení bootstrapem

1. Otevři http://localhost:8080/ui/
2. Zadej bootstrap heslo
3. Jsi **admin** → můžeš hned vytvářet packages, properties, entity, statements

V této fázi už můžeš zadávat data. OIDC zatím není nutný.

### 3. (Volitelně) Napoj Pocket ID

1. http://pocket-id.localhost:1411/setup — vytvoř uživatele
2. V Pocket ID vytvoř **public PKCE** klienta  
   Redirect URI: `http://localhost:8080/ui/callback`  
   (`client_id` po vytvoření nelze změnit — zkopíruj ho)
3. V KC: **Admin → OIDC / IdP** — issuer + client_id → Save
4. Přihlas se přes OIDC

### 4. Po prvním OIDC loginu dostaneš `forbidden` — to je očekávané

OIDC `sub` ≠ bootstrap subject `admin` a JWT obvykle neobsahuje `roles: ["admin"]`.

Zjisti své ID:

```bash
# v UI: sidebar ukazuje „Signed in: <id>“
# nebo:
curl -H "Authorization: Bearer <id_token>" http://localhost:8080/v1/me
```

Pak zvol **jednu** z metod níže.

#### Metoda 1 — bootstrap admin subject (nejrychlejší pro single-admin)

Nastav OIDC `sub` jako bootstrap admin a restartuj app:

```yaml
# deploy/docker-compose.yml → service app → environment:
KC_BOOTSTRAP_ADMIN_SUBJECT: "<sub-z-/v1/me>"
```

```bash
podman-compose -f deploy/docker-compose.yml up -d --force-recreate app
```

Tento subject má opět plná práva (jako admin).

#### Metoda 2 — policy pro konkrétní subject (bez restartu env)

Dočasně se vrať na bootstrap (SQL), vytvoř policy, znovu zapni OIDC:

```bash
# 1) Přepni runtime auth zpět na bootstrap
podman exec -i knowledge-core_postgres_1 \
  psql -U kc -d knowledge_core -c \
  "UPDATE auth_runtime SET auth_mode='bootstrap', oidc_issuer='', oidc_client_id='', oidc_audience='' WHERE id=1;"

podman-compose -f deploy/docker-compose.yml restart app
```

Přihlas se bootstrap heslem → **Model → Policies** (nebo API):

```bash
curl -X PUT http://localhost:8080/v1/policies/oidc-operators \
  -H "Authorization: Bearer <bootstrap-password>" \
  -H "Content-Type: application/json" \
  -d '{
    "priority": 1000,
    "document": {
      "effect": "allow",
      "operations": ["manage"],
      "subjects": ["<sub-z-/v1/me>"]
    }
  }'
```

Znovu napoj OIDC v **Admin → OIDC / IdP** (stejný issuer + client_id).

#### Metoda 3 — role `admin` v tokenu

Pokud IdP umí posílat claim `roles` (pole stringů) a obsahuje `"admin"`, KC ti dá plná práva automaticky.
Pocket ID to out-of-the-box typicky **nedělá** — spolehlivější jsou metody 1 nebo 2.

---

## B) Helm

1. Default je `bootstrap` — heslo z logu / PVC (`deploy/RUNBOOK.md`).
2. S heslem můžeš hned volat API / používat UI jako admin.
3. Po zapnutí Pocket ID / OIDC stejně jako výše: nastav `auth.bootstrapAdminSubject` na OIDC `sub`, **nebo** vytvoř `auth_policy` se `subjects: ["<sub>"]` a `operations: ["manage"]`.

```bash
helm upgrade kc deploy/helm/knowledge-core \
  --reuse-values \
  --set auth.bootstrapAdminSubject="<oidc-sub>"
```

---

## Minimální data po získání práv

1. **Model → Packages** — vytvoř package (nebo použij default, pokud existuje)
2. **Model → Properties** — vytvoř property (např. datatype `String`)
3. **Data → Entities** — vytvoř entitu
4. Otevři entitu — přidej statement (property + hodnota)

Bez package/property často nejde smysluplně editovat statements v UI.

---

## Diagnostika `forbidden`

| Kontrola | Co očekáváš |
|----------|-------------|
| `GET /v1/me` | `id` + případně `roles` |
| Auth mode | `/v1/ui/config` → `authMode` |
| Jsi admin? | `roles` obsahuje `admin`, **nebo** `id` == `KC_BOOTSTRAP_ADMIN_SUBJECT`, **nebo** existuje allow policy na tvůj `id` |
| Policy list | `GET /v1/policies` (vyžaduje už nějaké právo / bootstrap) |

`401 unauthenticated` = špatný/chybějící token.  
`403 forbidden` = jsi přihlášen, ale ACL tě pustí dál.
