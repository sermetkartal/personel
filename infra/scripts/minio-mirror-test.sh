#!/usr/bin/env bash
# =============================================================================
# Personel Platform — MinIO Site Replication Validation (Faz 5 #48)
#
# End-to-end test that an object PUT on vm3 (primary) is replicated to
# vm5 (mirror) within the configured propagation window. Steps:
#
#   1. Generate a 1 MiB random test object
#   2. Compute its SHA-256 hash
#   3. Upload it to primary/screenshots/cluster_test/<ts>.bin
#   4. Poll mirror/screenshots/cluster_test/<ts>.bin every 2s, up to 30s
#   5. Download the mirrored copy and verify the SHA-256 matches
#   6. Clean up the test object on primary (replication will propagate the
#      delete to the mirror)
#   7. Exit 0 on success, 1 on any failure
#
# The `screenshots` bucket is used because it is one of the standard
# Personel non-WORM buckets created by Wave 1 minio-init and therefore
# IS enrolled in site replication (unlike audit-worm + evidence-worm).
#
# Run on vm3.
# =============================================================================
set -euo pipefail

SCRIPT_NAME="minio-mirror-test"

PRIMARY_NAME="primary"
MIRROR_NAME="mirror"
PRIMARY_ENDPOINT="${PRIMARY_ENDPOINT:-https://192.168.5.44:9000}"
MIRROR_ENDPOINT="${MIRROR_ENDPOINT:-https://192.168.5.32:9000}"
ROOT_ENV_FILE="${MINIO_ROOT_ENV_FILE:-/etc/personel/secrets/minio-root.env}"

TEST_BUCKET="screenshots"
PROPAGATION_WAIT_SECONDS=30

log()  { printf '[%s] %s\n' "${SCRIPT_NAME}" "$*"; }
warn() { printf '[%s] WARN: %s\n' "${SCRIPT_NAME}" "$*" >&2; }
fail() { printf '[%s] FAIL: %s\n' "${SCRIPT_NAME}" "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Prerequisites
# ---------------------------------------------------------------------------
command -v docker   >/dev/null 2>&1 || fail "docker required"
command -v openssl  >/dev/null 2>&1 || fail "openssl required"
command -v sha256sum >/dev/null 2>&1 || fail "sha256sum required"

[[ -f "${ROOT_ENV_FILE}" ]] || fail "MinIO root env file not found: ${ROOT_ENV_FILE}"
# shellcheck disable=SC1090
. "${ROOT_ENV_FILE}"
[[ -n "${MINIO_ROOT_USER:-}" ]] || fail "MINIO_ROOT_USER not set"
[[ -n "${MINIO_ROOT_PASSWORD:-}" ]] || fail "MINIO_ROOT_PASSWORD not set"

# ---------------------------------------------------------------------------
# Workspace + cleanup trap
# ---------------------------------------------------------------------------
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "${WORK_DIR}"' EXIT

TS="$(date -u +%Y%m%dT%H%M%S%NZ)"
SRC_FILE="${WORK_DIR}/src.bin"
DST_FILE="${WORK_DIR}/dst.bin"
OBJECT_KEY="cluster_test/mirror_${TS}.bin"

# ---------------------------------------------------------------------------
# mc wrapper — host network so mc can reach both 192.168.5.44 + .5.32
# ---------------------------------------------------------------------------
mc_run() {
  docker run --rm \
    --network host \
    -v "${WORK_DIR}:/work" \
    -v /etc/personel/tls/root_ca.crt:/root/.mc/certs/CAs/root_ca.crt:ro \
    -e "MC_HOST_${PRIMARY_NAME}=https://${MINIO_ROOT_USER}:${MINIO_ROOT_PASSWORD}@${PRIMARY_ENDPOINT#https://}" \
    -e "MC_HOST_${MIRROR_NAME}=https://${MINIO_ROOT_USER}:${MINIO_ROOT_PASSWORD}@${MIRROR_ENDPOINT#https://}" \
    minio/mc:latest \
    --insecure \
    "$@"
}

# ---------------------------------------------------------------------------
# Step 1-2: random payload + hash
# ---------------------------------------------------------------------------
log "Generating 1 MiB random payload..."
openssl rand -out "${SRC_FILE}" 1048576
SRC_HASH="$(sha256sum "${SRC_FILE}" | awk '{print $1}')"
log "  sha256: ${SRC_HASH}"

# ---------------------------------------------------------------------------
# Step 3: PUT to primary
# ---------------------------------------------------------------------------
log "Uploading to ${PRIMARY_NAME}/${TEST_BUCKET}/${OBJECT_KEY}..."
if ! mc_run cp "/work/$(basename "${SRC_FILE}")" "${PRIMARY_NAME}/${TEST_BUCKET}/${OBJECT_KEY}" >/dev/null; then
  fail "upload to primary failed"
fi
log "  upload OK"

# ---------------------------------------------------------------------------
# Step 4: poll mirror
# ---------------------------------------------------------------------------
log "Polling ${MIRROR_NAME}/${TEST_BUCKET}/${OBJECT_KEY} (timeout ${PROPAGATION_WAIT_SECONDS}s)..."
FOUND=false
for i in $(seq 1 $((PROPAGATION_WAIT_SECONDS / 2))); do
  if mc_run stat "${MIRROR_NAME}/${TEST_BUCKET}/${OBJECT_KEY}" >/dev/null 2>&1; then
    FOUND=true
    log "  mirror has the object after $((i * 2))s"
    break
  fi
  sleep 2
done

if [[ "${FOUND}" != "true" ]]; then
  warn "Object did not appear on mirror within ${PROPAGATION_WAIT_SECONDS}s — cleaning up before failing"
  mc_run rm "${PRIMARY_NAME}/${TEST_BUCKET}/${OBJECT_KEY}" >/dev/null 2>&1 || true
  fail "site replication propagation timeout"
fi

# ---------------------------------------------------------------------------
# Step 5: download + verify hash
# ---------------------------------------------------------------------------
log "Downloading mirrored copy..."
if ! mc_run cp "${MIRROR_NAME}/${TEST_BUCKET}/${OBJECT_KEY}" "/work/$(basename "${DST_FILE}")" >/dev/null; then
  mc_run rm "${PRIMARY_NAME}/${TEST_BUCKET}/${OBJECT_KEY}" >/dev/null 2>&1 || true
  fail "download from mirror failed"
fi

DST_HASH="$(sha256sum "${DST_FILE}" | awk '{print $1}')"
log "  mirror sha256: ${DST_HASH}"

if [[ "${SRC_HASH}" != "${DST_HASH}" ]]; then
  mc_run rm "${PRIMARY_NAME}/${TEST_BUCKET}/${OBJECT_KEY}" >/dev/null 2>&1 || true
  fail "hash mismatch: primary=${SRC_HASH} mirror=${DST_HASH}"
fi

# ---------------------------------------------------------------------------
# Step 6: cleanup (replication will mirror the delete)
# ---------------------------------------------------------------------------
log "Cleaning up test object on primary (delete will replicate)..."
mc_run rm "${PRIMARY_NAME}/${TEST_BUCKET}/${OBJECT_KEY}" >/dev/null 2>&1 || \
  warn "cleanup delete failed; manual cleanup may be needed"

log ""
log "PASS: object PUT on primary appeared identically on mirror"
exit 0
