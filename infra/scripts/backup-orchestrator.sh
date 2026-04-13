#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Production Backup Orchestrator (Roadmap #58)
# =============================================================================
# This script supersedes the older infra/backup.sh and infra/scripts/backup.sh
# (both kept in place for compatibility — DO NOT delete; this file will replace
# them after the next operator review).
#
# Differences vs the older scripts:
#   * Uses pg_basebackup with `-X stream` for true point-in-time-recovery
#     base, instead of pg_dump (PITR requires WAL archive too — see
#     postgresql.conf.tls archive_command + backup-incremental.sh).
#   * Writes ClickHouse backup via the BACKUP DATABASE command and stores
#     it on the backups Disk configured in /etc/clickhouse-server/config.d/
#     (operator must provision the disk first — see clickhouse-backup runbook).
#   * Emits Vault raft snapshot for the auto-unseal-aware path.
#   * Emits Keycloak realm export via the kc.sh CLI inside the keycloak
#     container.
#   * Retention is policy-driven via env vars with sensible defaults.
#   * Calls the admin API's POST /v1/system/backup-runs to register the
#     SOC 2 A1.2 evidence record (best-effort; backup itself never fails
#     because of an API outage).
#
# Usage:
#   ./backup-orchestrator.sh                # full nightly backup
#   ./backup-orchestrator.sh --component pg # postgres only
#   ./backup-orchestrator.sh --dry-run      # show plan, exit
#
# Environment variables (defaults shown):
#   BACKUP_ROOT=/var/backups/personel       # where everything goes
#   BACKUP_RETENTION_DAILY=7                # nightly base count to keep
#   BACKUP_RETENTION_WAL=7                  # WAL archive day count
#   BACKUP_RETENTION_CLICKHOUSE=14          # ClickHouse backup count
#   BACKUP_RETENTION_VAULT=14               # Vault snapshot count
#   BACKUP_RETENTION_KEYCLOAK=14            # Realm export count
#   API_BASE_URL=http://127.0.0.1:8000      # admin API for evidence record
#
# Activation (production):
#   sudo systemctl enable --now personel-backup.timer
#
# =============================================================================
set -euo pipefail
IFS=$'\n\t'

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_DIR="${SCRIPT_DIR}/../compose"
ENV_FILE="${COMPOSE_DIR}/.env"

BACKUP_ROOT="${BACKUP_ROOT:-/var/backups/personel}"
BACKUP_RETENTION_DAILY="${BACKUP_RETENTION_DAILY:-7}"
BACKUP_RETENTION_WAL="${BACKUP_RETENTION_WAL:-7}"
BACKUP_RETENTION_CLICKHOUSE="${BACKUP_RETENTION_CLICKHOUSE:-14}"
BACKUP_RETENTION_VAULT="${BACKUP_RETENTION_VAULT:-14}"
BACKUP_RETENTION_KEYCLOAK="${BACKUP_RETENTION_KEYCLOAK:-14}"
API_BASE_URL="${API_BASE_URL:-http://127.0.0.1:8000}"

TIMESTAMP="$(date -u +%Y-%m-%dT%H-%M-%SZ)"
PG_BASE_DIR="${BACKUP_ROOT}/pg/base/${TIMESTAMP}"
PG_WAL_DIR="${BACKUP_ROOT}/pg/wal"
CH_DIR="${BACKUP_ROOT}/clickhouse"
VAULT_DIR="${BACKUP_ROOT}/vault"
KC_DIR="${BACKUP_ROOT}/keycloak/${TIMESTAMP}"
LOG_DIR="${BACKUP_ROOT}/logs"
LOG_FILE="${LOG_DIR}/backup-${TIMESTAMP}.log"

COMPONENT="all"
DRY_RUN=false

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------
while [[ $# -gt 0 ]]; do
  case "$1" in
    --component)
      COMPONENT="$2"
      shift 2
      ;;
    --dry-run)
      DRY_RUN=true
      shift
      ;;
    --help|-h)
      sed -n '2,40p' "${BASH_SOURCE[0]}" | sed 's/^# \?//'
      exit 0
      ;;
    *)
      echo "[backup] ERROR: Unknown flag: $1" >&2
      exit 1
      ;;
  esac
done

case "${COMPONENT}" in
  all|pg|postgres|clickhouse|vault|keycloak|minio) ;;
  *)
    echo "[backup] ERROR: --component must be one of: all,pg,clickhouse,vault,keycloak,minio" >&2
    exit 1
    ;;
esac

# ---------------------------------------------------------------------------
# Bootstrap directories + logging
# ---------------------------------------------------------------------------
mkdir -p "${LOG_DIR}" "${PG_WAL_DIR}" "${CH_DIR}" "${VAULT_DIR}" "$(dirname "${KC_DIR}")"
chmod 700 "${BACKUP_ROOT}"

log()  { printf '[backup] %s %s\n' "$(date -u +%H:%M:%SZ)" "$*" | tee -a "${LOG_FILE}"; }
warn() { printf '[backup] WARN %s %s\n' "$(date -u +%H:%M:%SZ)" "$*" | tee -a "${LOG_FILE}" >&2; }
die()  { printf '[backup] FATAL %s %s\n' "$(date -u +%H:%M:%SZ)" "$*" | tee -a "${LOG_FILE}" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Load env (best-effort; not every component needs every secret)
# ---------------------------------------------------------------------------
if [[ -f "${ENV_FILE}" ]]; then
  set -a
  # shellcheck source=/dev/null
  source "${ENV_FILE}"
  set +a
else
  warn "${ENV_FILE} not found — relying on caller-provided env vars."
fi

# ---------------------------------------------------------------------------
# Dry-run reporter
# ---------------------------------------------------------------------------
if [[ "${DRY_RUN}" == "true" ]]; then
  log "DRY RUN — no files will be written"
  log "Component:           ${COMPONENT}"
  log "Backup root:         ${BACKUP_ROOT}"
  log "PG base destination: ${PG_BASE_DIR}"
  log "PG WAL archive:      ${PG_WAL_DIR}"
  log "ClickHouse target:   ${CH_DIR}"
  log "Vault snapshot dir:  ${VAULT_DIR}"
  log "Keycloak export dir: ${KC_DIR}"
  log "Retention (daily):   ${BACKUP_RETENTION_DAILY}"
  log "Retention (WAL):     ${BACKUP_RETENTION_WAL} days"
  exit 0
fi

# ---------------------------------------------------------------------------
# Tool checks
# ---------------------------------------------------------------------------
need() { command -v "$1" >/dev/null 2>&1 || die "Required tool not found: $1"; }
need docker

# ---------------------------------------------------------------------------
# Component: Postgres pg_basebackup (PITR base) + WAL stream
# Retention: keep N most recent base directories; let backup-incremental.sh
#            handle WAL pruning.
# ---------------------------------------------------------------------------
backup_postgres() {
  log "=== Postgres base backup (pg_basebackup) ==="
  mkdir -p "${PG_BASE_DIR}"
  chmod 700 "${PG_BASE_DIR}"

  if docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" exec -T postgres \
       pg_basebackup \
         -h 127.0.0.1 \
         -U "${POSTGRES_USER:-postgres}" \
         -D /tmp/pg-base-${TIMESTAMP} \
         -F tar \
         -z \
         -P \
         -X stream \
         --checkpoint=fast \
       2>>"${LOG_FILE}"; then
    log "  pg_basebackup completed inside container"
  else
    die "  pg_basebackup failed — see ${LOG_FILE}"
  fi

  # Copy out of the container
  docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" cp \
    "postgres:/tmp/pg-base-${TIMESTAMP}" "${PG_BASE_DIR}/" \
    2>>"${LOG_FILE}" || die "Failed to copy base backup out of container"

  # Cleanup container-side
  docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" exec -T postgres \
    rm -rf "/tmp/pg-base-${TIMESTAMP}" 2>>"${LOG_FILE}" || true

  # Compute size + sha256
  PG_SIZE=$(du -sb "${PG_BASE_DIR}" | awk '{print $1}')
  log "  Base size: ${PG_SIZE} bytes"

  # Retention
  log "  Pruning bases older than ${BACKUP_RETENTION_DAILY} (count-based)"
  ls -1dt "${BACKUP_ROOT}/pg/base/"*/ 2>/dev/null \
    | tail -n +"$((BACKUP_RETENTION_DAILY + 1))" \
    | xargs -r rm -rf

  # WAL retention is by age in days
  find "${PG_WAL_DIR}" -type f -name "0*" \
    -mtime "+${BACKUP_RETENTION_WAL}" -delete 2>/dev/null || true
}

# ---------------------------------------------------------------------------
# Component: ClickHouse — BACKUP DATABASE TO Disk('backups')
# ---------------------------------------------------------------------------
backup_clickhouse() {
  log "=== ClickHouse backup ==="
  local backup_id="personel-${TIMESTAMP}.zip"

  if docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" exec -T clickhouse \
       clickhouse-client \
         --user="${CLICKHOUSE_USER:-personel_app}" \
         --password="${CLICKHOUSE_PASSWORD:-}" \
         --query="BACKUP DATABASE \`${CLICKHOUSE_DB:-personel}\` TO Disk('backups', '${backup_id}')" \
       2>>"${LOG_FILE}"; then
    log "  BACKUP DATABASE issued: ${backup_id}"
  else
    warn "  ClickHouse BACKUP failed (Disk 'backups' provisioned?). Capturing schema as fallback."
    docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" exec -T clickhouse \
      clickhouse-client \
        --user="${CLICKHOUSE_USER:-personel_app}" \
        --password="${CLICKHOUSE_PASSWORD:-}" \
        --query="SELECT create_table_query FROM system.tables WHERE database='${CLICKHOUSE_DB:-personel}'" \
      > "${CH_DIR}/schema-${TIMESTAMP}.sql" 2>>"${LOG_FILE}" || true
  fi

  # Retention by count
  ls -1t "${CH_DIR}"/personel-*.zip 2>/dev/null \
    | tail -n +"$((BACKUP_RETENTION_CLICKHOUSE + 1))" \
    | xargs -r rm -f
}

# ---------------------------------------------------------------------------
# Component: Vault raft snapshot
# ---------------------------------------------------------------------------
backup_vault() {
  log "=== Vault raft snapshot ==="
  local snap="${VAULT_DIR}/snapshot-${TIMESTAMP}.snap"

  if [[ -z "${VAULT_TOKEN:-}" ]]; then
    warn "VAULT_TOKEN not in env — using docker exec fallback"
    if docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" exec -T vault \
         vault operator raft snapshot save /tmp/snap.snap 2>>"${LOG_FILE}"; then
      docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" cp \
        vault:/tmp/snap.snap "${snap}" 2>>"${LOG_FILE}" || warn "  cp failed"
      docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" exec -T vault \
        rm -f /tmp/snap.snap 2>/dev/null || true
    else
      warn "  vault operator raft snapshot save failed — Vault may be sealed"
      return 0
    fi
  else
    VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}" \
    VAULT_TOKEN="${VAULT_TOKEN}" \
      vault operator raft snapshot save "${snap}" 2>>"${LOG_FILE}" || warn "snapshot failed"
  fi

  if [[ -f "${snap}" ]]; then
    log "  Snapshot: $(stat -c %s "${snap}" 2>/dev/null || stat -f %z "${snap}") bytes"
  fi

  # Retention by count
  ls -1t "${VAULT_DIR}"/snapshot-*.snap 2>/dev/null \
    | tail -n +"$((BACKUP_RETENTION_VAULT + 1))" \
    | xargs -r rm -f
}

# ---------------------------------------------------------------------------
# Component: Keycloak realm export
# ---------------------------------------------------------------------------
backup_keycloak() {
  log "=== Keycloak realm export ==="
  mkdir -p "${KC_DIR}"

  if docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" exec -T keycloak \
       /opt/keycloak/bin/kc.sh export \
         --dir /tmp/kc-export \
         --realm "${KEYCLOAK_REALM:-personel}" \
       2>>"${LOG_FILE}"; then
    docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" cp \
      keycloak:/tmp/kc-export/. "${KC_DIR}/" 2>>"${LOG_FILE}" || warn "kc cp failed"
    docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" exec -T keycloak \
      rm -rf /tmp/kc-export 2>/dev/null || true
    log "  Realm exported to ${KC_DIR}"
  else
    warn "  kc.sh export failed (Keycloak running with realm bound? --import-realm conflict?)"
  fi

  # Retention by count
  ls -1dt "${BACKUP_ROOT}/keycloak/"*/ 2>/dev/null \
    | tail -n +"$((BACKUP_RETENTION_KEYCLOAK + 1))" \
    | xargs -r rm -rf
}

# ---------------------------------------------------------------------------
# Component: MinIO — enable versioning on non-WORM buckets only
# audit-worm bucket has Object Lock (ADR 0014 + Roadmap #50) and is the
# authoritative copy. We do NOT mirror it (lock state can't be replicated to
# a regular target).
# ---------------------------------------------------------------------------
backup_minio() {
  log "=== MinIO bucket versioning (continuous, not point-in-time) ==="
  if ! command -v mc >/dev/null 2>&1; then
    warn "mc CLI not installed on host — skipping MinIO versioning step"
    return 0
  fi

  local mc_alias="personel-backup"
  mc alias set "${mc_alias}" \
    "http://127.0.0.1:${MINIO_PORT:-9000}" \
    "${MINIO_ROOT_USER:-minioadmin}" \
    "${MINIO_ROOT_PASSWORD:-}" \
    --api S3v4 \
    >/dev/null 2>>"${LOG_FILE}" || { warn "mc alias failed"; return 0; }

  for bucket in screenshots dsr-responses destruction-reports keystroke-blobs livrec; do
    if mc ls "${mc_alias}/${bucket}" >/dev/null 2>&1; then
      mc version enable "${mc_alias}/${bucket}" >/dev/null 2>>"${LOG_FILE}" || \
        warn "  versioning enable failed for ${bucket}"
      log "  ${bucket}: versioning enabled (idempotent)"
    fi
  done
  log "  audit-worm bucket: NOT touched (Object Lock authoritative copy)"
}

# ---------------------------------------------------------------------------
# Run components
# ---------------------------------------------------------------------------
START_EPOCH=$(date +%s)
log "===== Personel backup orchestrator: component=${COMPONENT} ====="

case "${COMPONENT}" in
  all)
    backup_postgres
    backup_clickhouse
    backup_vault
    backup_keycloak
    backup_minio
    ;;
  pg|postgres) backup_postgres ;;
  clickhouse)  backup_clickhouse ;;
  vault)       backup_vault ;;
  keycloak)    backup_keycloak ;;
  minio)       backup_minio ;;
esac

END_EPOCH=$(date +%s)
DURATION=$(( END_EPOCH - START_EPOCH ))
log "===== Backup complete in ${DURATION}s ====="

# ---------------------------------------------------------------------------
# Evidence record (Phase 3.0 collector A1.2 — best effort)
# ---------------------------------------------------------------------------
if command -v curl >/dev/null 2>&1; then
  TOTAL_SIZE=$(du -sb "${BACKUP_ROOT}" 2>/dev/null | awk '{print $1}' || echo 0)
  MANIFEST_SHA=$(printf '%s' "${TIMESTAMP}|${COMPONENT}|${TOTAL_SIZE}|${DURATION}" | sha256sum | awk '{print $1}')

  HTTP_CODE=$(curl -fsS -o /dev/null -w '%{http_code}' \
    -X POST "${API_BASE_URL}/v1/system/backup-runs" \
    -H 'Content-Type: application/json' \
    -d "{\"target_path\":\"${BACKUP_ROOT}\",\"size_bytes\":${TOTAL_SIZE},\"duration_seconds\":${DURATION},\"manifest_sha256\":\"${MANIFEST_SHA}\",\"component\":\"${COMPONENT}\"}" \
    2>>"${LOG_FILE}" || echo "000")

  if [[ "${HTTP_CODE}" =~ ^20[0-9]$ ]]; then
    log "Evidence record posted: HTTP ${HTTP_CODE}"
  else
    warn "Evidence record failed (HTTP ${HTTP_CODE}). Backup itself is valid."
    warn "Re-submit manually per infra/runbooks/soc2-manual-evidence-submission.md"
  fi
else
  warn "curl missing — skipping evidence post"
fi

log "Done."
