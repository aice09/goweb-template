# Getting Started

This guide covers prerequisites, local installation, configuration, optional integrations, Kubernetes deployment, and the normal upgrade workflow.

> **Maturity notice:** This repository is a starter project. YAML and configuration are examples, not proof of a successful production deployment. Validate the Go build, container images, rendered manifests, integration behavior, security settings, and restore procedure in a non-production environment first.

## 1. What is included

- Go/Fiber CRUD web application and browser UI
- PostgreSQL database, Redis cache, and NATS/JetStream messaging configuration
- Docker Compose for local development
- Kubernetes manifests under `k8s/base/`
- Helm chart under `deploy/helm/fast-crud/`
- Kustomize environment overlays under `deploy/kustomize/overlays/`
- Argo CD Application example under `deploy/argocd/`
- Optional S3-compatible object storage for application files (disabled by default)
- Optional webhook, SMTP email, and Microsoft Teams notifications
- Separate stdio MCP server under `cmd/mcp/`
- PostgreSQL backup and restore scripts under `scripts/`
- Prometheus/Grafana examples and QA scripts

Some integration settings are scaffolding or require provider-side setup. Check [Integrations](INTEGRATIONS.md) and the root README before enabling a feature.

## 2. Prerequisites

### Local Docker Compose

- Docker Engine or Docker Desktop with Docker Compose v2
- Git
- A terminal (Bash is required for the provided backup/restore scripts)
- Open TCP port `3000` for the web application
- Optional: ports `3001` (Grafana), `3002`/`9090` as configured for observability, `9000`/`9001` for MinIO API/console

For source development and tests:

- Go version compatible with `go.mod` (the root README recommends Go 1.23+)
- Node.js/npm only if running browser QA in `qa/`
- `kubectl`, Kustomize (or `kubectl apply -k`), Helm, and an accessible cluster for Kubernetes deployments
- Argo CD installed in the target cluster for GitOps
- `pg_dump`, `pg_restore`, AWS CLI, and `age` for encrypted backups

### Before production

Have a DNS name, TLS termination, a trusted container registry, a managed or backed-up PostgreSQL service, secret management, monitoring/alerting, and a tested recovery plan. Do not expose PostgreSQL, Redis, NATS monitoring, MinIO console, Prometheus, Grafana, MCP stdio, or setup endpoints publicly without appropriate access controls.

## 3. Get the source and configure secrets

```bash
git clone https://github.com/aice09/goweb-template.git goweb-template
cd goweb-template
cp .env.example .env
```

Generate independent random values rather than reusing example credentials:

```bash
openssl rand -base64 48
```

Use generated values for at least `SESSION_SECRET`, `SETUP_TOKEN`, database/Redis passwords, Grafana password, and any webhook or SMTP credentials. The actual environment variable names depend on the feature being configured; see `.env.example`.

**Do not commit `.env`, private keys, age identities, access tokens, or credentials.** Check `.gitignore` before adding any new secret files. For Kubernetes, use an external secret manager, External Secrets Operator, Sealed Secrets, or SOPS-encrypted manifests. Kubernetes Secret values are only base64-encoded by default; configure encryption at rest and restrictive RBAC.

## 4. Run with Docker Compose

Review `docker-compose.yml` and `.env.example`. The Compose file supplies internal service URLs for PostgreSQL, Redis, and NATS. The application is exposed at port `3000`.

```bash
docker compose config
docker compose up -d --build
docker compose ps
docker compose logs -f api
```

Open `http://localhost:3000`. On first run, the login page should direct you to `/setup` if no administrator exists. Enter the configured `SETUP_TOKEN`, set application metadata, and create the first administrator. Then sign in at `/login`.

Check service health:

```bash
curl -i http://localhost:3000/health/live
curl -i http://localhost:3000/health/ready
```

If the first-run setup fails, check `docker compose logs api postgres`, confirm the `SETUP_TOKEN` matches the `.env` value, and verify PostgreSQL is healthy. Do not repeatedly expose or share the setup token.

### Stop and start

```bash
docker compose stop
docker compose start
```

To stop and remove containers while preserving named volumes:

```bash
docker compose down
```

**Do not add `-v` unless you intentionally want to delete the Compose data volumes.** `docker compose down -v` can permanently remove local PostgreSQL, Redis, NATS, MinIO, and observability data.

## 5. Optional integrations in Compose

### S3-compatible application storage / MinIO

S3 support is optional. Leave `S3_ENABLED=false` when the application should not use object storage. To test with local MinIO, configure the `S3_*` variables in `.env` and start the profile:

```bash
docker compose --profile s3 up -d minio
```

MinIO's API is typically available on port `9000` and its console on `9001`. Create the target bucket before using the application. Configure a dedicated access key with only the permissions required for the target bucket. Do not expose the MinIO console to the public Internet. Verify the exact application-level S3 variables in `.env.example` and the integration code, as backup S3 variables and application asset-storage variables may be different.

### Prometheus and Grafana

```bash
docker compose --profile observability up -d
```

The example Grafana port is `3001`; credentials come from the Compose/environment configuration. Change the example password and restrict access. Treat these dashboards as a starting point, not a complete alerting/monitoring setup.

### Webhooks, email, and Microsoft Teams

Set only the credentials for integrations you want to enable. See [Integrations](INTEGRATIONS.md) for setup and security guidance. Use a test endpoint or non-production mailbox/channel first. Confirm notifications work from application logs and the receiving system before relying on them operationally.

## 6. Build and test the source

```bash
gofmt -w ./cmd
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/api
go build ./cmd/mcp
```

If dependency downloads fail, fix access to the Go module proxy or use an approved internal proxy before interpreting build failures as code defects. Run security tools configured by your CI, and review their findings.

Optional browser QA:

```bash
cd qa
npm ci
npx playwright install --with-deps
npx playwright test
```

Run tests against a disposable or staging environment, not production. See `qa/README.md` for the supported test setup.

## 7. Build container images

Build from the repository root:

```bash
docker build -t fast-crud:0.1.0 .
docker build -f Dockerfile.mcp -t fast-crud-mcp:0.1.0 .
```

Run a local smoke test by using Compose, then check the logs and health endpoints. If deploying to Kubernetes, push the API image to a registry reachable by cluster nodes and use an immutable version tag or digest. Never use `latest` as the only production release identifier.

## 8. Kubernetes deployment options

Choose **one** deployment method for a given namespace: raw Kustomize manifests, Helm, or Argo CD GitOps. Avoid applying all three to the same resources because they can compete over ownership.

### Option A — Kustomize base

Review and edit `k8s/base/` first. The examples require a pre-created Secret named `fast-crud-secrets` and a ConfigMap. Confirm the namespace, image, database URL, trusted origins, ingress hostname/TLS, and network policies match your cluster.

```bash
kubectl apply -k k8s/base
kubectl -n fast-crud get deploy,pods,svc,ingress
kubectl -n fast-crud rollout status deployment/fast-crud
kubectl -n fast-crud logs deployment/fast-crud
```

Use the exact namespace and object names in the manifests if you change them. Create the required Secret through your approved secret-management mechanism; do not store production values in the repository.

### Option B — Helm

Review `deploy/helm/fast-crud/values.yaml` and `deploy/helm/fast-crud/README.md`. Set the correct image repository/tag, service endpoints, ingress hostname, TLS settings, resource requests/limits, and Secret references.

```bash
helm lint deploy/helm/fast-crud
helm template fast-crud deploy/helm/fast-crud \
  --namespace fast-crud \
  -f deploy/helm/fast-crud/values.yaml
```

After reviewing the rendered output and creating the required Secret:

```bash
helm upgrade --install fast-crud deploy/helm/fast-crud \
  --namespace fast-crud --create-namespace \
  -f deploy/helm/fast-crud/values.yaml
kubectl -n fast-crud rollout status deployment/fast-crud
```

Use a separate version-controlled values file per environment and keep secrets out of those files.

### Option C — Argo CD GitOps (recommended for managed cluster deployments)

1. Put the project in a Git repository reachable by Argo CD.
2. Update `deploy/argocd/fast-crud-application.yaml` with your repository URL, branch/tag, and desired environment path.
3. Update the Kustomize overlays under `deploy/kustomize/overlays/` with the correct registry, immutable image tag, namespace, hostname, TLS, and non-secret environment configuration.
4. Provision the required Secrets outside Git.
5. Review the automated sync policy. The example enables pruning and self-healing; pruning deletes resources removed from Git, so use a protected branch and review changes carefully.
6. Apply the Argo CD Application manifest:

```bash
kubectl apply -f deploy/argocd/fast-crud-application.yaml
kubectl -n argocd get applications
argocd app get fast-crud-prod
argocd app diff fast-crud-prod
argocd app sync fast-crud-prod
argocd app wait fast-crud-prod --health
```

The last commands require the Argo CD CLI and a configured session. The Application manifest's repo URL is a placeholder until you replace it. Test a new environment in a non-production namespace first.

## 9. Upgrade procedure

Use the same deployment method that owns the release.

### Compose

1. Back up PostgreSQL and any persistent object storage.
2. Pull or check out the intended release tag.
3. Review release notes and `.env.example` changes.
4. Rebuild and start:

```bash
docker compose up -d --build
docker compose ps
docker compose logs --since=10m api
curl -fsS http://localhost:3000/health/ready
```

5. Verify login, list/create/update/delete operations, notifications, and storage if enabled.

### Kubernetes / Helm

1. Build, test, scan, and push a new immutable image tag.
2. Back up the database and confirm restore readiness.
3. Update the image tag in the chosen values/overlay.
4. For Helm, use `helm diff upgrade` if the plugin is installed, then `helm upgrade --install ...`.
5. For Kustomize/Argo CD, commit and review the manifest change, then let Argo CD reconcile it.
6. Watch rollout status, events, logs, and health endpoints.
7. Perform a smoke test before announcing completion.

### Rollback

- **Compose:** check out the previous release and image tag, then run `docker compose up -d` with the previous image/configuration. Restore the database only if the release introduced an incompatible or destructive schema/data change and after assessing the consequences.
- **Helm:** inspect history with `helm history fast-crud -n fast-crud`, then `helm rollback fast-crud <REVISION> -n fast-crud`.
- **Argo CD:** revert the Git commit to the last known-good version and let GitOps reconcile. Avoid manual live-cluster edits that leave Git and the cluster out of sync.
- Rollback application code does not automatically roll back database data. Use backward-compatible migrations and test rollback procedures before production.

## 10. Backup and restore

The supplied scripts are for PostgreSQL. They do not automatically back up every named volume, Redis data, NATS JetStream state, MinIO assets, Kubernetes Secrets, or GitOps configuration.

See [Operations and Recovery](OPERATIONS.md) for prerequisites, encrypted backup commands, restore testing, retention, and incident recovery. A backup is not proven until you have restored it into an isolated recovery environment.

## 11. Troubleshooting

| Symptom | Checks |
|---|---|
| App does not start | `docker compose ps`; `docker compose logs api`; check required env vars and DB connectivity |
| PostgreSQL unhealthy | Check credentials, disk space, logs, and volume permissions |
| Setup page loops/fails | Confirm `SETUP_TOKEN`; check API logs and database readiness |
| Kubernetes `ImagePullBackOff` | Verify image path/tag, registry credentials, and node access |
| Kubernetes `CrashLoopBackOff` | `kubectl describe pod`, container logs, Secret/ConfigMap names, and health probe paths |
| Ingress returns 404/502 | Check ingress controller, host rule, service port, DNS, and TLS secret |
| Argo CD OutOfSync | `argocd app diff`; check repository path, branch, rendered manifests, and secret dependencies |
| S3 requests fail | Confirm S3 is enabled, bucket exists, endpoint/region/path-style settings match the provider, and credentials have least-privilege access |
| Webhook/email/Teams not delivered | Check endpoint URL, outbound egress, TLS, SMTP credentials, provider logs, rate limits, and app logs |
| MCP client cannot connect | MCP is stdio: configure the client to launch the MCP process locally or in its supported execution environment; do not expose raw stdio as a public HTTP endpoint |

## 12. Production readiness checklist

- [ ] Build, unit tests, race tests, vet, and security scans pass in CI.
- [ ] Immutable image tags/digests and a documented release process are used.
- [ ] TLS is enabled; secure cookies are enabled behind HTTPS.
- [ ] Secrets are externalized and access is least-privilege.
- [ ] Database backup schedule, encryption keys, retention, and restore test are documented.
- [ ] PostgreSQL, Redis, NATS, and S3/MinIO persistence and recovery are addressed.
- [ ] Monitoring, logs, alerts, and resource limits are configured.
- [ ] Ingress and network policies are tested; internal service ports are not publicly exposed.
- [ ] S3, webhook, SMTP, Teams, and MCP are disabled unless explicitly configured and tested.
- [ ] Argo CD repository access, branch protections, pruning policy, and rollback procedure are reviewed.
- [ ] SSO/LDAP/MFA configuration is not assumed functional unless the corresponding provider flows are implemented and tested.
- [ ] Load, security, accessibility, and recovery tests pass in staging.
