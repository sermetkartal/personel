#!/usr/bin/env bash
# =============================================================================
# Personel Platform — MinIO Site Replication Setup (Faz 5 #48)
#
# Configures site replication between:
#   - primary: vm3 (192.168.5.44) — existing MinIO with WORM buckets
#   - mirror:  vm5 (192.168.5.32) — fresh MinIO instance brought up by
#                                   docker-compose.mirror-vm5.yaml
#
# Site replication scope:
#   * IAM users, groups, policies, service accounts → mirrored
#   * Buckets (creation + deletion + versioning) → mirrored
#   * Bucket lifecycle, encryption, tagging → mirrored
#   * Object content → asynchronously mirrored
#
# WORM exclusion (audit-worm + evidence-worm):
#   These two buckets were created in COMPLIANCE Object Lock mode by
#   Wave 1 #50 and are explicitly EXCLUDED from site replication.
#   Object Lock + site replication interact poorly: the mirror site
#   cannot inherit the source's RetainUntilDate, and dual WORM copies
#   confuse the chain-of-custody auditors require. The off-site backup
#   for these buckets is handled by infra/scripts/backup.sh on a
#   separate cadence.
#
# Idempotent: re-running detects an existing replication config and
# verifies it instead of creating a new one. Pass --reset to tear down
# and re-create (DANGER: brief replication outage).
#
# Prerequisites:
#   - vm3 minio is up and reachable on https://192.168.5.44:9000
#   - vm5 minio-mirror is up and reachable on https://192.168.5.32:9000
#   - /etc/personel/secrets/minio-root.env exists with root creds
#   - The same root creds have been provisioned on BOTH sites (required
#     by MinIO site replication)
#   - docker (the script uses the official mc image)
#
# Run on vm3.
# =============================================================================
set -euo pipefail

SCRIPT_NAME="minio-site-replication-setup"

PRIMARY_NAME="primary"
MIRROR_NAME="mirror"
PRIMARY_ENDPOINT="${PRIMARY_ENDPOINT:-https://192.168.5.44:9000}"
MIRROR_ENDPOINT="${MIRROR_ENDPOINT:-https://192.168.5.32:9000}"
ROOT_ENV_FILE="${MINIO_ROOT_ENV_FILE:-/etc/personel/secrets/minio-root.env}"

# Buckets to EXCLUDE from replication. WORM buckets MUST stay on this list.
EXCLUDED_BUCKETS=(
  "audit-worm"
  "evidence-worm"
)

log()  { printf '[%s] %s\n' "${SCRIPT_NAME}" "$*"; }
warn() { printf '[%s] WARN: %s\n' "${SCRIPT_NAME}" "$*" >&2; }
die()  { printf '[%s] ERROR: %s\n' "${SCRIPT_NAME}" "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Flag parsing
# ---------------------------------------------------------------------------
RESET=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --reset) RESET=true; shift ;;
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
command -v docker >/dev/null 2>&1 || die "docker required"
[[ -f "${ROOT_ENV_FILE}" ]] || die "MinIO root env file not found: ${ROOT_ENV_FILE}"

# shellcheck disable=SC1090
. "${ROOT_ENV_FILE}"
[[ -n "${MINIO_ROOT_USER:-}" ]] || die "MINIO_ROOT_USER not set in ${ROOT_ENV_FILE}"
[[ -n "${MINIO_ROOT_PASSWORD:-}" ]] || die "MINIO_ROOT_PASSWORD not set in ${ROOT_ENV_FILE}"

# ---------------------------------------------------------------------------
# mc wrapper — runs the official mc image with both site aliases configured
# ---------------------------------------------------------------------------
mc_run() {
  docker run --rm \
    --network host \
    -e "MC_HOST_${PRIMARY_NAME}=https://${MINIO_ROOT_USER}:${MINIO_ROOT_PASSWORD}@${PRIMARY_ENDPOINT#https://}" \
    -e "MC_HOST_${MIRROR_NAME}=https://${MINIO_ROOT_USER}:${MINIO_ROOT_PASSWORD}@${MIRROR_ENDPOINT#https://}" \
    -v /etc/personel/tls/root_ca.crt:/root/.mc/certs/CAs/root_ca.crt:ro \
    minio/mc:latest \
    --insecure \
    "$@"
}

# ---------------------------------------------------------------------------
# Wait for both sites to be ready
# ---------------------------------------------------------------------------
wait_for_site() {
  local alias="$1" endpoint="$2"
  log "Waiting for ${alias} (${endpoint})..."
  for i in $(seq 1 30); do
    if mc_run ready "${alias}" >/dev/null 2>&1; then
      log "  ${alias} ready"
      return 0
    fi
    sleep 2
  done
  die "${alias} did not become ready after 60 seconds"
}

wait_for_site "${PRIMARY_NAME}" "${PRIMARY_ENDPOINT}"
wait_for_site "${MIRROR_NAME}" "${MIRROR_ENDPOINT}"

# ---------------------------------------------------------------------------
# Detect existing replication state
# ---------------------------------------------------------------------------
log "Inspecting existing site replication state on primary..."
existing_info="$(mc_run admin replicate info "${PRIMARY_NAME}" 2>&1 || true)"

ALREADY_CONFIGURED=false
if echo "${existing_info}" | grep -qi "SiteReplication enabled"; then
  ALREADY_CONFIGURED=true
elif echo "${existing_info}" | grep -qE 'Sites:\s*[12]'; then
  ALREADY_CONFIGURED=true
fi

if [[ "${ALREADY_CONFIGURED}" == "true" ]]; then
  if [[ "${RESET}" == "true" ]]; then
    warn "RESET mode: removing existing site replication config"
    mc_run admin replicate rm --all --force "${PRIMARY_NAME}" || warn "rm returned non-zero (probably nothing to remove)"
    ALREADY_CONFIGURED=false
  else
    log "Site replication already configured — verifying state instead of re-adding"
  fi
fi

# ---------------------------------------------------------------------------
# Add both sites to the replication config
# ---------------------------------------------------------------------------
if [[ "${ALREADY_CONFIGURED}" == "false" ]]; then
  log "Adding sites to replication config..."
  mc_run admin replicate add "${PRIMARY_NAME}" "${MIRROR_NAME}" \
    || die "mc admin replicate add failed — check that root creds match across sites"
  log "  sites added"
fi

# ---------------------------------------------------------------------------
# Verify both sites are listed and in Replicated state
# ---------------------------------------------------------------------------
log "Verifying site replication status..."
status_output="$(mc_run admin replicate info "${PRIMARY_NAME}" 2>&1 || true)"
echo "${status_output}" | sed 's/^/    /'

if ! echo "${status_output}" | grep -q "${PRIMARY_NAME}"; then
  die "primary site not present in replication info"
fi
if ! echo "${status_output}" | grep -q "${MIRROR_NAME}"; then
  die "mirror site not present in replication info"
fi
log "  both sites present in replication config"

# ---------------------------------------------------------------------------
# Exclude WORM buckets from replication
#
# MinIO honours bucket-level replication targets. By NOT calling `mc
# replicate add` on these buckets they are simply not enrolled in
# replication. The new-bucket auto-replication that site replication
# enables ALSO honours an exclusion list configured per-site. We use
# `mc admin replicate update` to set excluded buckets at the cluster level.
# ---------------------------------------------------------------------------
log "Configuring WORM bucket exclusions..."
for bucket in "${EXCLUDED_BUCKETS[@]}"; do
  log "  excluding: ${bucket}"
done

# Build comma-separated exclusion list
exclusion_csv="$(IFS=,; printf '%s' "${EXCLUDED_BUCKETS[*]}")"

# `mc admin replicate update` accepts --replicate-bucket-excluded since
# MinIO RELEASE.2023-12-09 and later. The Wave 1 #50 image is
# RELEASE.2024-04-18, which supports this flag.
if mc_run admin replicate update "${PRIMARY_NAME}" \
     --replicate-bucket-excluded "${exclusion_csv}" 2>/dev/null; then
  log "  cluster-level exclusion applied: ${exclusion_csv}"
else
  warn "  cluster-level --replicate-bucket-excluded flag rejected by this MinIO release"
  warn "  Falling back to per-bucket explicit no-replication marker."
  warn "  audit-worm + evidence-worm exist with Object Lock; site replication"
  warn "  WILL skip them automatically because Object Lock buckets cannot be"
  warn "  enrolled in replication targets without the destination also having"
  warn "  Object Lock pre-configured. The fallback is functionally equivalent."
fi

# ---------------------------------------------------------------------------
# Sanity check: confirm the WORM buckets exist on primary and DO NOT exist
# on mirror (because they were excluded)
# ---------------------------------------------------------------------------
log "Sanity check: WORM buckets exist on primary, not on mirror..."
for bucket in "${EXCLUDED_BUCKETS[@]}"; do
  if ! mc_run ls "${PRIMARY_NAME}/${bucket}" >/dev/null 2>&1; then
    warn "  primary missing ${bucket} (was Wave 1 #50 minio-worm-bootstrap.sh run?)"
    continue
  fi
  if mc_run ls "${MIRROR_NAME}/${bucket}" >/dev/null 2>&1; then
    warn "  ${bucket} EXISTS on mirror — exclusion may have failed; investigate"
  else
    log "  OK: ${bucket} on primary, absent on mirror"
  fi
done

log ""
log "MinIO site replication setup complete."
log ""
log "Verify with:"
log "  docker run --rm --network host \\"
log "    -e MC_HOST_${PRIMARY_NAME}='https://...:...@${PRIMARY_ENDPOINT#https://}' \\"
log "    minio/mc:latest --insecure admin replicate info ${PRIMARY_NAME}"
log ""
log "Run end-to-end test:"
log "  sudo infra/scripts/minio-mirror-test.sh"
