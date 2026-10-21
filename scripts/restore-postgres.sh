#!/usr/bin/env bash
set -Eeuo pipefail
# Restore is destructive. Use a dedicated maintenance window and a verified backup.
: "${DATABASE_URL:?Set DATABASE_URL to the RESTORE TARGET connection string}"
: "${S3_BUCKET_BACKUPS:?Set S3_BUCKET_BACKUPS}"
: "${S3_REGION:=ap-southeast-1}"
if [[ -n "${S3_ACCESS_KEY_ID:-}" ]]; then export AWS_ACCESS_KEY_ID="$S3_ACCESS_KEY_ID"; fi
if [[ -n "${S3_SECRET_ACCESS_KEY:-}" ]]; then export AWS_SECRET_ACCESS_KEY="$S3_SECRET_ACCESS_KEY"; fi
if [[ -n "${S3_SESSION_TOKEN:-}" ]]; then export AWS_SESSION_TOKEN="$S3_SESSION_TOKEN"; fi
: "${S3_ENDPOINT_URL:=}"
: "${BACKUP_AGE_IDENTITY_FILE:?Set BACKUP_AGE_IDENTITY_FILE to a protected age identity file}"
: "${RESTORE_REQUIRE_CONFIRMATION:=true}"

if [[ $# -ne 1 ]]; then
  echo "Usage: $0 s3://bucket/key.dump[.age] | /path/to/backup.dump[.age]" >&2
  exit 2
fi
source_file="$1"
tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT
local_file="$tmpdir/backup"
if [[ "$source_file" == s3://* ]]; then
  command -v aws >/dev/null || { echo "AWS CLI is required" >&2; exit 1; }
  args=(s3 cp "$source_file" "$local_file" --region "$S3_REGION")
  if [[ -n "$S3_ENDPOINT_URL" ]]; then args+=(--endpoint-url "$S3_ENDPOINT_URL"); fi
  aws "${args[@]}"
else
  cp -- "$source_file" "$local_file"
fi

if [[ "$local_file" == *.age || "$source_file" == *.age ]]; then
  command -v age >/dev/null || { echo "age is required to decrypt this backup" >&2; exit 1; }
  [[ -r "$BACKUP_AGE_IDENTITY_FILE" ]] || { echo "Age identity file is not readable" >&2; exit 1; }
  age -d -i "$BACKUP_AGE_IDENTITY_FILE" "$local_file" > "$tmpdir/backup.dump"
  local_file="$tmpdir/backup.dump"
fi

command -v pg_restore >/dev/null || { echo "pg_restore is required" >&2; exit 1; }
pg_restore --list "$local_file" >/dev/null
if [[ "$RESTORE_REQUIRE_CONFIRMATION" == "true" ]]; then
  echo "WARNING: this will replace objects in the target database."
  read -r -p "Type RESTORE to continue: " confirm
  [[ "$confirm" == "RESTORE" ]] || { echo "Cancelled."; exit 1; }
fi

# --clean/--if-exists removes objects contained in the dump. Verify DATABASE_URL carefully.
pg_restore --verbose --clean --if-exists --no-owner --no-acl --exit-on-error \
  --dbname "$DATABASE_URL" "$local_file"
echo "Restore completed. Run application smoke tests and verify record counts before reopening traffic."
