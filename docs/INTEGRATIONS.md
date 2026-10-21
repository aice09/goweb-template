# Fast CRUD Pro — Integration Guide

This guide describes the intent, setup boundaries, and security requirements for optional integrations. Feature availability depends on the code path and provider setup in the checked-out release. Test integrations in a non-production environment before relying on them.

## 1. S3-compatible object storage

S3 is optional. Keep it disabled if the application should not store user files in object storage. The application-level feature and PostgreSQL backup destination are separate concerns and can use different buckets/credentials.

### Provider choices

- AWS S3
- MinIO
- Ceph RGW
- Other S3-compatible object stores after compatibility testing

### Configuration checklist

1. Create a dedicated bucket, such as `fast-crud-files`.
2. Create an access identity limited to that bucket and only required actions.
3. Set `S3_ENABLED=true` only when application file storage is required.
4. Set the endpoint, region, bucket, access key, secret key, TLS, and path-style options appropriate for the provider.
5. Configure CORS only if browser-origin access is actually needed; prefer server-mediated uploads.
6. Verify size limits, object ownership/authorization, deletion behavior, URL expiry, and backup/retention requirements.
7. Never expose root/admin credentials or the MinIO console publicly.

Do not assume an application S3 configuration also configures the backup scripts. Backup scripts use `S3_BUCKET_BACKUPS`, `S3_ENDPOINT_URL`, `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY`, and related backup variables; application-level S3 may use a different set of names. Review `.env.example` and the implementation.

## 2. Webhooks

Webhooks let the app notify another HTTPS endpoint about selected events. Configure only trusted receivers.

- Use HTTPS and validate certificates.
- Use a dedicated secret for HMAC signatures if supported by the implementation.
- On the receiving side, verify the signature over the exact raw request body, compare signatures in constant time, and reject stale/replayed events where feasible.
- Make receivers idempotent: the same event may be delivered more than once.
- Set outbound network controls to prevent requests to localhost, cloud metadata addresses, and internal control-plane services. Do not accept arbitrary user-supplied webhook URLs without SSRF defenses.
- Define timeout, retry, dead-letter, and observability expectations. If durable delivery is required, implement a database outbox and retry worker; best-effort notification is not equivalent to guaranteed delivery.

Send a test event to a controlled endpoint first and inspect both the sender logs and receiver logs.

## 3. SMTP email

Use an approved SMTP relay/provider. Prefer STARTTLS on port 587 or implicit TLS on port 465 according to provider requirements.

Set the SMTP host, port, username/password, sender address, and recipient configuration. Use a credential dedicated to the app; do not use a personal mailbox password. Confirm SPF, DKIM, and DMARC alignment for the sending domain if email is sent to external recipients.

Test:
1. Connection and TLS negotiation
2. Authentication
3. Sender/recipient acceptance
4. Delivery, spam placement, and bounce behavior
5. Failure handling and alerting

Never log SMTP passwords or message contents containing sensitive data.

## 4. Microsoft Teams

The starter's Teams integration is for notifications, not a full Teams chat client. Configure an approved Teams Workflows webhook/endpoint supported by your tenant. Microsoft changes webhook and connector capabilities over time, so validate the current tenant-specific method with your Microsoft 365 administrator.

Use a dedicated channel and test card formatting, delivery permissions, and endpoint rotation. Treat the URL as a secret. Do not paste it into source control, screenshots, tickets, or logs. For interactive/bidirectional chat, commands, identity, or message replies, implement a Teams bot using Microsoft Bot Framework / supported Microsoft 365 APIs and appropriate app registration, permissions, and tenant consent.

## 5. MCP server

The MCP server is a separate stdio process in `cmd/mcp/`, built with `Dockerfile.mcp`. Configure the trusted MCP host to launch the process and pass its environment variables through its supported configuration format. Do not expose stdio MCP as a public HTTP service.

- Keep `MCP_ENABLE_MUTATIONS=false` unless write operations are explicitly needed.
- Prefer a dedicated PostgreSQL role with only the required read permissions.
- Limit tools and database privileges to the minimum scope.
- Protect the MCP host configuration and logs.
- Test protocol compatibility with the intended MCP client.
- Review every write operation and do not enable mutation tools for untrusted users.

MCP is a tool interface; it does not automatically provide user authentication, authorization, approval workflows, or a full audit boundary equivalent to the web application.

## 6. Keycloak/OIDC, LDAP, JWT and MFA

The root README marks these as configuration scaffolding unless the relevant provider flows have been implemented in the release being used. Do not set an environment flag and assume SSO/MFA is active.

For a production design:
- Prefer Keycloak OIDC with issuer, audience, signature, state, nonce, PKCE, callback, and token-expiry validation.
- Enforce MFA at the identity provider and validate the resulting identity/assurance claims.
- If direct LDAP is required, use LDAPS or verified StartTLS and a least-privilege bind account.
- Map provider groups to explicit application roles.
- Test account linking, deprovisioning, session revocation, and authorization failures.

## 7. Redis and NATS

Redis is used as a cache in this starter; decide whether cache data is disposable before relying on persistence. Keep Redis on a private network and require authentication where supported.

NATS/JetStream can carry application events. For durable business workflows, configure persistent storage, limits, monitoring, idempotent consumers, and a transactional outbox. A successful database transaction and a separate message publish are not automatically atomic.

## 8. Observability and OpenTelemetry

Prometheus/Grafana configuration is included as a starting point. Restrict metric and dashboard access. Add alert rules, alert routing, log aggregation, exporters for PostgreSQL/Redis/NATS, and service-level objectives.

OpenTelemetry variables alone do not prove tracing is implemented. Verify that the application emits traces and that the configured collector/exporter receives them before treating tracing as operational.

## 9. CI/CD and GitOps

Recommended pipeline:

1. Format, unit tests, race tests, vet, and dependency checks.
2. Build and scan container images.
3. Publish immutable tags/digests.
4. Deploy to development/staging.
5. Run smoke, integration, and security tests.
6. Promote the same artifact by changing a protected GitOps overlay.
7. Let Argo CD reconcile the cluster and verify health.
8. Roll back by reverting the Git change.

Protect branches, require reviews for production overlays, avoid secrets in Git, and review the Argo CD automated prune policy. Separate application release artifacts from environment-specific configuration.
