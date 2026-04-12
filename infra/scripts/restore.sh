#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Restore Script
# TR: UYARI: Bu betik mevcut veriyi siler. Onay olmadan çalışmaz.
#     Geri yükleme sırası: Vault → Postgres → ClickHouse → MinIO.
#     Her adımda health check yapılır.
# EN: WARNING: This script destroys current data. Requires explicit confirmation.
#     Restore order: Vault → Postgres → ClickHouse → MinIO.
#     Health check is performed after each step.
#
# Usage:
#   ./restore.sh --backup-dir /var/lib/personel/backups/2026-04-12-02
#   ./restore.sh --backup-dir /path/to/backup --service vault
#   ./restore.sh --backup-dir /path/to/backup --service postgres
#   ./restore.sh --backup-dir /path/to/backup --service clickhouse
#   ./restore.sh --backup-dir /path/to/backup --service minio
#   ./restore.sh --list    — list available backup directories
#   ./restore.sh --dry-run --backup-dir /path/to/backup
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_DIR="${SCRIPT_DIR}/../compose"
ENV_FILE="${COMPOSE_DIR}/.env"

# ---------------------------------------------------------------------------
# Flag parsing
# ---------------------------------------------------------------------------
BACKUP_DIR_ARG=""
SERVICE_FILTER="all"
DRY_RUN=false
LIST_MODE=false
SKIP_CONFIRM=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --backup-dir)   BACKUP_DIR_ARG="${2:?'--backup-dir requires a path'}"; shift 2 ;;
    --service)      SERVICE_FILTER="${2:?'--service requires a name'}"; shift 2 ;;
    --dry-run)      DRY_RUN=true; shift ;;
    --list)         LIST_MODE=true; shift ;;
    --yes|-y)       SKIP_CONFIRM=true; shift ;;
    --help|-h)
      sed -n '2,25p' "${BASH_SOURCE[0]}" | sed 's/^# \?//'
      exit 0
      ;;
    *)
      echo "[restore] ERROR: Unknown flag: $1" >&2; exit 1 ;;
  esac
done

# ---------------------------------------------------------------------------
# Load environment
# ---------------------------------------------------------------------------
[[ -f "${ENV_FILE}" ]] || { echo "[restore] ERROR: ${ENV_FILE} not found." >&2; exit 1; }
set -a
# shellcheck source=/dev/null
source "${ENV_FILE}"
set +a

BACKUP_ROOT="${BACKUP_DIR:-/var/lib/personel/backups}"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
RED='\033[0;31m'; YELLOW='\033[1;33m'; GREEN='\033[0;32m'; NC='\033[0m'; BOLD='\033[1m'
log()  { echo -e "${GREEN}[restore]${NC} $*"; }
warn() { echo -e "${YELLOW}[restore WARN]${NC} $*" >&2; }
die()  { echo -e "${RED}[restore ERROR]${NC} $*" >&2; exit 1; }
step() { echo -e "\n${BOLD}[restore] === $* ===${NC}"; }

# ---------------------------------------------------------------------------
# List mode
# ---------------------------------------------------------------------------
if [[ "${LIST_MODE}" == "true" ]]; then
  log "Available backups in ${BACKUP_ROOT}:"
  if [[ -d "${BACKUP_ROOT}" ]]; then
    find "${BACKUP_ROOT}" \
      -maxdepth 1 -mindepth 1 -type d \
      | sort -r \
      | while IFS= read -r dir; do
          size="$(du -sh "${dir}" 2>/dev/null | awk '{print $1}' || echo '?')"
          manifest="${dir}/MANIFEST.sha256"
          [[ -f "${manifest}" ]] && marker="(verified)" || marker="(no manifest)"
          printf '  %-40s  %6s  %s\n' "$(basename "${dir}")" "${size}" "${marker}"
        done
  else
    warn "Backup root ${BACKUP_ROOT} does not exist."
  fi
  exit 0
fi

# ---------------------------------------------------------------------------
# Validate backup dir
# ---------------------------------------------------------------------------
[[ -n "${BACKUP_DIR_ARG}" ]] || die "--backup-dir is required. Use --list to see available backups."
[[ -d "${BACKUP_DIR_ARG}" ]] || die "Backup directory not found: ${BACKUP_DIR_ARG}"

RESTORE_FROM="${BACKUP_DIR_ARG}"
log "Restore source: ${RESTORE_FROM}"

# Verify manifest if present
MANIFEST_FILE="${RESTORE_FROM}/MANIFEST.sha256"
if [[ -f "${MANIFEST_FILE}" ]]; then
  log "Verifying manifest checksums..."
  VERIFY_FAILED=false
  while IFS= read -r line; do
    [[ "${line}" =~ ^# ]] && continue
    [[ -z "${line}" ]] && continue

    # sha256sum format: <hash>  <filename> or <hash> <filename>
    expected_hash="$(echo "${line}" | awk '{print $1}')"
    filename="$(echo "${line}" | awk '{print $NF}')"

    if [[ -f "${filename}" ]]; then
      if command -v sha256sum >/dev/null 2>&1; then
        actual_hash="$(sha256sum "${filename}" | awk '{print $1}')"
      else
        actual_hash="$(shasum -a 256 "${filename}" | awk '{print $1}')"
      fi

      if [[ "${actual_hash}" != "${expected_hash}" ]]; then
        warn "Checksum mismatch: ${filename}"
        VERIFY_FAILED=true
      fi
    else
      warn "File in manifest not found: ${filename}"
    fi
  done < "${MANIFEST_FILE}"

  if [[ "${VERIFY_FAILED}" == "true" ]]; then
    die "Manifest verification failed. Backup may be corrupt. Aborting."
  fi
  log "Manifest checksums verified."
else
  warn "No MANIFEST.sha256 found — skipping checksum verification."
fi

# ---------------------------------------------------------------------------
# Dry run
# ---------------------------------------------------------------------------
if [[ "${DRY_RUN}" == "true" ]]; then
  log "DRY RUN — the following would be restored from ${RESTORE_FROM}:"

  [[ "${SERVICE_FILTER}" == "all" || "${SERVICE_FILTER}" == "vault" ]] && \
    [[ -f "${RESTORE_FROM}/vault-snapshot.snap" ]] && \
    log "  [1] Vault raft snapshot → restore"

  [[ "${SERVICE_FILTER}" == "all" || "${SERVICE_FILTER}" == "postgres" ]] && \
    log "  [2] Postgres databases"

  [[ "${SERVICE_FILTER}" == "all" || "${SERVICE_FILTER}" == "clickhouse" ]] && \
    log "  [3] ClickHouse schema"

  [[ "${SERVICE_FILTER}" == "all" || "${SERVICE_FILTER}" == "minio" ]] && \
    [[ -d "${RESTORE_FROM}/minio-mirror" ]] && \
    log "  [4] MinIO buckets from minio-mirror/"

  exit 0
fi

# ---------------------------------------------------------------------------
# Confirmation gate
# ---------------------------------------------------------------------------
if [[ "${SKIP_CONFIRM}" == "false" ]]; then
  echo ""
  echo -e "${RED}${BOLD}WARNING: This will DESTROY current data and replace it with the backup.${NC}"
  echo -e "  Backup: ${RESTORE_FROM}"
  echo -e "  Service filter: ${SERVICE_FILTER}"
  echo ""
  printf 'Type "yes-restore" to continue: '
  read -r CONFIRM
  [[ "${CONFIRM}" == "yes-restore" ]] || { log "Aborted by user."; exit 1; }
fi

LOG_DIR="${BACKUP_ROOT}/logs"
mkdir -p "${LOG_DIR}"
RESTORE_LOG="${LOG_DIR}/restore-$(date -u +"%Y-%m-%dT%H%M%SZ").log"
exec > >(tee -a "${RESTORE_LOG}") 2>&1

log "=== Personel Restore Started ==="
log "Source:  ${RESTORE_FROM}"
log "Filter:  ${SERVICE_FILTER}"
log "Log:     ${RESTORE_LOG}"

# ---------------------------------------------------------------------------
# Helper: health_check <service_name> <check_command>
# ---------------------------------------------------------------------------
health_check() {
  local service="$1" check_cmd="$2"
  local attempts=0 max=12 interval=5

  log "  Health check: ${service}..."
  while [[ "${attempts}" -lt "${max}" ]]; do
    if eval "${check_cmd}" >/dev/null 2>&1; then
      log "  ${service}: healthy."
      return 0
    fi
    attempts=$(( attempts + 1 ))
    log "  Waiting for ${service}... (${attempts}/${max})"
    sleep "${interval}"
  done

  die "${service} did not become healthy after $(( max * interval ))s. Check logs."
}

# ---------------------------------------------------------------------------
# Step 1: Vault restore
# ---------------------------------------------------------------------------
if [[ "${SERVICE_FILTER}" == "all" || "${SERVICE_FILTER}" == "vault" ]]; then
  step "Step 1: Vault"
  VAULT_SNAP="${RESTORE_FROM}/vault-snapshot.snap"

  if [[ ! -f "${VAULT_SNAP}" ]]; then
    warn "vault-snapshot.snap not found in ${RESTORE_FROM} — skipping Vault restore."
  else
    log "Restoring Vault raft snapshot..."

    if [[ -n "${VAULT_TOKEN:-}" ]]; then
      VAULT_ADDR="${VAULT_ADDR:-http://127.0.0.1:8200}" \
      VAULT_TOKEN="${VAULT_TOKEN}" \
      vault operator raft snapshot restore -force "${VAULT_SNAP}" \
        || die "Vault restore command failed."
    else
      log "VAULT_TOKEN not set — attempting restore via docker exec."
      docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" \
        cp "${VAULT_SNAP}" vault:/tmp/restore-snapshot.snap \
        2>/dev/null || die "Could not copy snapshot to Vault container."
      docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" \
        exec -T vault \
        vault operator raft snapshot restore -force /tmp/restore-snapshot.snap \
        || die "Vault snapshot restore failed via docker exec."
    fi

    log "Vault snapshot restored. Restarting Vault..."
    docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" restart vault

    health_check "Vault" \
      "curl -sf ${VAULT_ADDR:-http://127.0.0.1:8200}/v1/sys/health | grep -q '\\\"initialized\\\":true'"

    log "Vault restore complete."
    warn "NOTE: After Vault restore you must unseal Vault using infra/scripts/vault-unseal.sh"
  fi
fi

# ---------------------------------------------------------------------------
# Step 2: Postgres restore
# ---------------------------------------------------------------------------
if [[ "${SERVICE_FILTER}" == "all" || "${SERVICE_FILTER}" == "postgres" ]]; then
  step "Step 2: Postgres"

  export PGPASSWORD="${POSTGRES_PASSWORD:-}"
  PG_HOST="${POSTGRES_HOST:-localhost}"
  PG_PORT="${POSTGRES_PORT:-5432}"
  PG_USER="${POSTGRES_USER:-postgres}"

  health_check "Postgres" \
    "pg_isready -h ${PG_HOST} -p ${PG_PORT} -U ${PG_USER}"

  for dump_file in "${RESTORE_FROM}"/postgres-*.dump; do
    [[ -f "${dump_file}" ]] || continue
    # Extract db name from filename: postgres-<dbname>.dump
    db_name="$(basename "${dump_file}" .dump | sed 's/^postgres-//')"

    log "  Restoring database: ${db_name} from ${dump_file}..."

    # Drop and recreate database
    psql \
      --host="${PG_HOST}" \
      --port="${PG_PORT}" \
      --username="${PG_USER}" \
      --dbname=postgres \
      --command="SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '${db_name}' AND pid <> pg_backend_pid();" \
      >/dev/null 2>&1 || true

    psql \
      --host="${PG_HOST}" \
      --port="${PG_PORT}" \
      --username="${PG_USER}" \
      --dbname=postgres \
      --command="DROP DATABASE IF EXISTS \"${db_name}\";" \
      >/dev/null 2>&1 || warn "  Could not drop ${db_name} — it may not exist."

    psql \
      --host="${PG_HOST}" \
      --port="${PG_PORT}" \
      --username="${PG_USER}" \
      --dbname=postgres \
      --command="CREATE DATABASE \"${db_name}\";" \
      >/dev/null 2>&1 || warn "  Could not create ${db_name}."

    pg_restore \
      --host="${PG_HOST}" \
      --port="${PG_PORT}" \
      --username="${PG_USER}" \
      --dbname="${db_name}" \
      --no-owner \
      --no-privileges \
      --verbose \
      "${dump_file}" 2>&1 | tee -a "${RESTORE_LOG}" | tail -5 || \
      warn "  pg_restore reported errors for ${db_name} — check log."

    log "  ${db_name}: restored."
  done

  unset PGPASSWORD

  health_check "Postgres post-restore" \
    "pg_isready -h ${PG_HOST} -p ${PG_PORT} -U ${PG_USER}"

  log "Postgres restore complete."
fi

# ---------------------------------------------------------------------------
# Step 3: ClickHouse restore
# ---------------------------------------------------------------------------
if [[ "${SERVICE_FILTER}" == "all" || "${SERVICE_FILTER}" == "clickhouse" ]]; then
  step "Step 3: ClickHouse"

  CH_HOST="${CLICKHOUSE_HOST:-localhost}"
  CH_HTTP_PORT="${CLICKHOUSE_HTTP_PORT:-8123}"
  CH_USER="${CLICKHOUSE_USER:-personel_app}"
  CH_PASS="${CLICKHOUSE_PASSWORD:-}"
  CH_DB="${CLICKHOUSE_DB:-personel}"

  health_check "ClickHouse" \
    "curl -sf -u '${CH_USER}:${CH_PASS}' 'http://${CH_HOST}:${CH_HTTP_PORT}/ping' | grep -q 'Ok'"

  # Restore via RESTORE command (CH 22.4+) if backup files exist
  CH_BACKUP_GLOB="${RESTORE_FROM}/clickhouse/"
  if [[ -d "${CH_BACKUP_GLOB}" ]]; then
    log "  ClickHouse backup dir found: ${CH_BACKUP_GLOB}"

    # Restore schema from dump if available
    SCHEMA_FILE="${RESTORE_FROM}/clickhouse/schema.sql"
    if [[ -f "${SCHEMA_FILE}" ]]; then
      log "  Restoring ClickHouse schema..."
      curl -sf \
        -u "${CH_USER}:${CH_PASS}" \
        "http://${CH_HOST}:${CH_HTTP_PORT}/" \
        --data-binary @"${SCHEMA_FILE}" \
        || warn "  ClickHouse schema restore encountered errors."
      log "  Schema applied."
    else
      warn "  No schema.sql found — data must be restored from ClickHouse native backup."
      warn "  See: RESTORE DATABASE \`${CH_DB}\` FROM File('<backup-id>') in ClickHouse SQL."
    fi
  else
    warn "  No ClickHouse backup directory found in ${RESTORE_FROM} — skipping."
  fi

  health_check "ClickHouse post-restore" \
    "curl -sf -u '${CH_USER}:${CH_PASS}' 'http://${CH_HOST}:${CH_HTTP_PORT}/ping' | grep -q 'Ok'"

  log "ClickHouse restore step complete."
fi

# ---------------------------------------------------------------------------
# Step 4: MinIO restore
# ---------------------------------------------------------------------------
if [[ "${SERVICE_FILTER}" == "all" || "${SERVICE_FILTER}" == "minio" ]]; then
  step "Step 4: MinIO"

  MINIO_MIRROR_DIR="${RESTORE_FROM}/minio-mirror"

  if [[ ! -d "${MINIO_MIRROR_DIR}" ]]; then
    warn "minio-mirror/ not found in ${RESTORE_FROM} — skipping MinIO restore."
  else
    if ! command -v mc >/dev/null 2>&1; then
      warn "mc (MinIO client) not installed — skipping MinIO restore."
    else
      MC_ALIAS="personel-restore"
      MINIO_ENDPOINT="http://localhost:${MINIO_PORT:-9000}"
      MINIO_USER="${MINIO_BACKUP_ACCESS_KEY:-}"
      MINIO_PASS="${MINIO_BACKUP_SECRET_KEY:-}"

      if [[ -z "${MINIO_USER}" ]] || [[ -z "${MINIO_PASS}" ]]; then
        warn "MINIO_BACKUP_ACCESS_KEY/SECRET not set — skipping MinIO restore."
      else
        health_check "MinIO" \
          "mc ready ${MC_ALIAS} --quiet 2>/dev/null"

        mc alias set "${MC_ALIAS}" \
          "${MINIO_ENDPOINT}" \
          "${MINIO_USER}" \
          "${MINIO_PASS}" \
          --api S3v4 \
          >/dev/null 2>&1

        for bucket_dir in "${MINIO_MIRROR_DIR}"/*/; do
          [[ -d "${bucket_dir}" ]] || continue
          bucket="$(basename "${bucket_dir}")"

          log "  Restoring bucket: ${bucket}..."

          # Create bucket if not exists
          mc mb --ignore-existing "${MC_ALIAS}/${bucket}" >/dev/null 2>&1 || true

          # Mirror from backup to MinIO
          mc mirror \
            --preserve \
            --overwrite \
            "${bucket_dir}" \
            "${MC_ALIAS}/${bucket}" \
            2>&1 | tee -a "${RESTORE_LOG}" | tail -3 || \
            warn "  mc mirror had errors for ${bucket}."

          log "  ${bucket}: restored."
        done
      fi
    fi
  fi

  log "MinIO restore step complete."
fi

# ---------------------------------------------------------------------------
# Final health summary
# ---------------------------------------------------------------------------
step "Final Health Summary"

declare -a HEALTH_STATUS

check_service() {
  local name="$1" cmd="$2"
  if eval "${cmd}" >/dev/null 2>&1; then
    log "  [OK]   ${name}"
    HEALTH_STATUS+=("OK:${name}")
  else
    warn "  [FAIL] ${name} — check manually"
    HEALTH_STATUS+=("FAIL:${name}")
  fi
}

check_service "Vault" \
  "curl -sf '${VAULT_ADDR:-http://127.0.0.1:8200}/v1/sys/health' | grep -q 'initialized'"

check_service "Postgres" \
  "pg_isready -h '${POSTGRES_HOST:-localhost}' -p '${POSTGRES_PORT:-5432}' -U '${POSTGRES_USER:-postgres}'"

check_service "ClickHouse" \
  "curl -sf -u '${CLICKHOUSE_USER:-personel_app}:${CLICKHOUSE_PASSWORD:-}' 'http://${CLICKHOUSE_HOST:-localhost}:${CLICKHOUSE_HTTP_PORT:-8123}/ping' | grep -q 'Ok'"

# Count failures
FAIL_COUNT=0
for s in "${HEALTH_STATUS[@]}"; do
  [[ "${s}" == FAIL:* ]] && FAIL_COUNT=$(( FAIL_COUNT + 1 ))
done

log ""
log "=== Restore Complete ==="
log "Source:   ${RESTORE_FROM}"
log "Log:      ${RESTORE_LOG}"

if [[ "${FAIL_COUNT}" -gt 0 ]]; then
  warn "${FAIL_COUNT} service(s) failed health check. Review log: ${RESTORE_LOG}"
  exit 1
fi

log "All services healthy. Restore successful."

if [[ "${SERVICE_FILTER}" == "all" ]] || [[ "${SERVICE_FILTER}" == "vault" ]]; then
  warn "If Vault was restored, run: infra/scripts/vault-unseal.sh"
fi
