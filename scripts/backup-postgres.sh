#!/usr/bin/env bash
set -Eeuo pipefail
# Run on a trusted host with pg_dump, age (if encryption is enabled), and AWS CLI installed.
: "${DATABASE_URL:?Set DATABASE_URL to the source PostgreSQL connection string}"
: "${S3_BUCKET_BACKUPS:?Set S3_BUCKET_BACKUPS}"
: "${S3_REGION:=ap-southeast-1}"
if [[ -n "${S3_ACCESS_KEY_ID:-}" ]]; then export AWS_ACCESS_KEY_ID="$S3_ACCESS_KEY_ID"; fi
if [[ -n "${S3_SECRET_ACCESS_KEY:-}" ]]; then export AWS_SECRET_ACCESS_KEY="$S3_SECRET_ACCESS_KEY"; fi
if [[ -n "${S3_SESSION_TOKEN:-}" ]]; then export AWS_SESSION_TOKEN="$S3_SESSION_TOKEN"; fi
: "${S3_ENDPOINT_URL:=}"
: "${S3_BACKUP_PREFIX:=postgres/}"
: "${BACKUP_LOCAL_DIR:=./backups}"
: "${BACKUP_ENCRYPTION_ENABLED:=true}"
: "${BACKUP_AGE_RECIPIENT:?Set BACKUP_AGE_RECIPIENT to an age public recipient}"
: "${BACKUP_VERIFY_AFTER_UPLOAD:=true}"

command -v pg_dump >/dev/null || { echo "pg_dump is required" >&2; exit 1; }
command -v aws >/dev/null || { echo "AWS CLI is required" >&2; exit 1; }
mkdir -p "$BACKUP_LOCAL_DIR"
umask 077
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
file="$BACKUP_LOCAL_DIR/fast-crud-${stamp}.dump"
encrypted="${file}.age"

pg_dump --format=custom --no-owner --no-acl "$DATABASE_URL" --file "$file"
pg_restore --list "$file" >/dev/null

upload_file="$file"
if [[ "$BACKUP_ENCRYPTION_ENABLED" == "true" ]]; then
  command -v age >/dev/null || { echo "age is required when backup encryption is enabled" >&2; rm -f "$file"; exit 1; }
  age -r "$BACKUP_AGE_RECIPIENT" -o "$encrypted" "$file"
  upload_file="$encrypted"
fi

args=(s3 cp "$upload_file" "s3://${S3_BUCKET_BACKUPS%/}/${S3_BACKUP_PREFIX#/}$(basename "$upload_file")" --region "$S3_REGION")
if [[ -n "$S3_ENDPOINT_URL" ]]; then args+=(--endpoint-url "$S3_ENDPOINT_URL"); fi
aws "${args[@]}"

if [[ "$BACKUP_VERIFY_AFTER_UPLOAD" == "true" ]]; then
  remote="s3://${S3_BUCKET_BACKUPS%/}/${S3_BACKUP_PREFIX#/}$(basename "$upload_file")"
  verify_args=(s3api head-object --bucket "$S3_BUCKET_BACKUPS" --key "${S3_BACKUP_PREFIX#/}$(basename "$upload_file")" --region "$S3_REGION")
  if [[ -n "$S3_ENDPOINT_URL" ]]; then verify_args+=(--endpoint-url "$S3_ENDPOINT_URL"); fi
  aws "${verify_args[@]}" >/dev/null
fi

# Remove plaintext dump after successful encryption/upload.
if [[ "$BACKUP_ENCRYPTION_ENABLED" == "true" ]]; then rm -f "$file"; fi
echo "Backup uploaded: $(basename "$upload_file")"
