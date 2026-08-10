# Knowledge Core — provozní runbook

## Instalace

```bash
helm upgrade --install kc oci://ghcr.io/l-ra/knowledge-core --version 0.1.0 \
  --namespace knowledge-core --create-namespace \
  --set image.tag=0.1.0
```

Lokálně z repozitáře:

```bash
helm upgrade --install kc deploy/helm/knowledge-core \
  --set image.repository=ghcr.io/l-ra/knowledge-core \
  --set image.tag=latest
```

## Bootstrap admin

Default auth mode v Helm je `bootstrap`.

1. Heslo se vygeneruje při prvním startu podu a **jednou** zaloguje:
   ```bash
   kubectl logs -n knowledge-core deploy/kc-knowledge-core | grep bootstrap
   ```
2. Heslo je na PVC: `/data/admin.password`
3. Reset:
   ```bash
   kubectl exec -n knowledge-core deploy/kc-knowledge-core -- /knowledge-core admin reset-password
   ```
4. API volání:
   ```bash
   curl -H "Authorization: Bearer <password>" http://127.0.0.1:8080/v1/packages
   ```

## Pocket ID (OIDC)

### Lokální compose

```bash
podman-compose -f deploy/docker-compose.yml up -d
# App startuje v bootstrap režimu. Pocket ID: http://pocket-id.localhost:1411/setup
```

1. V Pocket ID vytvoř public PKCE klienta (client_id z IdP — po create immutable).
2. Redirect: `http://localhost:8080/ui/callback`
3. V KC UI: **Admin → OIDC / IdP** — vlož issuer + client_id (`PUT /v1/admin/auth`).
4. Runtime nastavení je v tabulce `auth_runtime` (přepíše env při startu).

### Helm

```bash
helm upgrade --install kc deploy/helm/knowledge-core \
  --set pocketId.enabled=true \
  --set pocketId.appUrl=https://id.example.com \
  --set pocketId.adminApiKey=<api-key-from-pocket-id> \
  --set pocketId.oidcClient.callbackURLs[0]=https://app.example.com/callback \
  --set auth.oidcAudience=knowledge-core
```

Když je nastavený `pocketId.adminApiKey`, post-install Job zaregistruje OIDC klienta
`POST /api/oidc/clients` v Pocket ID (idempotentní — existující klient = OK).

Bez API klíče vytvořte klienta ručně v Pocket ID UI s client id = `auth.oidcAudience`.

## PostgreSQL backup

Bundled PostgreSQL používá PVC `*-postgresql-data`.

```bash
# Logical dump
kubectl exec -n knowledge-core sts/kc-postgresql -- \
  pg_dump -U kc knowledge_core > knowledge_core-$(date +%F).sql

# Restore (příklad)
kubectl exec -i -n knowledge-core sts/kc-postgresql -- \
  psql -U kc knowledge_core < knowledge_core-YYYY-MM-DD.sql
```

Doporučení: VolumeSnapshot / externí backup řešení pro PVC; dump před upgradem chartu.

## Outbox worker

CronJob běží každou minutu (`outboxWorker.schedule`):

```bash
kubectl get cronjob -n knowledge-core
kubectl create job --from=cronjob/kc-knowledge-core-outbox outbox-manual -n knowledge-core
```

Ručně:

```bash
kubectl exec -n knowledge-core deploy/kc-knowledge-core -- \
  /knowledge-core outbox process --limit 100
```

## Health a diagnostika

```bash
kubectl get pods -n knowledge-core
curl -sS http://127.0.0.1:8080/healthz
kubectl logs -n knowledge-core deploy/kc-knowledge-core --tail=100
```

## Hardening

| Value | Default | Popis |
|-------|---------|-------|
| `podDisruptionBudget.enabled` | `true` | PDB `minAvailable: 1` |
| `networkPolicy.enabled` | `false` | Omezí ingress:8080 a egress na PG/OIDC/DNS/HTTP(S) |
| `resources.*` | requests/limits nastaveny | CPU/memory |

Zapnutí NetworkPolicy:

```bash
--set networkPolicy.enabled=true
```

## Upgrade / release

CI na tagu `v*` publikuje image `ghcr.io/l-ra/knowledge-core:<version>` a chart stejné verze.

```bash
git tag v0.2.0 && git push origin v0.2.0
helm upgrade kc oci://ghcr.io/l-ra/knowledge-core --version 0.2.0
```

## Obnova po havárii

1. Obnovit PostgreSQL (dump nebo PVC snapshot)
2. Obnovit PVC `/data` (bootstrap heslo) — jinak se vygeneruje nové heslo
3. `helm upgrade --install` se stejným release name
4. `POST /v1/projections/search/rebuild` a `POST /v1/projections/rdf/rebuild` pokud projekce nejsou aktuální
