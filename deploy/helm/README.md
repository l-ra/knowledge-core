# Helm deployment

Chart: `deploy/helm/knowledge-core`

Includes optional subcharts:

- **postgresql** — PostgreSQL 16 with generated credentials (enabled by default)
- **pocket-id** — Pocket ID OIDC provider (disabled by default)
- **pgadmin** — pgAdmin 4 web UI for PostgreSQL (disabled by default, tag 9.16)

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
  --set pocketId.appUrl=https://id.example.com \
  --set pocketId.adminApiKey=<api-key> \
  --set pocketId.oidcClient.callbackURLs[0]=https://app.example.com/callback
```

When `pocketId.enabled=true`, Knowledge Core switches to `KC_AUTH_MODE=oidc` and uses Pocket ID as issuer.

If `pocketId.adminApiKey` is set, a Helm hook Job registers OIDC client id `auth.oidcAudience` via Pocket ID API.
Otherwise configure the client manually in Pocket ID UI.

## With pgAdmin (PostgreSQL UI)

```bash
helm upgrade --install kc deploy/helm/knowledge-core \
  --set pgAdmin.enabled=true \
  --set postgresql.enabled=true

kubectl port-forward svc/kc-pgadmin 5050:80

# Login email: pgAdmin.auth.email (default admin@example.com)
kubectl get secret kc-pgadmin -o jsonpath='{.data.password}' | base64 -d; echo
```

When enabled, pgAdmin pre-registers the bundled PostgreSQL server (`kc` user). Password is read from the postgresql subchart secret unless `pgAdmin.postgres.password` is set.

## Hardening

- PDB enabled by default (`podDisruptionBudget.enabled`)
- Resource requests/limits set in `values.yaml`
- Optional NetworkPolicy: `--set networkPolicy.enabled=true`

## CI

GitHub Actions workflow `.github/workflows/ci.yml`:

- Go tests (unit + acceptance with Postgres service)
- Docker build
- `helm lint` + template smoke test
- On push to `main`: publish image to `ghcr.io/l-ra/knowledge-core` and dev Helm chart to `oci://ghcr.io/l-ra`

Tag release (`.github/workflows/release.yml`):

```bash
git tag v0.2.0 && git push origin v0.2.0
helm install kc oci://ghcr.io/l-ra/knowledge-core --version 0.2.0
```

## Versioning

| Artifact | Source of truth | `main` branch | Tag `vX.Y.Z` release |
|----------|-----------------|---------------|----------------------|
| Container image | Git tag | `latest` + commit SHA | `X.Y.Z`, `X.Y`, `latest` |
| Helm chart `version` | Git tag (release) / CI run (dev) | `0.0.0-dev.<run>` | `X.Y.Z` |
| Helm chart `appVersion` | Same as chart `version` for releases | dev version | `X.Y.Z` |
| Default image tag in chart | `appVersion` when `image.tag` is empty | dev version | `X.Y.Z` |

Subchart `version` fields (`postgresql`, `pocket-id`, `pgadmin`) version the bundled dependency packages only; they are **not** bumped on application release.

### Release flow

1. Create and push a SemVer tag: `git tag v1.2.0 && git push origin v1.2.0`
2. `release.yml` runs:
   - sets `Chart.yaml` `version` and `appVersion` via `deploy/helm/scripts/set-chart-version.sh`
   - runs tests, builds and pushes the image with SemVer tags
   - packages the chart and pushes to `oci://ghcr.io/l-ra/knowledge-core`
   - creates a GitHub Release with the `.tgz` artifact
   - commits the same `Chart.yaml` version back to `main` (so the repo reflects the latest release)

You do **not** need to manually edit `Chart.yaml` before tagging.

### Dev chart on `main`

Each push to `main` publishes a chart with version `0.0.0-dev.<run_number>` so it never overwrites a SemVer release in the OCI registry. Use tagged releases for production installs.

```bash
# Production
helm upgrade --install kc oci://ghcr.io/l-ra/knowledge-core --version 1.2.0

# Latest dev (optional — version changes every CI run)
helm upgrade --install kc oci://ghcr.io/l-ra/knowledge-core --version 0.0.0-dev.42
```

## Values reference

| Value | Default | Description |
|-------|---------|-------------|
| `auth.mode` | `bootstrap` | `bootstrap`, `dev`, or `oidc` (overridden when Pocket ID enabled) |
| `auth.bootstrapAdminSubject` | `admin` | Subject id with admin role |
| `postgresql.enabled` | `true` | Deploy bundled PostgreSQL |
| `pocketId.enabled` | `false` | Deploy Pocket ID subchart |
| `pgAdmin.enabled` | `false` | Deploy pgAdmin 4 subchart (requires postgresql) |
| `pocketId.adminApiKey` | `""` | Enables OIDC client registration Job |
| `persistence.enabled` | `true` | PVC for bootstrap password file (`/data`) |
| `outboxWorker.enabled` | `true` | CronJob running `knowledge-core outbox process` |
| `outboxWorker.schedule` | `*/1 * * * *` | Cron schedule |
| `podDisruptionBudget.enabled` | `true` | PDB |
| `networkPolicy.enabled` | `false` | Restrict pod network |

## Outbox worker

```bash
# Manual
kubectl exec -it deploy/kc-knowledge-core -- /knowledge-core outbox process --limit 100

# Or rely on CronJob (enabled by default)
kubectl get cronjob
```

See also [RUNBOOK.md](../RUNBOOK.md).

