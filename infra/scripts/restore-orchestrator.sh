#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Production Restore Orchestrator (Roadmap #58)
# =============================================================================
# Companion to backup-orchestrator.sh. This script supersedes the older
# infra/scripts/restore.sh (kept for compatibility).
#
# SAFETY:
#   * Refuses to run if the target service is currently UP — operator must
#     `docker compose stop <svc>` first. This prevents accidental write to
#     a live volume.
#   * Logs every step to /var/backups/personel/logs/restore-<ts>.log
#   * Operates on a single component per invocation. Multi-component restore
#     requires multiple invocations in dependency order:
#       vault → postgres → clickhouse → keycloak → minio
#
# Usage:
#   ./restore-orchestrator.sh --component pg         --backup-id 2026-04-13T02-00-00Z
#   ./restore-orchestrator.sh --component clickhouse --backup-id 2026-04-13T02-00-00Z
#   ./restore-orchestrator.sh --component vault      --backup-id 2026-04-13T02-00-00Z
#   ./restore-orchestrator.sh --component keycloak   --backup-id 2026-04-13T02-00-00Z
#   ./restore-orchestrator.sh --list pg              # list available pg backups
#   ./restore-orchestrator.sh --pitr-target '2026-04-13 14:30:00 UTC'  --component pg \
#                             --backup-id 2026-04-13T02-00-00Z
#
# Environment:
#   BACKUP_ROOT        default /var/backups/personel
# =============================================================================
set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_DIR="${SCRIPT_DIR}/../compose"
ENV_FILE="${COMPOSE_DIR}/.env"
BACKUP_ROOT="${BACKUP_ROOT:-/var/backups/personel}"

COMPONENT=""
BACKUP_ID=""
PITR_TARGET=""
LIST_MODE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --component)   COMPONENT="$2"; shift 2 ;;
    --backup-id)   BACKUP_ID="$2"; shift 2 ;;
    --pitr-target) PITR_TARGET="$2"; shift 2 ;;
    --list)        LIST_MODE="$2"; shift 2 ;;
    --help|-h)
      sed -n '2,35p' "${BASH_SOURCE[0]}" | sed 's/^# \?//'
      exit 0
      ;;
    *) echo "[restore] ERROR: Unknown flag: $1" >&2; exit 1 ;;
  esac
done

TIMESTAMP="$(date -u +%Y-%m-%dT%H-%M-%SZ)"
LOG_DIR="${BACKUP_ROOT}/logs"
LOG_FILE="${LOG_DIR}/restore-${TIMESTAMP}.log"
mkdir -p "${LOG_DIR}"

log()  { printf '[restore] %s %s\n' "$(date -u +%H:%M:%SZ)" "$*" | tee -a "${LOG_FILE}"; }
warn() { printf '[restore] WARN %s %s\n' "$(date -u +%H:%M:%SZ)" "$*" | tee -a "${LOG_FILE}" >&2; }
die()  { printf '[restore] FATAL %s %s\n' "$(date -u +%H:%M:%SZ)" "$*" | tee -a "${LOG_FILE}" >&2; exit 1; }

if [[ -f "${ENV_FILE}" ]]; then
  set -a; source "${ENV_FILE}"; set +a
fi

# ---------------------------------------------------------------------------
# --list mode: enumerate available backup IDs for a component
# ---------------------------------------------------------------------------
if [[ -n "${LIST_MODE}" ]]; then
  case "${LIST_MODE}" in
    pg|postgres) ls -1t "${BACKUP_ROOT}/pg/base/" 2>/dev/null || true ;;
    clickhouse)  ls -1t "${BACKUP_ROOT}/clickhouse/" 2>/dev/null | grep -E '^personel-' || true ;;
    vault)       ls -1t "${BACKUP_ROOT}/vault/" 2>/dev/null | grep -E '^snapshot-' || true ;;
    keycloak)    ls -1t "${BACKUP_ROOT}/keycloak/" 2>/dev/null || true ;;
    *) die "Unknown --list value: ${LIST_MODE}" ;;
  esac
  exit 0
fi

# ---------------------------------------------------------------------------
# Validate args
# ---------------------------------------------------------------------------
[[ -n "${COMPONENT}" ]] || die "Missing --component (pg|clickhouse|vault|keycloak)"
[[ -n "${BACKUP_ID}" ]] || die "Missing --backup-id (use --list ${COMPONENT} to enumerate)"

# ---------------------------------------------------------------------------
# Service-up check — REFUSE to restore over a live container
# ---------------------------------------------------------------------------
service_running() {
  local svc="$1"
  docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" ps "${svc}" \
    --format json 2>/dev/null | grep -q '"State":"running"'
}

require_stopped() {
  local svc="$1"
  if service_running "${svc}"; then
    die "${svc} container is RUNNING. Stop it first: docker compose stop ${svc}"
  fi
  log "Confirmed ${svc} container is stopped"
}

# ---------------------------------------------------------------------------
# Component: Postgres
# Restores the pg_basebackup tar into the postgres data volume, then
# (optionally) replays WAL up to PITR_TARGET.
# ---------------------------------------------------------------------------
restore_postgres() {
  local base_dir="${BACKUP_ROOT}/pg/base/${BACKUP_ID}"
  [[ -d "${base_dir}" ]] || die "Base backup not found: ${base_dir}"

  require_stopped postgres

  log "=== Restoring Postgres from ${BACKUP_ID} ==="

  # Find the data tar inside the base directory
  local tar_file
  tar_file=$(find "${base_dir}" -name "base.tar.gz" -type f | head -1)
  [[ -f "${tar_file}" ]] || die "base.tar.gz not found under ${base_dir}"
  log "  Source tar: ${tar_file}"

  # Wipe the data volume contents (the container is already stopped)
  local data_dir="${POSTGRES_DATA_DIR:-/var/lib/personel/postgres/data}"
  log "  Wiping ${data_dir} (operator must have backed up the existing data)"
  read -p "  Type 'YES' to confirm wipe: " confirm
  [[ "${confirm}" == "YES" ]] || die "Aborted by operator"

  rm -rf "${data_dir:?}"/*
  rm -rf "${data_dir:?}"/.[!.]* 2>/dev/null || true

  # Extract base tar
  tar -C "${data_dir}" -xzf "${tar_file}" 2>>"${LOG_FILE}" || die "tar extract failed"
  log "  Base extracted"

  # Replay WAL if PITR target requested
  if [[ -n "${PITR_TARGET}" ]]; then
    log "  PITR target: ${PITR_TARGET}"
    cat > "${data_dir}/recovery.signal" <<EOF
# Personel restore — PITR mode
EOF
    cat >> "${data_dir}/postgresql.auto.conf" <<EOF
restore_command = 'cp ${BACKUP_ROOT}/pg/wal/%f %p'
recovery_target_time = '${PITR_TARGET}'
recovery_target_action = 'pause'
EOF
    log "  recovery.signal + restore_command written; postgres will pause at target on first start"
  else
    log "  No PITR target — restoring base only"
  fi

  # Set ownership for postgres uid (999 in personel/postgres image)
  chown -R 999:999 "${data_dir}" 2>>"${LOG_FILE}" || warn "chown failed (run as root?)"

  log "  Restore staged. Operator must now: docker compose start postgres"
  log "  Then verify: docker compose logs -f postgres | grep 'ready to accept'"
}

# ---------------------------------------------------------------------------
# Component: ClickHouse
# Uses the RESTORE DATABASE command. Requires the BACKUP zip to be on the
# Disk('backups') target.
# ---------------------------------------------------------------------------
restore_clickhouse() {
  local backup_file="${BACKUP_ROOT}/clickhouse/personel-${BACKUP_ID}.zip"
  [[ -f "${backup_file}" ]] || die "ClickHouse backup not found: ${backup_file}"

  if service_running clickhouse; then
    warn "ClickHouse is running. RESTORE DATABASE supports replacing in place;"
    warn "however operator should still confirm to proceed."
    read -p "Type 'YES' to RESTORE INTO LIVE CLICKHOUSE: " confirm
    [[ "${confirm}" == "YES" ]] || die "Aborted by operator"
  fi

  log "=== Restoring ClickHouse from ${BACKUP_ID} ==="
  docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" exec -T clickhouse \
    clickhouse-client \
      --user="${CLICKHOUSE_USER:-personel_app}" \
      --password="${CLICKHOUSE_PASSWORD:-}" \
      --query="RESTORE DATABASE \`${CLICKHOUSE_DB:-personel}\` FROM Disk('backups', 'personel-${BACKUP_ID}.zip')" \
    2>>"${LOG_FILE}" || die "RESTORE DATABASE failed — see ${LOG_FILE}"

  log "  Restored. Verify with: clickhouse-client -q 'SHOW TABLES FROM ${CLICKHOUSE_DB:-personel}'"
}

# ---------------------------------------------------------------------------
# Component: Vault raft snapshot
# DESTRUCTIVE on the vault data volume.
# ---------------------------------------------------------------------------
restore_vault() {
  local snap="${BACKUP_ROOT}/vault/snapshot-${BACKUP_ID}.snap"
  [[ -f "${snap}" ]] || die "Vault snapshot not found: ${snap}"

  log "=== Restoring Vault from ${BACKUP_ID} ==="
  log "  WARNING: This rolls back ALL Vault state (PKI, KV, transit, AppRole)."
  read -p "  Type 'YES' to confirm Vault restore: " confirm
  [[ "${confirm}" == "YES" ]] || die "Aborted by operator"

  if [[ -z "${VAULT_TOKEN:-}" ]]; then
    die "VAULT_TOKEN must be set (root token from initial unseal ceremony)"
  fi

  # Vault must be UP and unsealed for raft restore (it's an API call)
  service_running vault || die "Vault must be running and unsealed"

  # Copy snapshot into container
  docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" cp \
    "${snap}" vault:/tmp/restore.snap 2>>"${LOG_FILE}" || die "cp into vault failed"

  docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" exec -T vault \
    sh -c "VAULT_TOKEN=${VAULT_TOKEN} vault operator raft snapshot restore -force /tmp/restore.snap" \
    2>>"${LOG_FILE}" || die "raft snapshot restore failed"

  docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" exec -T vault \
    rm -f /tmp/restore.snap 2>/dev/null || true

  log "  Vault restored. May need to re-unseal if force restore reset Shamir state."
}

# ---------------------------------------------------------------------------
# Component: Keycloak realm
# ---------------------------------------------------------------------------
restore_keycloak() {
  local kc_dir="${BACKUP_ROOT}/keycloak/${BACKUP_ID}"
  [[ -d "${kc_dir}" ]] || die "Keycloak export not found: ${kc_dir}"

  require_stopped keycloak

  log "=== Restoring Keycloak realm from ${BACKUP_ID} ==="
  log "  WARNING: existing realm will be replaced on next keycloak start"

  # Drop the export into the import directory used by --import-realm
  local target="${COMPOSE_DIR}/keycloak/realm-personel.json"
  local source
  source=$(find "${kc_dir}" -name "realm-personel*.json" -type f | head -1)
  [[ -f "${source}" ]] || die "realm-personel*.json not found in ${kc_dir}"

  cp "${source}" "${target}.restored-${TIMESTAMP}" 2>>"${LOG_FILE}"
  log "  Restored realm staged at: ${target}.restored-${TIMESTAMP}"
  log "  Operator must review then: mv ${target}.restored-${TIMESTAMP} ${target}"
  log "  Then: docker compose start keycloak  (with --import-realm in command)"
}

# ---------------------------------------------------------------------------
# Dispatch
# ---------------------------------------------------------------------------
log "===== Restore orchestrator: component=${COMPONENT} backup_id=${BACKUP_ID} ====="
case "${COMPONENT}" in
  pg|postgres) restore_postgres ;;
  clickhouse)  restore_clickhouse ;;
  vault)       restore_vault ;;
  keycloak)    restore_keycloak ;;
  minio)
    log "MinIO has no point-in-time restore — versioning is the recovery mechanism."
    log "Use: mc cp --version-id <version> personel-backup/<bucket>/<key> <dest>"
    exit 0
    ;;
  *) die "Unknown --component: ${COMPONENT}" ;;
esac

log "===== Restore complete ====="
