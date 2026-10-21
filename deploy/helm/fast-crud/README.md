# Helm deployment

Build/push an image, create the namespace and external Secret, then install:

```bash
docker build -t ghcr.io/YOUR_ORG/fast-crud:0.1.0 .
docker push ghcr.io/YOUR_ORG/fast-crud:0.1.0

kubectl create namespace fast-crud
kubectl -n fast-crud create secret generic fast-crud-secrets \
  --from-literal=DATABASE_URL='postgres://...' \
  --from-literal=SESSION_SECRET="$(openssl rand -base64 48)" \
  --from-literal=SETUP_TOKEN="$(openssl rand -base64 48)"

helm upgrade --install fast-crud ./deploy/helm/fast-crud \
  --namespace fast-crud \
  --set image.repository=ghcr.io/YOUR_ORG/fast-crud \
  --set image.tag=0.1.0 \
  --set ingress.host=crud.example.com \
  --set config.TRUSTED_ORIGINS=https://crud.example.com
```

Externalize credentials using External Secrets Operator, SOPS, Sealed Secrets, Vault, or cloud secret managers. Do not commit credentials to values files.

This chart deploys the stateless application only. PostgreSQL, Redis, NATS JetStream, and S3-compatible object storage are external dependencies. Add PVCs to stateful dependencies, not the stateless API pod. The chart is a starter and needs `helm lint`, template validation, and staging rollout tests before production.

## Optional integrations

Set `config.S3_ENABLED` to `"true"` and add `S3_ACCESS_KEY` / `S3_SECRET_KEY` to the pre-created Secret to enable application-level S3/MinIO object endpoints. Configure `S3_ENDPOINT`, `S3_REGION`, `S3_BUCKET`, and `S3_USE_SSL` in `config`. Webhook, Teams, and SMTP settings are environment variables in `config`; place `WEBHOOK_HMAC_SECRET`, `SMTP_PASSWORD`, and other credentials in the Secret. See `integrations/README.md` for routes, setup, and limitations.

## GitOps

For Argo CD, use `deploy/argocd/fast-crud-application.yaml` as a starting Application manifest. Update the repository URL, image repository/tags, hostnames, and secret-management approach before syncing. Do not store real secrets in Git.
