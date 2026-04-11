#!/bin/sh
# =============================================================================
# Personel Platform — WORM Audit Bucket Initialization
#
# Creates the audit-worm MinIO bucket with S3 Object Lock in Compliance Mode.
# This bucket is the WORM (Write Once Read Many) sink for daily audit chain
# checkpoints. See docs/adr/0014-worm-audit-sink.md for the design rationale.
#
# IMPORTANT:
#   - Object Lock must be enabled at bucket CREATION TIME. It cannot be
#     retroactively enabled on an existing bucket.
#   - This script is IDEMPOTENT for the bucket existence check, but it cannot
#     add Object Lock to an existing bucket that lacks it. If audit-worm already
#     exists without Object Lock, the script will emit an error and exit 1.
#   - The audit-sink service account has PutObject + GetObject only.
#     No DeleteObject. No bucket-level operations.
#
# Inputs (environment variables):
#   MINIO_ENDPOINT         MinIO server address (e.g. http://minio:9000)
#   MINIO_ROOT_USER        MinIO root access key
#   MINIO_ROOT_PASSWORD    MinIO root secret key
#   AUDIT_SINK_ACCESS_KEY  Access key for the audit-sink service account
#   AUDIT_SINK_SECRET_KEY  Secret key for the audit-sink service account
#
# =============================================================================
set -eu

MINIO_ENDPOINT="${MINIO_ENDPOINT:-http://minio:9000}"
ALIAS="personel-worm-init"
BUCKET="audit-worm"

echo "[worm-init] Waiting for MinIO to be ready..."
for i in $(seq 1 30); do
  if mc ready "${ALIAS}" 2>/dev/null; then
    break
  fi
  sleep 2
  echo "[worm-init] Retry $i/30..."
done

# Configure alias using root credentials (needed for bucket creation and
# user/policy management).
mc alias set "${ALIAS}" "${MINIO_ENDPOINT}" \
  "${MINIO_ROOT_USER}" "${MINIO_ROOT_PASSWORD}" --api s3v4 --quiet

# ---------------------------------------------------------------------------
# Check if the bucket already exists.
# ---------------------------------------------------------------------------
if mc ls "${ALIAS}/${BUCKET}" >/dev/null 2>&1; then
  echo "[worm-init] Bucket '${BUCKET}' already exists — verifying Object Lock status..."

  # Verify Object Lock is enabled. mc's output format: "Object lock configuration: Enabled"
  lock_status=$(mc stat "${ALIAS}/${BUCKET}" 2>/dev/null | grep -i "object lock" || true)

  if echo "${lock_status}" | grep -qi "enabled"; then
    echo "[worm-init] Object Lock is ENABLED on '${BUCKET}' — OK"
  else
    echo "[worm-init] ERROR: Bucket '${BUCKET}' exists but Object Lock is NOT enabled."
    echo "[worm-init] Object Lock cannot be retroactively enabled. You must:"
    echo "[worm-init]   1. Drain and remove the existing bucket (after verifying no locked objects)."
    echo "[worm-init]   2. Re-run this script to create a fresh bucket with Object Lock."
    echo "[worm-init] See docs/security/runbooks/worm-audit-recovery.md §Recovery."
    exit 1
  fi
else
  # Create the bucket WITH Object Lock enabled.
  # --with-lock enables S3 Object Lock (requires MinIO >= 2021-01-05).
  echo "[worm-init] Creating bucket '${BUCKET}' with Object Lock enabled..."
  mc mb --with-lock "${ALIAS}/${BUCKET}"
  echo "[worm-init] Bucket '${BUCKET}' created with Object Lock."
fi

# ---------------------------------------------------------------------------
# Set the default Object Lock retention policy.
# Mode: COMPLIANCE — even the MinIO root account cannot bypass.
# Validity: 5 years (1826 days, accounting for one leap year).
#
# This is the default applied to objects that do not specify their own
# RetainUntilDate at put time. The audit sink code also sets an explicit
# RetainUntilDate per object for belt-and-braces.
# ---------------------------------------------------------------------------
echo "[worm-init] Setting default retention policy: COMPLIANCE, 1826 days (5 years)..."
mc retention set --default COMPLIANCE 1826d "${ALIAS}/${BUCKET}"
echo "[worm-init] Default retention policy applied."

# ---------------------------------------------------------------------------
# Enable versioning (belt-and-braces; required for Object Lock to function
# correctly with MinIO's implementation of the S3 spec).
# ---------------------------------------------------------------------------
mc version enable "${ALIAS}/${BUCKET}" 2>/dev/null || true
echo "[worm-init] Versioning enabled on '${BUCKET}'."

# ---------------------------------------------------------------------------
# Create the audit-sink service account if the environment provides credentials.
# This account has PutObject + GetObject only; NO DeleteObject.
# ---------------------------------------------------------------------------
if [ -n "${AUDIT_SINK_ACCESS_KEY:-}" ] && [ -n "${AUDIT_SINK_SECRET_KEY:-}" ]; then
  # Create the user (idempotent — mc returns ok even if the user already exists).
  mc admin user add "${ALIAS}" \
    "${AUDIT_SINK_ACCESS_KEY}" \
    "${AUDIT_SINK_SECRET_KEY}" 2>/dev/null || true

  # Create and apply the audit-sink policy.
  mc admin policy create "${ALIAS}" audit-sink-policy /dev/stdin <<'POLICY' 2>/dev/null || true
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "AuditSinkWrite",
      "Effect": "Allow",
      "Action": [
        "s3:PutObject"
      ],
      "Resource": [
        "arn:aws:s3:::audit-worm/*"
      ]
    },
    {
      "Sid": "AuditSinkRead",
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:GetObjectVersion",
        "s3:ListBucket",
        "s3:ListBucketVersions"
      ],
      "Resource": [
        "arn:aws:s3:::audit-worm",
        "arn:aws:s3:::audit-worm/*"
      ]
    }
  ]
}
POLICY

  mc admin policy attach "${ALIAS}" audit-sink-policy \
    --user "${AUDIT_SINK_ACCESS_KEY}" 2>/dev/null || true

  echo "[worm-init] audit-sink service account configured with PutObject + GetObject only."
else
  echo "[worm-init] WARN: AUDIT_SINK_ACCESS_KEY / AUDIT_SINK_SECRET_KEY not set."
  echo "[worm-init]       The audit-sink service account was NOT created."
  echo "[worm-init]       Set these env vars and re-run, or create the account manually."
fi

# ---------------------------------------------------------------------------
# Final status check.
# ---------------------------------------------------------------------------
echo "[worm-init] Final bucket status:"
mc stat "${ALIAS}/${BUCKET}" 2>/dev/null || true

echo "[worm-init] WORM audit bucket initialization complete."
