# Zadání — Fáze 5: Authorization

**Status:** hotovo (A4/A5/A6)

## Cíl

Centrální authorization na každém read/write path. Default deny. RBAC + omezený ABAC. OIDC pro produkci, dev headers pro lokální vývoj.

## Scope

**In:**

- Tabulka `auth_policy` (deklarativní JSON dokumenty)
- Bootstrap admin role / subject (`KC_BOOTSTRAP_ADMIN_SUBJECT`)
- Dev auth: `X-Subject`, `X-Roles`, volitelně `X-Subject-Attributes` (JSON)
- OIDC JWT (`KC_OIDC_ISSUER`, `KC_OIDC_AUDIENCE`) — volitelné
- Operace: `discover`, `read`, `create`, `update`, `delete`, `manage`
- Granularita: entity, property (v kontextu entity), package
- Read filtering: bez `discover` → 404; bez `read` na property → skryté ve výsledcích / 404
- Write: celá operace zamítnuta pokud není autorizována (A7 overlap)
- Acceptance A4, A5, A6

## Policy dokument (JSON)

```json
{
  "effect": "allow",
  "operations": ["discover", "read", "update"],
  "roles": ["entity-editor"],
  "subjects": ["U42"],
  "resource": { "type": "entity", "publicId": "Q100" },
  "properties": ["P_name", "P_owner"]
}
```

Precedence: default deny → explicit allow → explicit deny wins.

## Acceptance

| ID | Scénář | Status |
|----|--------|--------|
| A4 | Partial property update nesmí smazat skrytá statement data | done |
| A5 | Property-level read filter | done |
| A6 | Bez discover → 404 jako neexistující entity | done |
