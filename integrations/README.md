# Integration guide

## Optional application-level S3 / MinIO

S3 storage is **off by default**. Set `S3_ENABLED=true`, configure `S3_ENDPOINT`, `S3_REGION`, `S3_BUCKET`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, and `S3_USE_SSL`. The bucket must exist before the API starts. In Compose, start MinIO with `docker compose --profile s3 up -d minio`, create the bucket with `mc`, and set `S3_ENDPOINT=minio:9000`, `S3_USE_SSL=false`. In production, use HTTPS and an IAM/service account restricted to the one bucket and required object operations.

Authenticated API routes (under `/api/v1`, CSRF required for mutations):

- `POST /files/upload` — multipart form field `file`, maximum 10 MiB
- `GET /files?prefix=...` — list up to 100 objects
- `GET /files/download?key=...` — returns a 5-minute presigned URL
- `DELETE /files?key=...` — deletes an object

Object metadata is not stored in PostgreSQL yet; the bucket is the source of truth. Add database metadata/ownership records before using this for multi-tenant or regulated workloads. The application currently trusts authenticated users to access objects in the configured bucket; add authorization/tenant checks before multi-user production use.

## Webhooks

Set `WEBHOOK_URLS` to a comma-separated list of HTTPS endpoints. Item create/update/delete events are posted as JSON. When `WEBHOOK_HMAC_SECRET` is set, the request includes `X-FastCRUD-Signature: sha256=<hex HMAC-SHA256 of raw request body>`. Receivers must verify signatures with constant-time comparison. Delivery is best-effort and asynchronous in this starter; it does not have persistent retries or a dead-letter queue. For reliable delivery, consume the existing NATS JetStream events or implement an outbox/worker.

## Microsoft Teams

Set `TEAMS_WEBHOOK_URL` to a Microsoft Teams Workflows webhook endpoint approved by your tenant. Item events are sent as Adaptive Card notifications. This provides one-way notifications, not a full Teams chat bot, chat history integration, or bidirectional commands. Keep the URL secret and rotate it if exposed.

## Email

Set `SMTP_HOST`, `SMTP_PORT` (normally `587`), optional `SMTP_USERNAME` / `SMTP_PASSWORD`, `EMAIL_FROM`, and comma-separated `EMAIL_NOTIFY_TO`. Item events trigger notifications. `POST /api/integrations/email/test` sends a test email; it requires a logged-in session and CSRF token. Use a trusted SMTP relay that requires STARTTLS; delivery and provider policies vary.

## Integration status and tests

- `GET /api/integrations/status` reports whether integrations are configured (not an end-to-end connectivity test).
- `POST /api/integrations/webhook/test` sends a test event to configured webhook/Teams endpoints.
- `POST /api/integrations/email/test` tests SMTP delivery.

Both test endpoints require authentication and CSRF protection. Never expose credentials in browser JavaScript or commit real secrets.

## Common integrations and recommended boundaries

| Capability | Recommended integration | Current status |
|---|---|---|
| Identity / SSO / MFA | Keycloak via OIDC; LDAP/AD only over LDAPS/StartTLS | Existing configuration scaffolding; full auth flow still needs implementation/testing |
| Event bus | NATS JetStream | Existing event publish path; CRUD writes and event publish are not yet transactional |
| Cache | Redis | Existing optional cache |
| Object storage | AWS S3 / MinIO | Optional app-level object API added; validate authorization before multi-tenant use |
| Email | SMTP relay | Implemented basic notifications and test endpoint |
| Chat notifications | Teams Workflows webhook | One-way Adaptive Card notifications |
| Outbound integrations | HTTPS webhooks + HMAC | Basic best-effort delivery; no durable retries |
| GitOps | Argo CD + Kustomize overlays | Application manifest and dev/staging/prod overlays added; repository/image placeholders must be replaced |
| Observability | Prometheus / Grafana; OpenTelemetry collector recommended | Existing metrics/dashboard scaffolding; OTEL instrumentation is not implemented |
| CI / supply chain | GitHub Actions, image scanning, SBOM/signing recommended | Existing CI; add image publishing and signed immutable tags for production |
| Secrets | External Secrets Operator, SOPS, Vault, cloud secret manager | Manifests reference Kubernetes Secret; choose and configure a secrets controller |
| Backups | pg_dump + S3, restore drills | Existing scripts; test recovery and retention policy |
