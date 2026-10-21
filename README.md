# goweb-template — Go, PostgreSQL, Redis, NATS

A deployable starter for a CRUD web application with a Bootstrap admin UI, setup wizard, PostgreSQL persistence, Redis read cache, NATS events, security defaults, QA automation, and optional Prometheus/Grafana observability.

## Documentation

[Read the Docs](https://goweb-template.readthedocs.io/)


- [Getting Started](docs/GETTING_STARTED.md) — prerequisites, install, first-run setup, Docker, Kubernetes, Argo CD, upgrade, and troubleshooting.
- [Operations and Recovery](docs/OPERATIONS.md) — health checks, backup/restore, rollback, incident handling, and monitoring.
- [Integrations](docs/INTEGRATIONS.md) — optional S3, webhooks, SMTP email, Teams, MCP, identity providers, Redis/NATS, and GitOps.

## Stack

- Go + Fiber v2 HTTP framework
- PostgreSQL + `pgxpool`
- Redis cache
- NATS messaging (Core NATS in this starter; not guaranteed delivery)
- Bootstrap 5, DataTables Bootstrap 5 integration, Font Awesome
- Go tests, Playwright browser tests, GitHub Actions, govulncheck, gosec, Trivy, Lighthouse CI, k6 example
- Optional Prometheus and Grafana profile

## Quick start

1. Install Docker Compose v2 and Go 1.23+.
2. Copy the environment file and set unique secrets:

   ```bash
   cp .env.example .env
   openssl rand -base64 48
   ```

   Put the generated value into `SESSION_SECRET`. Change `SETUP_TOKEN`, PostgreSQL/Redis passwords, and the Grafana password. For a local setup, also update `.env` values to match the Compose overrides where necessary.

3. Start services:

   ```bash
   docker compose up -d --build
   ```

4. Open `http://localhost:3000`. If no administrator exists, the login page redirects to `/setup`.
5. Enter the `SETUP_TOKEN` value from `.env`, configure the application name, and create the initial administrator.
6. Sign in at `/login`; manage items at `/`.

> The setup wizard configures application metadata and the first admin account. PostgreSQL, Redis, and NATS endpoints are deployment-level configuration supplied through environment variables/secrets. A web wizard should not be allowed to rewrite arbitrary server connection strings or expose database credentials.

## Local Go development

The simplest path is Docker Compose. To run the Go process on the host, update `.env` endpoints to `localhost`, start the dependencies, then:

```bash
set -a && . ./.env && set +a
go mod tidy
go run ./cmd/api
```

## Routes

| Method | Route | Auth | Purpose |
|---|---|---:|---|
| GET | `/setup` | Setup-only | First-run setup page |
| POST | `/api/setup/status` | No | Returns whether setup is needed |
| POST | `/api/setup/complete` | Setup token | Creates first admin and locks setup |
| GET | `/login` | No | Login page |
| POST | `/api/auth/login` | No | Login; sets HttpOnly session cookie |
| POST | `/api/auth/logout` | Yes | Clear session cookie |
| GET | `/api/auth/me` | Yes | Current administrator |
| GET | `/` | Yes | CRUD dashboard |
| GET | `/api/v1/items` | Yes | List with pagination |
| POST | `/api/v1/items` | Yes | Create |
| GET | `/api/v1/items/:id` | Yes | Read one |
| PUT | `/api/v1/items/:id` | Yes | Update |
| DELETE | `/api/v1/items/:id` | Yes | Delete |
| GET | `/health/live` | No | Liveness |
| GET | `/health/ready` | No | PostgreSQL readiness |
| GET | `/metrics` | No | Prometheus text metrics |

## Security model and deployment notes

- First-run setup is locked in the database after successful completion; setup requests require `SETUP_TOKEN`.
- Passwords are hashed with bcrypt; sessions are signed and stored in HttpOnly cookies with SameSite settings.
- Mutating browser requests require a CSRF token header that matches the CSRF cookie.
- Security headers, body limits, request timeouts, basic rate limiting, strict input validation, and parameterized SQL are configured.
- Keep `.env` out of source control. Use Kubernetes Secrets, External Secrets, Vault, or another secret manager in shared environments.
- Terminate TLS at a trusted ingress/reverse proxy and set `COOKIE_SECURE=true`.
- Restrict `/setup`, `/metrics`, Grafana, Prometheus, NATS monitoring, and database ports at the network layer in production.
- The sample has one administrator role. Extend it with explicit RBAC and audit records before supporting multiple user roles.
- Core NATS publishing is best-effort and can lose messages. For durable business events, use JetStream and a transactional outbox with idempotent consumers.
- The starter exposes Prometheus metrics for request counts and duration; add database/Redis/NATS exporters and alert rules before production use.
- Run dependency and security scans in CI. A passing scanner does not guarantee application security.

## UI libraries

The demo loads Bootstrap 5, DataTables with its Bootstrap 5 adapter, and Font Awesome from public CDNs. For a locked-down or production environment, vendor pinned assets locally and apply Subresource Integrity (SRI) to external assets. Confirm current CDN versions before release.

## QA and performance

```bash
go test ./...
go test -race ./...
go vet ./...
govulncheck ./...
gosec ./...
```

Browser tests live under `qa/` and use Playwright. Lighthouse CI configuration is in `lighthouserc.json`; the k6 smoke script is in `qa/k6-smoke.js`. Run performance tests only against a test/staging environment.

## SEO

The public login/setup pages have a title and description. The authenticated CRUD dashboard is marked `noindex`. Add sitemap/robots metadata only if public, indexable pages are introduced. Do not index admin pages or API routes.

## Production checklist

- [ ] Replace every example password and token.
- [ ] Enable TLS and `COOKIE_SECURE=true`.
- [ ] Set trusted origins to exact production origins.
- [ ] Use a managed database or verified backups and test restore procedures.
- [ ] Add migration jobs instead of relying on runtime schema changes.
- [ ] Add durable NATS JetStream + transactional outbox if events must not be lost.
- [ ] Add RBAC, audit trail, MFA/SSO, account recovery, and password policy as needed.
- [ ] Configure alerts, log retention, metrics access controls, and resource limits.
- [ ] Run load, accessibility, and security tests against staging.


## Durable NATS events and audit trail

The application provisions a file-backed JetStream stream (`NATS_STREAM`) for `${NATS_SUBJECT_PREFIX}.>` subjects and waits for JetStream publish acknowledgements. The stream uses a retention window configured by `NATS_MAX_AGE`. Keep the NATS data volume persistent and back it up according to your recovery objectives.

Audit events are written to PostgreSQL `audit_logs` for successful local login, bootstrap, and item create/update/delete actions. Avoid logging passwords, tokens, session cookies, or other secrets. Configure database retention and restricted access for audit records.

**Delivery guarantee caveat:** JetStream persists messages that are successfully acknowledged by the server, but CRUD writes and NATS publishes are not yet committed through a transactional outbox. A process crash between the database commit and the publish can still omit an event. For strict end-to-end delivery, add an outbox row in the same SQL transaction as each CRUD operation and a retrying dispatcher.

## LDAP, Keycloak/OIDC, JWT and MFA configuration status

The `.env.example` documents the variables needed for LDAP, OIDC/Keycloak, JWT, and MFA policy. These settings are **configuration scaffolding only in this starter version**: the current login route still uses the local username/password database and signed session cookie. Do not set `AUTH_MODE=ldap`, `AUTH_MODE=oidc`, or `OIDC_ENABLED=true` expecting those providers to work until the corresponding provider handlers, token validation, callback/state/nonce/PKCE flow, group-to-role mapping, and provider-specific tests have been implemented.

Recommended production approach:
- Prefer Keycloak OIDC for SSO and enforce MFA (TOTP/WebAuthn) in Keycloak authentication flows.
- Use Keycloak as the issuer and validate JWTs against its OIDC discovery/JWKS metadata for API bearer authentication.
- If direct LDAP authentication is mandatory, use LDAPS or verified StartTLS, bind with a least-privileged service account, use a safe configurable user filter, and map groups to application roles.
- Do not implement MFA by trusting a client-supplied boolean environment value; validate the authenticated identity's MFA assurance from the identity provider.

## S3-compatible storage and database backup

`docker compose --profile s3 up -d minio` starts a local S3-compatible service for development. The application does not yet include a browser asset-upload/presigned-URL handler; S3 variables are provided as deployment configuration for that adapter. The backup scripts can use S3-compatible endpoints and the S3 credential variables. Do not expose the MinIO console publicly.

For PostgreSQL backups, install `pg_dump`, `pg_restore`, `age`, and AWS CLI on a trusted backup runner. Use a separate S3 bucket with versioning and server-side encryption, least-privileged credentials, and object-lock/immutability where supported.

Example backup:

```bash
export DATABASE_URL='postgres://backup_user:...@db.example.internal:5432/appdb?sslmode=verify-full'
export S3_BUCKET_BACKUPS='my-private-backup-bucket'
export S3_REGION='ap-southeast-1'
export BACKUP_AGE_RECIPIENT='age1...'
export BACKUP_ENCRYPTION_ENABLED=true
./scripts/backup-postgres.sh
```

Example restore (destructive; use a separate recovery target first):

```bash
export DATABASE_URL='postgres://restore_user:...@recovery-db.example.internal:5432/appdb?sslmode=verify-full'
export S3_BUCKET_BACKUPS='my-private-backup-bucket'
export BACKUP_AGE_IDENTITY_FILE='/run/secrets/backup-age-identity'
export RESTORE_REQUIRE_CONFIRMATION=true
./scripts/restore-postgres.sh 's3://my-private-backup-bucket/postgres/fast-crud-YYYYMMDDTHHMMSSZ.dump.age'
```

Restore procedure:
1. Declare an incident/change and stop writes or place the app in maintenance mode.
2. Provision a separate recovery database with matching PostgreSQL major version.
3. Verify the backup object's provenance and size; decrypt it with the protected age identity.
4. Restore into the recovery database; do not point the script at production until the recovery test passes.
5. Run smoke tests, verify row counts and application login, and check audit/event consistency.
6. Repoint the application only after approval; preserve the previous database for rollback.
7. Record restore duration and the recovered backup timestamp. Run periodic restore drills.

The scripts use custom-format `pg_dump` backups, optional age encryption, S3 upload, and a destructive restore confirmation. They do not configure automated scheduling or retention lifecycle policies; configure those in a protected scheduler and S3 lifecycle policy. Test the scripts in staging before relying on them.


## Docker and Kubernetes

- `Dockerfile` builds the HTTP API container.
- `Dockerfile.mcp` builds the standalone stdio MCP server.
- `k8s/base/` contains Kustomize manifests for namespace, ConfigMap, Secret references, Deployment, Service, Ingress, and a basic ingress NetworkPolicy.
- `deploy/helm/fast-crud/` contains a Helm chart with configurable values, probes, resource requests/limits, ingress, and optional HPA.
- The API is stateless and does not need a PVC. Use persistent storage for PostgreSQL and NATS JetStream only when you run those dependencies in the cluster. S3-compatible object storage does not need an application PVC.

Kubernetes examples are templates. Update the image, hostname, CSP, secret names, external endpoints, egress policies, TLS, and security settings. Validate with `kubectl kustomize k8s/base`, `helm lint deploy/helm/fast-crud`, and staging rollout tests.

## MCP server

The separate `cmd/mcp` binary implements an MCP stdio server exposing `health_check`, `list_items`, and `get_item` read-only tools by default. Build it with:

```bash
go build -o fast-crud-mcp ./cmd/mcp
```

A trusted MCP host can launch it with environment variables:

```json
{
  "mcpServers": {
    "fast-crud": {
      "command": "/absolute/path/to/fast-crud-mcp",
      "env": {
        "DATABASE_URL": "postgres://mcp_readonly:...@localhost:5432/appdb?sslmode=verify-full",
        "MCP_ENABLE_MUTATIONS": "false"
      }
    }
  }
}
```

The snippet is an illustrative MCP host configuration; adapt it to the client you use and avoid placing secrets in source control. The MCP process uses stdio and should not be exposed as a public network service. Use a dedicated database account with only `SELECT` permissions for read-only operation. The optional create/update/delete tools appear only when `MCP_ENABLE_MUTATIONS=true`; do not enable them without a trusted host, human approval for destructive actions, and a least-privileged database role. The starter MCP implementation directly queries PostgreSQL and does not yet reuse the HTTP API's audit middleware or Redis cache. Add audit records for MCP operations before enabling mutations in production.

## Optional integrations added

- **Application-level S3 / MinIO:** off by default (`S3_ENABLED=false`). When enabled, authenticated `/api/v1/files/*` routes support uploads (10 MiB limit), object listing, 5-minute presigned downloads, and deletion. The bucket must exist and credentials must have only required bucket permissions.
- **Outbound webhooks:** configure `WEBHOOK_URLS` and optionally `WEBHOOK_HMAC_SECRET`; item create/update/delete events are sent as signed JSON when configured.
- **Microsoft Teams:** configure `TEAMS_WEBHOOK_URL` from an approved Teams Workflows webhook to send one-way Adaptive Card notifications. This is not a full conversational Teams bot.
- **Email:** configure SMTP settings and `EMAIL_NOTIFY_TO` for CRUD event notifications; authenticated test endpoints are available under `/api/integrations/`.
- **Argo CD GitOps:** `deploy/argocd/fast-crud-application.yaml` and Kustomize overlays for dev/staging/prod are provided. Replace repository/image/host placeholders and use immutable image tags. Keep secrets out of Git.
- **Common integrations:** Keycloak/LDAP configuration scaffolding, NATS JetStream, Redis, Prometheus/Grafana, CI, and PostgreSQL backup scripts remain available. See `integrations/README.md` for implementation boundaries.

These additions are not production-verified in this environment because Go dependencies could not be downloaded. Run `go mod tidy`, `go test ./...`, Docker builds, Kustomize builds, and an integration test in CI before deploying. Webhook delivery is currently best-effort without durable retry; S3 object ownership/tenant authorization and Teams bidirectional chat are not implemented.

## Deploy documentation to Vercel

The repository includes `vercel.json` to build and publish the complete Sphinx documentation site with the Read the Docs theme. In Vercel, import `aice09/goweb-template`, select `docs` as the project root, and use the `Other` framework preset. Vercel creates an isolated Python environment with `uv`, installs `requirements.txt`, builds the pages, and serves `_build/html` from the `docs` project root. Connect the `main` branch for automatic production deployments; use the Vercel project URL or attach a custom domain in project settings.
