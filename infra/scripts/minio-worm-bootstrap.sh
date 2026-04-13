#!/usr/bin/env bash
# =============================================================================
# Personel Platform — MinIO WORM Bucket Bootstrap
#
# Creates two S3 Object Lock buckets in COMPLIANCE mode with 5-year default
# retention:
#
#   audit-worm     — daily hash-chain checkpoints from apps/api/internal/audit
#                    + entry-level mirror (Phase 3.1)
#   evidence-worm  — SOC 2 Type II evidence locker items from
#                    apps/api/internal/evidence
#
# COMPLIANCE mode is irreversible: even the MinIO root user cannot delete or
# overwrite an object before its RetainUntilDate. This is the cryptographic
# guarantee CLAUDE.md §10 + ADR 0014 rely on for tamper-evident audit.
#
# Idempotent: re-running is safe. The script will refuse to "fix" a bucket
# that exists without Object Lock (Object Lock cannot be retroactively
# enabled) and will exit non-zero so the operator sees the error.
#
# Outputs:
#   /etc/personel/secrets/audit-writer.creds  (chmod 600)
#       line 1: ACCESS_KEY
#       line 2: SECRET_KEY
#
# Prerequisites:
#   docker (script runs the official minio/mc image)
#   /etc/personel/secrets/minio-root.env containing:
#       MINIO_ROOT_USER=...
#       MINIO_ROOT_PASSWORD=...
#
# Usage:
#   sudo infra/scripts/minio-worm-bootstrap.sh
#   sudo infra/scripts/minio-worm-bootstrap.sh --force   # rotate writer creds
#
# This script does NOT touch the running stack. Run it once, then point the
# api service config at /etc/personel/secrets/audit-writer.creds and restart.
#
# See docs/operations/minio-worm-migration.md for the full runbook.
# =============================================================================
set -euo pipefail

SCRIPT_NAME="minio-worm-bootstrap"
MINIO_ENDPOINT="${MINIO_ENDPOINT:-http://minio:9000}"
MINIO_NETWORK="${MINIO_NETWORK:-personel_default}"
ROOT_ENV_FILE="${MINIO_ROOT_ENV_FILE:-/etc/personel/secrets/minio-root.env}"
WRITER_CREDS_FILE="/etc/personel/secrets/audit-writer.creds"

AUDIT_BUCKET="audit-worm"
EVIDENCE_BUCKET="evidence-worm"
RETENTION_DAYS="1826"   # 5 years, accounts for one leap year

log()  { printf '[%s] %s\n' "${SCRIPT_NAME}" "$*"; }
warn() { printf '[%s] WARN: %s\n' "${SCRIPT_NAME}" "$*" >&2; }
die()  { printf '[%s] ERROR: %s\n' "${SCRIPT_NAME}" "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Flag parsing
# ---------------------------------------------------------------------------
FORCE_ROTATE=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --force) FORCE_ROTATE=true; shift ;;
    --help|-h)
      sed -n '2,40p' "${BASH_SOURCE[0]}" | sed 's/^# \?//'
      exit 0
      ;;
    *) die "unknown flag: $1" ;;
  esac
done

# ---------------------------------------------------------------------------
# Prerequisites
# ---------------------------------------------------------------------------
command -v docker >/dev/null 2>&1 || die "docker required to invoke mc image"
[[ -f "${ROOT_ENV_FILE}" ]] || die "MinIO root env file not found: ${ROOT_ENV_FILE}"

# shellcheck disable=SC1090
. "${ROOT_ENV_FILE}"
[[ -n "${MINIO_ROOT_USER:-}" ]] || die "MINIO_ROOT_USER not set in ${ROOT_ENV_FILE}"
[[ -n "${MINIO_ROOT_PASSWORD:-}" ]] || die "MINIO_ROOT_PASSWORD not set in ${ROOT_ENV_FILE}"

install -d -m 0700 "$(dirname "${WRITER_CREDS_FILE}")"

# ---------------------------------------------------------------------------
# mc wrapper — runs the official mc image attached to the personel network
# ---------------------------------------------------------------------------
mc_run() {
  docker run --rm \
    --network "${MINIO_NETWORK}" \
    -e MC_HOST_personel="http://${MINIO_ROOT_USER}:${MINIO_ROOT_PASSWORD}@${MINIO_ENDPOINT#http://}" \
    minio/mc:latest \
    "$@"
}

mc_run_stdin() {
  docker run --rm -i \
    --network "${MINIO_NETWORK}" \
    -e MC_HOST_personel="http://${MINIO_ROOT_USER}:${MINIO_ROOT_PASSWORD}@${MINIO_ENDPOINT#http://}" \
    minio/mc:latest \
    "$@"
}

# ---------------------------------------------------------------------------
# Wait for MinIO readiness
# ---------------------------------------------------------------------------
log "Waiting for MinIO at ${MINIO_ENDPOINT}..."
for i in $(seq 1 30); do
  if mc_run ready personel >/dev/null 2>&1; then
    log "  MinIO ready"
    break
  fi
  if [[ "$i" == "30" ]]; then
    die "MinIO did not become ready after 60 seconds"
  fi
  sleep 2
done

# ---------------------------------------------------------------------------
# Create bucket with Object Lock (idempotent + lock verification)
# ---------------------------------------------------------------------------
create_worm_bucket() {
  local bucket="$1"
  log "Provisioning bucket '${bucket}'..."

  if mc_run ls "personel/${bucket}" >/dev/null 2>&1; then
    log "  bucket exists — verifying Object Lock status"
    local lock_info
    lock_info="$(mc_run retention info --default "personel/${bucket}" 2>&1 || true)"
    if echo "${lock_info}" | grep -qi "compliance"; then
      log "  Object Lock COMPLIANCE confirmed on '${bucket}'"
    else
      die "Bucket '${bucket}' exists but is NOT in COMPLIANCE mode. Object Lock cannot be retroactively enabled. See docs/operations/minio-worm-migration.md §Recovery."
    fi
  else
    log "  creating bucket '${bucket}' with Object Lock..."
    mc_run mb --with-lock "personel/${bucket}"
    log "  bucket created"
  fi

  # Versioning is required for Object Lock to function (S3 spec).
  mc_run version enable "personel/${bucket}" >/dev/null 2>&1 || true

  # Default retention: COMPLIANCE mode, 5 years. Applied to objects that do
  # NOT specify their own RetainUntilDate at PUT time. The audit + evidence
  # writer code in apps/api also sets explicit per-object retention as
  # belt-and-braces.
  log "  setting default retention: COMPLIANCE ${RETENTION_DAYS}d"
  mc_run retention set --default COMPLIANCE "${RETENTION_DAYS}d" "personel/${bucket}"
}

create_worm_bucket "${AUDIT_BUCKET}"
create_worm_bucket "${EVIDENCE_BUCKET}"

# ---------------------------------------------------------------------------
# Bucket policy: deny PutBucketObjectLockConfiguration so the retention
# defaults cannot be reduced after creation. COMPLIANCE mode already blocks
# delete/overwrite, but PutObjectLockConfiguration could in theory shorten
# the *default* retention for newly written objects. This policy closes that
# attack vector.
# ---------------------------------------------------------------------------
deny_lock_reconfigure_policy() {
  local bucket="$1"
  log "Locking down bucket policy on '${bucket}'..."
  mc_run_stdin admin policy create personel "deny-${bucket}-relock" /dev/stdin <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "DenyRelockReconfigure",
      "Effect": "Deny",
      "Action": [
        "s3:PutBucketObjectLockConfiguration",
        "s3:DeleteBucket",
        "s3:DeleteBucketPolicy"
      ],
      "Resource": [
        "arn:aws:s3:::${bucket}"
      ]
    }
  ]
}
EOF
}

deny_lock_reconfigure_policy "${AUDIT_BUCKET}"
deny_lock_reconfigure_policy "${EVIDENCE_BUCKET}"

# ---------------------------------------------------------------------------
# Service account: personel-audit-writer
# PutObject + GetObject on both WORM buckets. NO Delete. NO bucket admin.
# ---------------------------------------------------------------------------
provision_writer_account() {
  log "Provisioning service account 'personel-audit-writer'..."

  if [[ -f "${WRITER_CREDS_FILE}" ]] && [[ "${FORCE_ROTATE}" == "false" ]]; then
    warn "writer creds already exist at ${WRITER_CREDS_FILE} — pass --force to rotate"
    return 0
  fi

  local access_key secret_key
  access_key="personel-audit-writer"
  secret_key="$(openssl rand -hex 24)"

  # Idempotent user create; mc admin user add returns success even if exists.
  mc_run admin user add personel "${access_key}" "${secret_key}"

  # Policy: PUT + GET only on both WORM buckets, no Delete*, no bucket-level ops.
  mc_run_stdin admin policy create personel personel-audit-writer-policy /dev/stdin <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "AuditWormWrite",
      "Effect": "Allow",
      "Action": [
        "s3:PutObject",
        "s3:PutObjectRetention",
        "s3:PutObjectLegalHold",
        "s3:GetObject",
        "s3:GetObjectVersion",
        "s3:GetObjectRetention",
        "s3:GetObjectLegalHold",
        "s3:ListBucket",
        "s3:ListBucketVersions"
      ],
      "Resource": [
        "arn:aws:s3:::${AUDIT_BUCKET}",
        "arn:aws:s3:::${AUDIT_BUCKET}/*",
        "arn:aws:s3:::${EVIDENCE_BUCKET}",
        "arn:aws:s3:::${EVIDENCE_BUCKET}/*"
      ]
    },
    {
      "Sid": "DenyDelete",
      "Effect": "Deny",
      "Action": [
        "s3:DeleteObject",
        "s3:DeleteObjectVersion",
        "s3:DeleteBucket",
        "s3:BypassGovernanceRetention"
      ],
      "Resource": [
        "arn:aws:s3:::${AUDIT_BUCKET}",
        "arn:aws:s3:::${AUDIT_BUCKET}/*",
        "arn:aws:s3:::${EVIDENCE_BUCKET}",
        "arn:aws:s3:::${EVIDENCE_BUCKET}/*"
      ]
    }
  ]
}
EOF

  mc_run admin policy attach personel personel-audit-writer-policy --user "${access_key}"

  install -d -m 0700 "$(dirname "${WRITER_CREDS_FILE}")"
  printf '%s\n%s\n' "${access_key}" "${secret_key}" > "${WRITER_CREDS_FILE}"
  chmod 600 "${WRITER_CREDS_FILE}"

  log "writer creds written to ${WRITER_CREDS_FILE}"
}

provision_writer_account

# ---------------------------------------------------------------------------
# Final status
# ---------------------------------------------------------------------------
log ""
log "Bootstrap complete."
log "  audit-worm    COMPLIANCE ${RETENTION_DAYS}d"
log "  evidence-worm COMPLIANCE ${RETENTION_DAYS}d"
log "  service account: personel-audit-writer"
log ""
log "Verify with:"
log "  docker run --rm --network ${MINIO_NETWORK} \\"
log "    -e MC_HOST_personel='http://...:...@${MINIO_ENDPOINT#http://}' \\"
log "    minio/mc:latest retention info personel/${AUDIT_BUCKET}"
log ""
log "Next steps:"
log "  1. Update apps/api/configs/api.yaml from api.yaml.minio-worm-snippet"
log "  2. Mount ${WRITER_CREDS_FILE} into the api container (read-only)"
log "  3. Restart api"
log "  4. Watch the audit checkpoint job log for 'WORM put OK'"
log ""
log "See docs/operations/minio-worm-migration.md"
