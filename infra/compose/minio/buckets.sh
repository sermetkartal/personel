#!/bin/sh
# =============================================================================
# Personel Platform — MinIO Bucket Initialization
# Idempotent: safe to re-run. Creates all required buckets and applies
# lifecycle policies per the data-retention-matrix.
# =============================================================================
set -eu

MINIO_ENDPOINT="${MINIO_ENDPOINT:-http://minio:9000}"
ALIAS="personel"

echo "[minio-init] Waiting for MinIO to be ready..."
for i in $(seq 1 30); do
  if mc ready "${ALIAS}" 2>/dev/null; then
    break
  fi
  sleep 2
  echo "[minio-init] Retry $i/30..."
done

# Configure alias
mc alias set "${ALIAS}" "${MINIO_ENDPOINT}" \
  "${MINIO_ROOT_USER}" "${MINIO_ROOT_PASSWORD}" --api s3v4

# ---------------------------------------------------------------------------
# Helper: create bucket if not exists
# ---------------------------------------------------------------------------
create_bucket() {
  local name="$1"
  if mc ls "${ALIAS}/${name}" >/dev/null 2>&1; then
    echo "[minio-init] Bucket '${name}' already exists — skipping"
  else
    mc mb "${ALIAS}/${name}"
    echo "[minio-init] Created bucket '${name}'"
  fi
}

# ---------------------------------------------------------------------------
# Create buckets
# ---------------------------------------------------------------------------
create_bucket "screenshots"
create_bucket "keystroke-blobs"
create_bucket "screen-clips"
create_bucket "dsr-responses"
create_bucket "destruction-reports"
create_bucket "backups"

# Sensitive-flagged bucket: KVKK m.6 special-category data — shorter TTL
create_bucket "sensitive-events"

# ---------------------------------------------------------------------------
# Apply versioning (required for lifecycle rules with non-current expiry)
# ---------------------------------------------------------------------------
for bucket in screenshots keystroke-blobs screen-clips dsr-responses sensitive-events; do
  mc version enable "${ALIAS}/${bucket}" 2>/dev/null || true
done

# ---------------------------------------------------------------------------
# Apply lifecycle policies
# ---------------------------------------------------------------------------
echo "[minio-init] Applying lifecycle policies..."
mc ilm import "${ALIAS}/screenshots" < /lifecycle.json 2>/dev/null || \
  echo "[minio-init] WARN: lifecycle import failed for screenshots (check lifecycle.json)"

# Keystroke blobs use the same policy file with a different prefix
mc ilm import "${ALIAS}/keystroke-blobs" < /lifecycle.json 2>/dev/null || true
mc ilm import "${ALIAS}/sensitive-events" < /lifecycle.json 2>/dev/null || true

# Backups: 30-day expiry for daily, 90-day for weekly
mc ilm set "${ALIAS}/backups" \
  --expiry-days 30 \
  --prefix "daily/" 2>/dev/null || true

mc ilm set "${ALIAS}/backups" \
  --expiry-days 90 \
  --prefix "weekly/" 2>/dev/null || true

# DSR responses: 5 years (KVKK m.11 — response evidence must be retained)
mc ilm set "${ALIAS}/dsr-responses" \
  --expiry-days 1825 2>/dev/null || true

# Destruction reports: permanent (compliance evidence)
# No expiry rule on destruction-reports

# ---------------------------------------------------------------------------
# Service user policies
# ---------------------------------------------------------------------------

# Gateway writer: write screenshots, screen-clips; read policy bundles
mc admin policy create "${ALIAS}" gateway-writer /dev/stdin <<'EOF' 2>/dev/null || true
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:PutObject", "s3:GetObject", "s3:DeleteObject"],
      "Resource": [
        "arn:aws:s3:::screenshots/*",
        "arn:aws:s3:::screen-clips/*",
        "arn:aws:s3:::keystroke-blobs/*"
      ]
    },
    {
      "Effect": "Allow",
      "Action": ["s3:ListBucket"],
      "Resource": ["arn:aws:s3:::screenshots", "arn:aws:s3:::screen-clips", "arn:aws:s3:::keystroke-blobs"]
    }
  ]
}
EOF

# DLP reader: read keystroke-blobs only
mc admin policy create "${ALIAS}" dlp-reader /dev/stdin <<'EOF' 2>/dev/null || true
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject"],
      "Resource": ["arn:aws:s3:::keystroke-blobs/*", "arn:aws:s3:::sensitive-events/*"]
    },
    {
      "Effect": "Allow",
      "Action": ["s3:ListBucket"],
      "Resource": ["arn:aws:s3:::keystroke-blobs", "arn:aws:s3:::sensitive-events"]
    }
  ]
}
EOF

# Backup writer: full access to backups bucket
mc admin policy create "${ALIAS}" backup-writer /dev/stdin <<'EOF' 2>/dev/null || true
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:*"],
      "Resource": ["arn:aws:s3:::backups", "arn:aws:s3:::backups/*"]
    }
  ]
}
EOF

# API reader: read screenshots, dsr-responses, destruction-reports
mc admin policy create "${ALIAS}" api-reader /dev/stdin <<'EOF' 2>/dev/null || true
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:ListBucket"],
      "Resource": [
        "arn:aws:s3:::screenshots", "arn:aws:s3:::screenshots/*",
        "arn:aws:s3:::dsr-responses", "arn:aws:s3:::dsr-responses/*",
        "arn:aws:s3:::destruction-reports", "arn:aws:s3:::destruction-reports/*"
      ]
    },
    {
      "Effect": "Allow",
      "Action": ["s3:PutObject"],
      "Resource": ["arn:aws:s3:::dsr-responses/*", "arn:aws:s3:::destruction-reports/*"]
    }
  ]
}
EOF

echo "[minio-init] Bucket initialization complete."
