# Helm deployment

Chart: `deploy/helm/knowledge-core`

Includes optional subcharts:

- **postgresql** — PostgreSQL 16 with generated credentials (enabled by default)
- **pocket-id** — Pocket ID OIDC provider (disabled by default)

## Quick install (local cluster)

```bash
# Build image locally (optional — CI publishes ghcr.io/l-ra/knowledge-core)
docker build -f deploy/Dockerfile -t ghcr.io/l-ra/knowledge-core:dev .

# Install with bootstrap admin password (logged once on pod start)
helm upgrade --install kc deploy/helm/knowledge-core \
  --set image.repository=ghcr.io/l-ra/knowledge-core \
  --set image.tag=dev \
  --set image.pullPolicy=IfNotPresent

kubectl port-forward svc/kc-knowledge-core 8080:8080

# Initial admin password
kubectl logs deploy/kc-knowledge-core | grep bootstrap

# API call
curl -H "Authorization: Bearer <password>" http://127.0.0.1:8080/v1/packages
```

## Reset bootstrap admin password

```bash
kubectl exec -it deploy/kc-knowledge-core -- /knowledge-core admin reset-password
```

## With Pocket ID (OIDC)

```bash
helm upgrade --install kc deploy/helm/knowledge-core \
  --set pocketId.enabled=true \
  --set pocketId.appUrl=https://id.example.com
```

When `pocketId.enabled=true`, Knowledge Core switches to `KC_AUTH_MODE=oidc` and uses Pocket ID as issuer.

Configure an OIDC client in Pocket ID with client id `knowledge-core` (`auth.oidcAudience`).

## CI

GitHub Actions workflow `.github/workflows/ci.yml`:

- Go tests (unit + acceptance with Postgres service)
- Docker build
- `helm lint` + template smoke test
- On push to `main`: publish image to `ghcr.io/l-ra/knowledge-core` and Helm chart to `oci://ghcr.io/l-ra`

Install published chart:

```bash
helm install kc oci://ghcr.io/l-ra/knowledge-core --version 0.1.0
```

## Values reference

| Value | Default | Description |
|-------|---------|-------------|
| `auth.mode` | `bootstrap` | `bootstrap`, `dev`, or `oidc` (overridden when Pocket ID enabled) |
| `auth.bootstrapAdminSubject` | `admin` | Subject id with admin role |
| `postgresql.enabled` | `true` | Deploy bundled PostgreSQL |
| `pocketId.enabled` | `false` | Deploy Pocket ID subchart |
| `persistence.enabled` | `true` | PVC for bootstrap password file (`/data`) |
| `outboxWorker.enabled` | `true` | CronJob running `knowledge-core outbox process` |
| `outboxWorker.schedule` | `*/1 * * * *` | Cron schedule |

## Outbox worker

```bash
# Manual
kubectl exec -it deploy/kc-knowledge-core -- /knowledge-core outbox process --limit 100

# Or rely on CronJob (enabled by default)
kubectl get cronjob
```

