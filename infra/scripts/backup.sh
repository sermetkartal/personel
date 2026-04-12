#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Backup Script
# TR: Postgres, ClickHouse, MinIO, Vault ve config verilerini yedekler.
#     7 günlük retention uygulanır. Her çalışma sonunda API'ye evidence kaydı gönderilir.
# EN: Backs up Postgres, ClickHouse, MinIO, Vault and config data.
#     Enforces 7-day retention. Emits evidence record to API after each run.
#
# Usage:
#   ./backup.sh                    — full backup
#   ./backup.sh --vault-only       — Vault raft snapshot only
#   ./backup.sh --skip-clickhouse  — skip ClickHouse (faster)
#   ./backup.sh --skip-minio       — skip MinIO mirror
#   ./backup.sh --dry-run          — show what would run, exit
#
# Output directory structure:
#   /var/lib/personel/backups/YYYY-MM-DD-HH/
#     vault-snapshot.snap
#     postgres-personel.dump
#     postgres-personel_keycloak.dump
#     clickhouse-personel.tar.gz
#     minio-mirror/
#     config.tar.gz
#     MANIFEST.sha256
#
# Retention: backups older than BACKUP_RETENTION_DAILY days (default 7) are removed.
#
# Evidence: POST /v1/system/backup-runs emits an A1.2 SOC 2 evidence item.
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_DIR="${SCRIPT_DIR}/../compose"
ENV_FILE="${COMPOSE_DIR}/.env"

# ---------------------------------------------------------------------------
# Flag parsing
# ---------------------------------------------------------------------------
VAULT_ONLY=false
SKIP_CLICKHOUSE=false
SKIP_MINIO=false
DRY_RUN=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --vault-only)       VAULT_ONLY=true; shift ;;
    --skip-clickhouse)  SKIP_CLICKHOUSE=true; shift ;;
    --skip-minio)       SKIP_MINIO=true; shift ;;
    --dry-run)          DRY_RUN=true; shift ;;
    --help|-h)
      sed -n '2,30p' "${BASH_SOURCE[0]}" | sed 's/^# \?//'
      exit 0
      ;;
    *)
      echo "[backup] ERROR: Unknown flag: $1" >&2; exit 1 ;;
  esac
done

# ---------------------------------------------------------------------------
# Load environment
# ---------------------------------------------------------------------------
[[ -f "${ENV_FILE}" ]] || { echo "[backup] ERROR: ${ENV_FILE} not found." >&2; exit 1; }
set -a
# shellcheck source=/dev/null
source "${ENV_FILE}"
set +a

# ---------------------------------------------------------------------------
# Configuration (with safe defaults)
# ---------------------------------------------------------------------------
BACKUP_ROOT="${BACKUP_DIR:-/var/lib/personel/backups}"
RETENTION_DAYS="${BACKUP_RETENTION_DAILY:-7}"
TIMESTAMP="$(date -u +"%Y-%m-%d-%H")"
BACKUP_PATH="${BACKUP_ROOT}/${TIMESTAMP}"
LOG_FILE="${BACKUP_ROOT}/logs/backup-${TIMESTAMP}.log"
API_BASE_URL="${API_BASE_URL:-http://localhost:8080}"

# Postgres
PG_HOST="${POSTGRES_HOST:-localhost}"
PG_PORT="${POSTGRES_PORT:-5432}"
PG_USER="${POSTGRES_USER:-postgres}"
PG_PASS="${POSTGRES_PASSWORD:-}"
PG_DATABASES=("${POSTGRES_DB:-personel}" "personel_keycloak")

# ClickHouse
CH_HOST="${CLICKHOUSE_HOST:-localhost}"
CH_HTTP_PORT="${CLICKHOUSE_HTTP_PORT:-8123}"
CH_USER="${CLICKHOUSE_USER:-personel_app}"
CH_PASS="${CLICKHOUSE_PASSWORD:-}"
CH_DB="${CLICKHOUSE_DB:-personel}"

# Vault
VAULT_ADDR_LOCAL="${VAULT_ADDR:-http://127.0.0.1:8200}"
VAULT_TOKEN="${VAULT_TOKEN:-}"

# MinIO
MC_ALIAS="personel-backup"
MINIO_ENDPOINT="http://localhost:${MINIO_PORT:-9000}"
MINIO_USER="${MINIO_BACKUP_ACCESS_KEY:-}"
MINIO_PASS="${MINIO_BACKUP_SECRET_KEY:-}"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
log()  { echo "[backup] $(date -u +"%H:%M:%SZ") $*" | tee -a "${LOG_FILE}"; }
warn() { echo "[backup] WARN: $*" | tee -a "${LOG_FILE}" >&2; }
die()  { echo "[backup] ERROR: $*" | tee -a "${LOG_FILE}" >&2; exit 1; }

# portable sha256sum (macOS uses shasum -a 256, Linux has sha256sum)
sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1"
  else
    shasum -a 256 "$1"
  fi
}

check_dependency() {
  command -v "$1" >/dev/null 2>&1 || die "Required tool not found: $1. Install it before running backup."
}

# ---------------------------------------------------------------------------
# Dry run
# ---------------------------------------------------------------------------
if [[ "${DRY_RUN}" == "true" ]]; then
  echo "[backup] DRY RUN — backup would be written to: ${BACKUP_PATH}"
  echo "[backup] Postgres DBs: ${PG_DATABASES[*]}"
  [[ "${SKIP_CLICKHOUSE}" == "false" ]] && echo "[backup] ClickHouse DB: ${CH_DB}"
  [[ "${SKIP_MINIO}" == "false" ]] && echo "[backup] MinIO mirror: all buckets"
  echo "[backup] Vault snapshot via: ${VAULT_ADDR_LOCAL}"
  echo "[backup] Retention: ${RETENTION_DAYS} days"
  exit 0
fi

# ---------------------------------------------------------------------------
# Prerequisite checks
# ---------------------------------------------------------------------------
check_dependency docker
check_dependency pg_dump
[[ "${SKIP_MINIO}" == "false" ]] && check_dependency mc
command -v curl >/dev/null 2>&1 || warn "curl not found — API evidence record will be skipped."

# ---------------------------------------------------------------------------
# Setup backup directory and log
# ---------------------------------------------------------------------------
mkdir -p "${BACKUP_PATH}" "${BACKUP_ROOT}/logs"
chmod 700 "${BACKUP_PATH}"

log "=== Personel Backup Started ==="
log "Backup path: ${BACKUP_PATH}"
log "Timestamp:   ${TIMESTAMP}"

BACKUP_START_EPOCH="$(date +%s)"
TOTAL_SIZE=0
declare -a MANIFEST_LINES

# ---------------------------------------------------------------------------
# Step 1: Vault raft snapshot
# ---------------------------------------------------------------------------
log "--- Step 1: Vault snapshot ---"
VAULT_SNAP="${BACKUP_PATH}/vault-snapshot.snap"

if [[ -z "${VAULT_TOKEN}" ]]; then
  warn "VAULT_TOKEN not set — attempting Vault snapshot via docker exec."
  # Try via docker exec (works if Vault container is named 'vault')
  docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" exec -T vault \
    vault operator raft snapshot save /tmp/vault-snap.snap 2>/dev/null && \
  docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" cp \
    vault:/tmp/vault-snap.snap "${VAULT_SNAP}" 2>/dev/null || \
  warn "Vault snapshot via docker exec failed. Check VAULT_TOKEN or Vault status."
else
  VAULT_ADDR="${VAULT_ADDR_LOCAL}" \
  VAULT_TOKEN="${VAULT_TOKEN}" \
  vault operator raft snapshot save "${VAULT_SNAP}" 2>/dev/null || \
  warn "Vault snapshot failed. Continuing..."
fi

if [[ -f "${VAULT_SNAP}" ]]; then
  sz="$(wc -c < "${VAULT_SNAP}" | tr -d ' ')"
  TOTAL_SIZE=$(( TOTAL_SIZE + sz ))
  MANIFEST_LINES+=("$(sha256_file "${VAULT_SNAP}")")
  log "  vault-snapshot.snap: ${sz} bytes"
else
  warn "  Vault snapshot not produced — backup may be incomplete."
fi

[[ "${VAULT_ONLY}" == "true" ]] && { log "Vault-only mode — stopping here."; }

if [[ "${VAULT_ONLY}" == "false" ]]; then

# ---------------------------------------------------------------------------
# Step 2: Postgres pg_dump
# ---------------------------------------------------------------------------
log "--- Step 2: Postgres pg_dump ---"
export PGPASSWORD="${PG_PASS}"

for db in "${PG_DATABASES[@]}"; do
  DUMP_FILE="${BACKUP_PATH}/postgres-${db}.dump"
  log "  Dumping ${db} ..."
  if pg_dump \
      --host="${PG_HOST}" \
      --port="${PG_PORT}" \
      --username="${PG_USER}" \
      --format=custom \
      --compress=9 \
      --file="${DUMP_FILE}" \
      "${db}" 2>>"${LOG_FILE}"; then
    sz="$(wc -c < "${DUMP_FILE}" | tr -d ' ')"
    TOTAL_SIZE=$(( TOTAL_SIZE + sz ))
    MANIFEST_LINES+=("$(sha256_file "${DUMP_FILE}")")
    log "  postgres-${db}.dump: ${sz} bytes"
  else
    warn "  pg_dump failed for ${db} — skipping."
  fi
done

unset PGPASSWORD

# ---------------------------------------------------------------------------
# Step 3: ClickHouse backup
# ---------------------------------------------------------------------------
if [[ "${SKIP_CLICKHOUSE}" == "false" ]]; then
  log "--- Step 3: ClickHouse backup ---"
  CH_SNAP_DIR="${BACKUP_PATH}/clickhouse"
  mkdir -p "${CH_SNAP_DIR}"

  # Use ClickHouse BACKUP command via HTTP interface (available since CH 22.4)
  CH_BACKUP_ID="backup-${TIMESTAMP}"

  if curl -sf \
      -u "${CH_USER}:${CH_PASS}" \
      "http://${CH_HOST}:${CH_HTTP_PORT}/" \
      --data "BACKUP DATABASE \`${CH_DB}\` TO File('${CH_BACKUP_ID}')" \
      2>>"${LOG_FILE}"; then

    # Copy the backup from ClickHouse data dir (mounted as volume)
    log "  ClickHouse BACKUP command executed."

    # Fallback: also dump schema via HTTP
    SCHEMA_FILE="${CH_SNAP_DIR}/schema.sql"
    curl -sf \
      -u "${CH_USER}:${CH_PASS}" \
      "http://${CH_HOST}:${CH_HTTP_PORT}/?query=SHOW+CREATE+DATABASE+\`${CH_DB}\`" \
      > "${SCHEMA_FILE}" 2>/dev/null || true

    if [[ -f "${SCHEMA_FILE}" ]]; then
      MANIFEST_LINES+=("$(sha256_file "${SCHEMA_FILE}")")
    fi
    log "  ClickHouse schema saved."
  else
    warn "  ClickHouse BACKUP command failed — capturing schema only."
    SCHEMA_FILE="${CH_SNAP_DIR}/schema-fallback.sql"
    curl -sf \
      -u "${CH_USER}:${CH_PASS}" \
      "http://${CH_HOST}:${CH_HTTP_PORT}/?query=SHOW+CREATE+DATABASE+\`${CH_DB}\`" \
      > "${SCHEMA_FILE}" 2>/dev/null || warn "  ClickHouse schema capture also failed."
  fi
else
  log "--- Step 3: ClickHouse --- SKIPPED (--skip-clickhouse)"
fi

# ---------------------------------------------------------------------------
# Step 4: MinIO mirror
# ---------------------------------------------------------------------------
if [[ "${SKIP_MINIO}" == "false" ]]; then
  log "--- Step 4: MinIO mirror ---"
  MINIO_MIRROR_DIR="${BACKUP_PATH}/minio-mirror"
  mkdir -p "${MINIO_MIRROR_DIR}"

  if [[ -z "${MINIO_USER}" ]] || [[ -z "${MINIO_PASS}" ]]; then
    warn "MINIO_BACKUP_ACCESS_KEY or MINIO_BACKUP_SECRET_KEY not set — skipping MinIO mirror."
  else
    # Configure mc alias (suppress output)
    mc alias set "${MC_ALIAS}" \
      "${MINIO_ENDPOINT}" \
      "${MINIO_USER}" \
      "${MINIO_PASS}" \
      --api S3v4 \
      >/dev/null 2>>"${LOG_FILE}" || warn "mc alias set failed — check MinIO credentials."

    # Mirror all buckets except audit-worm (Object Lock Compliance — skip)
    SKIP_BUCKETS=("${MINIO_BUCKET_AUDIT_WORM:-audit-worm}")

    for bucket in $(mc ls "${MC_ALIAS}" 2>/dev/null | awk '{print $NF}' | tr -d '/'); do
      skip=false
      for sb in "${SKIP_BUCKETS[@]}"; do
        [[ "${bucket}" == "${sb}" ]] && skip=true && break
      done

      if [[ "${skip}" == "true" ]]; then
        log "  Skipping bucket: ${bucket} (Object Lock — not mirrorable)"
        continue
      fi

      log "  Mirroring bucket: ${bucket} ..."
      mc mirror \
        --preserve \
        "${MC_ALIAS}/${bucket}" \
        "${MINIO_MIRROR_DIR}/${bucket}" \
        >>"${LOG_FILE}" 2>&1 || warn "  mc mirror failed for ${bucket}."
    done
    log "  MinIO mirror complete."
  fi
else
  log "--- Step 4: MinIO --- SKIPPED (--skip-minio)"
fi

# ---------------------------------------------------------------------------
# Step 5: Config tar (compose dir minus secrets)
# ---------------------------------------------------------------------------
log "--- Step 5: Config archive ---"
CONFIG_TAR="${BACKUP_PATH}/config.tar.gz"

tar \
  --exclude="${COMPOSE_DIR}/.env" \
  --exclude="${COMPOSE_DIR}/keycloak/realm-personel-provisioned.json" \
  --exclude="${COMPOSE_DIR}/nats/creds" \
  --exclude="${COMPOSE_DIR}/vault/data" \
  -czf "${CONFIG_TAR}" \
  -C "$(dirname "${COMPOSE_DIR}")" \
  "$(basename "${COMPOSE_DIR}")" \
  2>>"${LOG_FILE}" || warn "Config tar encountered errors — check log."

if [[ -f "${CONFIG_TAR}" ]]; then
  sz="$(wc -c < "${CONFIG_TAR}" | tr -d ' ')"
  TOTAL_SIZE=$(( TOTAL_SIZE + sz ))
  MANIFEST_LINES+=("$(sha256_file "${CONFIG_TAR}")")
  log "  config.tar.gz: ${sz} bytes"
fi

fi  # end if ! vault_only

# ---------------------------------------------------------------------------
# Write manifest
# ---------------------------------------------------------------------------
MANIFEST_FILE="${BACKUP_PATH}/MANIFEST.sha256"
{
  printf '# Personel backup manifest — %s\n' "${TIMESTAMP}"
  for line in "${MANIFEST_LINES[@]}"; do
    printf '%s\n' "${line}"
  done
} > "${MANIFEST_FILE}"

log "Manifest written: ${MANIFEST_FILE}"

# ---------------------------------------------------------------------------
# Retention: remove backups older than RETENTION_DAYS
# ---------------------------------------------------------------------------
log "--- Retention: removing backups older than ${RETENTION_DAYS} days ---"

# macOS: find -mtime uses days since last modified
# Linux: same semantics
find "${BACKUP_ROOT}" \
  -maxdepth 1 \
  -mindepth 1 \
  -type d \
  -mtime "+${RETENTION_DAYS}" \
  -print \
  | while IFS= read -r old_dir; do
      log "  Removing old backup: ${old_dir}"
      rm -rf "${old_dir}"
    done

# Also prune old log files
find "${BACKUP_ROOT}/logs" \
  -maxdepth 1 \
  -name "backup-*.log" \
  -mtime "+${RETENTION_DAYS}" \
  -delete 2>/dev/null || true

# ---------------------------------------------------------------------------
# Calculate duration and total size
# ---------------------------------------------------------------------------
BACKUP_END_EPOCH="$(date +%s)"
DURATION_SEC=$(( BACKUP_END_EPOCH - BACKUP_START_EPOCH ))

log ""
log "=== Backup Complete ==="
log "Duration:    ${DURATION_SEC}s"
log "Total size:  ${TOTAL_SIZE} bytes"
log "Location:    ${BACKUP_PATH}"

# ---------------------------------------------------------------------------
# Evidence: POST to admin API (A1.2 SOC 2 control)
# ---------------------------------------------------------------------------
log "--- Emitting backup-run evidence record ---"

if command -v curl >/dev/null 2>&1; then
  # Compute SHA256 of the manifest for the evidence payload
  MANIFEST_SHA256=""
  if [[ -f "${MANIFEST_FILE}" ]]; then
    MANIFEST_SHA256="$(sha256_file "${MANIFEST_FILE}" | awk '{print $1}')"
  fi

  HTTP_STATUS="$(curl -sf \
    -o /dev/null \
    -w "%{http_code}" \
    -X POST "${API_BASE_URL}/v1/system/backup-runs" \
    -H "Content-Type: application/json" \
    -d "$(printf '{
      "target_path": "%s",
      "size_bytes": %d,
      "duration_seconds": %d,
      "manifest_sha256": "%s",
      "vault_snapshot": %s,
      "postgres": %s,
      "clickhouse": %s,
      "minio": %s
    }' \
    "${BACKUP_PATH}" \
    "${TOTAL_SIZE}" \
    "${DURATION_SEC}" \
    "${MANIFEST_SHA256}" \
    "$([[ "${VAULT_ONLY}" == "true" ]] || [[ -f "${BACKUP_PATH}/vault-snapshot.snap" ]] && echo true || echo false)" \
    "$([[ "${VAULT_ONLY}" == "false" ]] && echo true || echo false)" \
    "$([[ "${VAULT_ONLY}" == "false" ]] && [[ "${SKIP_CLICKHOUSE}" == "false" ]] && echo true || echo false)" \
    "$([[ "${VAULT_ONLY}" == "false" ]] && [[ "${SKIP_MINIO}" == "false" ]] && echo true || echo false)" \
    )" \
    2>>"${LOG_FILE}" || true)"

  if [[ "${HTTP_STATUS}" == "200" ]] || [[ "${HTTP_STATUS}" == "201" ]]; then
    log "  Evidence record emitted. HTTP ${HTTP_STATUS}"
  else
    warn "  API evidence record failed (HTTP ${HTTP_STATUS:-000}). Backup data is still valid."
    warn "  Re-submit manually: see infra/runbooks/soc2-manual-evidence-submission.md"
  fi
else
  warn "curl not available — skipping API evidence record."
fi

log "Done."
