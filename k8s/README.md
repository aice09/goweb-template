# Kubernetes deployment

This package deploys the stateless Go web/API container. PostgreSQL, Redis, NATS JetStream, and S3-compatible storage are configured as external services by default. The application pod does not need a PVC for item data. Use persistent storage for PostgreSQL/NATS/MinIO only if you deploy those stateful services inside Kubernetes.

## 1. Build and publish the image

```bash
docker build -t ghcr.io/YOUR_ORG/fast-crud:0.1.0 .
docker push ghcr.io/YOUR_ORG/fast-crud:0.1.0
```

Update the image in `base/deployment.yaml`.

## 2. Create secrets out-of-band

Do not commit a real Secret manifest. Use External Secrets Operator, Sealed Secrets, SOPS, Vault, or your cloud provider's secret manager. For a quick test only:

```bash
kubectl create namespace fast-crud
kubectl -n fast-crud create secret generic fast-crud-secrets \
  --from-literal=DATABASE_URL='postgres://app:CHANGE_ME@postgres.example.internal:5432/appdb?sslmode=verify-full' \
  --from-literal=SESSION_SECRET="$(openssl rand -base64 48)" \
  --from-literal=SETUP_TOKEN="$(openssl rand -base64 48)" \
  --from-literal=REDIS_PASSWORD='CHANGE_ME' \
  --from-literal=NATS_USERNAME='fast-crud' \
  --from-literal=NATS_PASSWORD='CHANGE_ME' \
  --from-literal=JWT_SIGNING_KEY="$(openssl rand -base64 48)" \
  --from-literal=OIDC_CLIENT_SECRET='CHANGE_ME' \
  --from-literal=LDAP_BIND_PASSWORD='CHANGE_ME' \
  --from-literal=S3_ACCESS_KEY_ID='CHANGE_ME' \
  --from-literal=S3_SECRET_ACCESS_KEY='CHANGE_ME' \
  --from-literal=GRAFANA_ADMIN_PASSWORD='CHANGE_ME' \
  --from-literal=BACKUP_AGE_RECIPIENT='age1CHANGE_ME'
```

Ensure the Secret contains every key referenced by the Deployment. The example command is a template; use real credentials and omit keys for features you are not using. Kubernetes Secrets are base64-encoded by default, not encrypted at rest unless the cluster is configured for encryption.

## 3. Configure the environment

Edit `base/configmap.yaml`:
- Set `TRUSTED_ORIGINS` to the exact public origin.
- Set Redis, NATS, and S3 endpoints to reachable private endpoints.
- Align CSP directives with the actual frontend asset origins.
- Change the example hostname and ingress class.

**Compatibility warning:** the current application code does not yet implement the documented LDAP/OIDC/JWT/MFA or S3 asset-upload settings. Do not enable these options until their handlers are implemented. NATS JetStream publishing and PostgreSQL audit logging are present in the starter, but a transactional outbox is still required for atomic database-to-event delivery.

## 4. Deploy

```bash
kubectl apply -k k8s/base
kubectl -n fast-crud rollout status deployment/fast-crud
kubectl -n fast-crud get pods,svc,ingress
```

## Storage

- App API: stateless; no PVC required.
- PostgreSQL: managed database or StatefulSet with PVC and verified backups.
- NATS JetStream: persistent file storage on a PVC, preferably a dedicated NATS cluster/operator.
- S3 assets/backups: object storage; no app PVC required.
- Backup jobs: use a Kubernetes CronJob or external scheduler with least-privilege credentials and a mounted age identity Secret.

## Production hardening

Configure TLS at the ingress, network policies for database/Redis/NATS egress, pod disruption budgets, HPA, image signing/scanning, namespace quotas, resource quotas, backup CronJobs, alerts, and External Secrets. Test upgrades and restore drills in staging first.

## Optional application integrations

The application-level S3 object API is disabled by default (`S3_ENABLED=false`). To enable it, set `S3_ENABLED=true` in the ConfigMap and provide `S3_ACCESS_KEY` and `S3_SECRET_KEY` through the `fast-crud-secrets` Secret. Also configure `S3_ENDPOINT`, `S3_REGION`, `S3_BUCKET`, and `S3_USE_SSL`. The bucket must already exist. Use `S3_USE_SSL=true` for production.

For email, add `SMTP_HOST`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `EMAIL_FROM`, and `EMAIL_NOTIFY_TO` (comma-separated) to configuration/secrets. For webhook/Teams events, set `WEBHOOK_URLS`, `WEBHOOK_HMAC_SECRET`, and/or `TEAMS_WEBHOOK_URL`; treat URLs as credentials. Configure these via External Secrets Operator, SOPS, Sealed Secrets, Vault, or your cloud secret manager rather than committing values.

## Argo CD GitOps

An Argo CD Application and Kustomize overlays are under `deploy/argocd/` and `deploy/kustomize/overlays/{dev,staging,prod}`. Replace the Git repository URL, org/image name, immutable image tags, hostnames, and environment values before applying. Keep environment-specific secret material outside Git. See `integrations/README.md` for current integration boundaries and limitations.
