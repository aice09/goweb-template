# goweb-template — Operations and Recovery

This runbook covers routine operation, PostgreSQL backup/restore, upgrades, rollback, monitoring, and incident handling.

## 1. Routine health checks

### Docker Compose

```bash
docker compose ps
docker compose logs --since=15m api
curl -fsS http://localhost:3000/health/live
curl -fsS http://localhost:3000/health/ready
```

Inspect the dependency logs if the application is not ready:

```bash
docker compose logs --since=15m postgres redis nats
```

### Kubernetes

```bash
kubectl -n fast-crud get pods,deploy,svc,ingress
kubectl -n fast-crud rollout status deployment/fast-crud
kubectl -n fast-crud describe deployment fast-crud
kubectl -n fast-crud logs deployment/fast-crud --since=15m
kubectl -n fast-crud get events --sort-by=.lastTimestamp
```

Adapt the namespace and workload names to the actual manifests.

## 2. PostgreSQL backup

### Requirements

Run the script from a trusted backup host/runner with:

- `pg_dump` and `pg_restore` compatible with the PostgreSQL server version
- AWS CLI v2 (or a compatible AWS CLI build)
- `age` when encryption is enabled
- Network access to PostgreSQL and the S3-compatible endpoint
- A private bucket with lifecycle/retention policy and least-privilege credentials

The script expects `DATABASE_URL`, `S3_BUCKET_BACKUPS`, and `BACKUP_AGE_RECIPIENT` when encryption is enabled. The `age` recipient is public; protect the matching private identity separately.

Example for AWS S3:

```bash
export DATABASE_URL='postgres://backup_user:URL_ENCODED_PASSWORD@db.example.internal:5432/appdb?sslmode=verify-full'
export S3_BUCKET_BACKUPS='my-private-backup-bucket'
export S3_REGION='ap-southeast-1'
export S3_BACKUP_PREFIX='postgres/'
export BACKUP_ENCRYPTION_ENABLED=true
export BACKUP_AGE_RECIPIENT='age1YOUR_PUBLIC_RECIPIENT'
export BACKUP_VERIFY_AFTER_UPLOAD=true
./scripts/backup-postgres.sh
```

For MinIO or Ceph RGW, configure `S3_ENDPOINT_URL`, `S3_ACCESS_KEY_ID`, `S3_SECRET_ACCESS_KEY`, and any required region/path-style behavior. Check provider-specific TLS and addressing requirements before running.

The script creates a PostgreSQL custom-format dump, validates its listing, encrypts it with `age` when enabled, uploads it, and optionally checks that the object exists. If encryption is disabled, the uploaded dump is plaintext; do not use that mode for sensitive production data unless an approved alternative encryption layer is guaranteed.

### Backup verification and retention

- Check the script exit code and confirm the object exists in the expected bucket/key.
- Alert on missing daily backups and failed uploads.
- Use bucket versioning, server-side encryption, restricted deletion permissions, and lifecycle policies.
- Keep the `age` private identity separate from the backup bucket and backup runner.
- Test restoration on a schedule. A successful upload does not prove a usable backup.
- Back up S3 application objects separately if the app uses object storage. Database backups do not contain S3 object bytes.

## 3. PostgreSQL restore — destructive operation

**Never restore over production as your first test.** Use an isolated recovery database and verify the result before any production cutover.

The restore script requires the correct `DATABASE_URL`, the bucket, and the private `age` identity file if the backup is encrypted. It requires confirmation by default.

```bash
export DATABASE_URL='postgres://restore_user:URL_ENCODED_PASSWORD@recovery-db.example.internal:5432/appdb?sslmode=verify-full'
export S3_BUCKET_BACKUPS='my-private-backup-bucket'
export S3_REGION='ap-southeast-1'
export BACKUP_AGE_IDENTITY_FILE='/secure/path/backup-age-identity'
export RESTORE_REQUIRE_CONFIRMATION=true

./scripts/restore-postgres.sh \
  's3://my-private-backup-bucket/postgres/fast-crud-YYYYMMDDTHHMMSSZ.dump.age'
```

Replace the example object path with the exact backup object. Confirm the target host/database carefully before proceeding.

### Recovery validation

1. Record the incident/change and stop writes to the recovery target.
2. Verify the backup object, expected timestamp, size, and provenance.
3. Restore into a fresh database with a compatible PostgreSQL major version.
4. Verify expected tables and row counts; test login and CRUD flows.
5. Confirm audit records and dependent integrations are sensible for the restored point in time.
6. Document measured recovery time and any data loss relative to the incident.
7. Approve cutover separately; retain the pre-cutover database until rollback is no longer required.

## 4. What else must be backed up?

| Component | Suggested recovery approach |
|---|---|
| PostgreSQL | Encrypted logical backups plus periodic restore drills; use PITR/WAL archiving if required by RPO |
| S3/MinIO/Ceph RGW assets | Bucket versioning/replication or provider-native backup; record bucket policy and lifecycle configuration |
| Redis | Treat as cache unless the app's data model relies on it; if persistence matters, back up its configured data volume |
| NATS JetStream | Persist the configured data directory and document stream/consumer configuration; validate replay after recovery |
| `.env` / secrets | Do not put secrets in Git or backup logs; back up through an approved encrypted secret-management process |
| Kubernetes / Helm / Argo CD | Git is the source of truth for manifests; separately back up cluster-specific secrets and external resources |
| Grafana/Prometheus | Preserve dashboards/configuration in Git; back up persistent data only if historical metrics/state must be retained |

## 5. Upgrade and rollback

1. Read release notes and compare `.env.example` for new/renamed variables.
2. Run tests and security scans; build a new immutable image.
3. Back up the database and any persistent files/objects affected by the release.
4. Deploy to staging and test startup, health endpoints, CRUD, and enabled integrations.
5. Promote the same image digest to production.
6. Watch rollout events, logs, latency/error metrics, and external dependency health.
7. Roll back application code if health checks fail. Restore database state only if necessary and only after evaluating whether newer writes would be lost.

Use `helm history` and `helm rollback` for Helm-owned releases. With Argo CD, revert the Git commit and allow reconciliation. With Compose, restore the previous image tag and configuration. Do not mix deployment ownership methods for the same workload.

## 6. Incident checklist

- Identify scope and impact; assign an incident owner.
- Preserve relevant logs and timestamps without copying credentials or session tokens.
- Check application health, database connectivity, disk space, resource pressure, and external dependencies.
- If compromise is suspected, revoke/rotate affected credentials and restrict ingress/egress.
- Do not delete persistent volumes or prune resources as a first troubleshooting step.
- Restore into an isolated environment first.
- Record the root cause, corrective actions, and a follow-up test.

## 7. Monitoring

The project includes Prometheus/Grafana examples. Before production, configure durable log collection, alert routing, and alerts for:

- Application readiness failures and elevated 5xx rates
- High request latency and error rate
- PostgreSQL availability, connection saturation, storage and backup failures
- Redis availability/memory pressure
- NATS JetStream storage/consumer lag where used
- S3 upload/download failures and capacity/quota issues
- Certificate expiry, pod restarts, CPU/memory saturation, and disk pressure

Metrics endpoints should not be publicly accessible without authentication/network restrictions. Dashboards alone are not alerting; configure and test alert delivery.
