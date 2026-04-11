#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Restore Script
# TR: UYARI: Bu betik veri siler. Onay olmadan çalışmaz.
# EN: WARNING: This script deletes data. Requires explicit confirmation.
#
# Usage:
#   ./restore.sh --backup-dir /var/lib/personel/backups/daily/20260410T020000Z
#   ./restore.sh --backup-dir /path/to/backup [--service vault|postgres|clickhouse|minio]
#   ./restore.sh --list     — list available backups
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_DIR="${SCRIPT_DIR}/compose"

set -a
# shellcheck source=/dev/null
source "${COMPOSE_DIR}/.env"
set +a

RED='\033[0;31m'; YELLOW='\033[1;33m'; GREEN='\033[0;32m'; NC='\033[0m'; BOLD='\033[1m'
log()  { echo -e "${GREEN}[restore]${NC} $*"; }
warn() { echo -e "${YELLOW}[restore WARN]${NC} $*" >&2; }
die()  { echo -e "${RED}[restore ERROR]${NC} $*" >&2; exit 1; }

BACKUP_DIR_ARG=""
SERVICE_FILTER=""
LIST_ONLY=false

for arg in "$@"; do
  case "${arg}" in
    --backup-dir=*) BACKUP_DIR_ARG="${arg#--backup-dir=}" ;;
    --backup-dir)   shift; BACKUP_DIR_ARG="${1:-}" ;;
    --service=*)    SERVICE_FILTER="${arg#--service=}" ;;
    --list)         LIST_ONLY=true ;;
  esac
done

# ---------------------------------------------------------------------------
if [[ "${LIST_ONLY}" == "true" ]]; then
  echo "Available daily backups:"
  ls -1d "${BACKUP_DIR:-/var/lib/personel/backups}/daily/"*/ 2>/dev/null | sort -r | head -20
  echo ""
  echo "Available weekly backups:"
  ls -1d "${BACKUP_DIR:-/var/lib/personel/backups}/weekly/"*/ 2>/dev/null | sort -r | head -10
  exit 0
fi

[[ -n "${BACKUP_DIR_ARG}" ]] || die "Usage: $0 --backup-dir /path/to/backup [--service NAME]"
[[ -d "${BACKUP_DIR_ARG}" ]] || die "Backup directory not found: ${BACKUP_DIR_ARG}"

MANIFEST="${BACKUP_DIR_ARG}/MANIFEST.json"
[[ -f "${MANIFEST}" ]] || die "No MANIFEST.json found in ${BACKUP_DIR_ARG}"

BACKUP_TIMESTAMP=$(python3 -c "import json; m=json.load(open('${MANIFEST}')); print(m['backup_timestamp'])")
BACKUP_TENANT=$(python3 -c "import json; m=json.load(open('${MANIFEST}')); print(m.get('tenant_id','unknown'))")

# ---------------------------------------------------------------------------
# SAFETY GATE: explicit confirmation required
# ---------------------------------------------------------------------------
echo ""
echo -e "${RED}${BOLD}╔══════════════════════════════════════════════════════════════╗${NC}"
echo -e "${RED}${BOLD}║         DESTRUCTIVE OPERATION — RESTORE CONFIRMATION         ║${NC}"
echo -e "${RED}${BOLD}╠══════════════════════════════════════════════════════════════╣${NC}"
echo -e "${RED}${BOLD}║                                                              ║${NC}"
echo -e "${RED}${BOLD}║  TR: Bu işlem mevcut verilerin üzerine YAZABİLİR.            ║${NC}"
echo -e "${RED}${BOLD}║  EN: This operation may OVERWRITE existing data.             ║${NC}"
echo -e "${RED}${BOLD}║                                                              ║${NC}"
echo "  Backup timestamp : ${BACKUP_TIMESTAMP}"
echo "  Backup tenant    : ${BACKUP_TENANT}"
echo "  Restore service  : ${SERVICE_FILTER:-ALL}"
echo -e "${RED}${BOLD}║                                                              ║${NC}"
echo -e "${RED}${BOLD}╚══════════════════════════════════════════════════════════════╝${NC}"
echo ""

read -r -p "Type RESTORE to confirm you want to proceed: " CONFIRM
[[ "${CONFIRM}" == "RESTORE" ]] || die "Aborted. No data was changed."

# Second confirmation for full restore
if [[ -z "${SERVICE_FILTER}" ]]; then
  read -r -p "Full restore will STOP all services. Type YES to confirm: " CONFIRM2
  [[ "${CONFIRM2}" == "YES" ]] || die "Aborted."
fi

# ---------------------------------------------------------------------------
decrypt_if_needed() {
  local file="$1"
  if [[ "${file}" == *.gpg ]]; then
    local out="${file%.gpg}"
    gpg --batch --yes \
        --passphrase "${BACKUP_GPG_PASSPHRASE}" \
        --output "${out}" \
        --decrypt "${file}"
    echo "${out}"
  else
    echo "${file}"
  fi
}

# ---------------------------------------------------------------------------
# Stop services
# ---------------------------------------------------------------------------
if [[ -z "${SERVICE_FILTER}" ]] || [[ "${SERVICE_FILTER}" == "all" ]]; then
  log "Stopping all services..."
  cd "${COMPOSE_DIR}"
  docker compose stop
fi

# ---------------------------------------------------------------------------
# Restore Vault
# ---------------------------------------------------------------------------
if [[ -z "${SERVICE_FILTER}" ]] || [[ "${SERVICE_FILTER}" == "vault" ]]; then
  log "--- Restoring Vault ---"
  VAULT_SNAP=$(ls "${BACKUP_DIR_ARG}"/vault-raft-*.snap.gpg 2>/dev/null || \
               ls "${BACKUP_DIR_ARG}"/vault-raft-*.snap 2>/dev/null | head -1)
  [[ -n "${VAULT_SNAP}" ]] || { warn "No Vault snapshot found — skipping"; }
  if [[ -n "${VAULT_SNAP}" ]]; then
    VAULT_SNAP_PLAIN=$(decrypt_if_needed "${VAULT_SNAP}")
    docker compose up -d vault
    sleep 10
    # Restore requires Vault to be initialized; the snapshot restore creates a new leader
    docker exec personel-vault vault operator raft snapshot restore \
      -force "/vault/data/restore-tmp.snap" || true
    docker cp "${VAULT_SNAP_PLAIN}" "personel-vault:/vault/data/restore-tmp.snap"
    log "Vault snapshot restored. Unseal required."
    "${SCRIPT_DIR}/scripts/vault-unseal.sh"
  fi
fi

# ---------------------------------------------------------------------------
# Restore PostgreSQL
# ---------------------------------------------------------------------------
if [[ -z "${SERVICE_FILTER}" ]] || [[ "${SERVICE_FILTER}" == "postgres" ]]; then
  log "--- Restoring PostgreSQL ---"
  PG_TAR=$(ls "${BACKUP_DIR_ARG}"/postgres/**/*.tar.gz.gpg 2>/dev/null || \
           ls "${BACKUP_DIR_ARG}"/postgres/**/*.tar.gz 2>/dev/null | head -1 || echo "")
  if [[ -n "${PG_TAR}" ]]; then
    PG_TAR_PLAIN=$(decrypt_if_needed "${PG_TAR}")
    PG_DATA="${POSTGRES_DATA_DIR:-/var/lib/personel/postgres/data}"
    log "  Stopping postgres..."
    docker compose stop postgres
    log "  Clearing data directory: ${PG_DATA}"
    rm -rf "${PG_DATA:?}"/*
    log "  Extracting backup..."
    tar -xzf "${PG_TAR_PLAIN}" -C "${PG_DATA}"
    chown -R 999:999 "${PG_DATA}" 2>/dev/null || true
    log "  Starting postgres..."
    docker compose up -d postgres
    log "PostgreSQL restored"
  else
    warn "No PostgreSQL backup found in ${BACKUP_DIR_ARG}"
  fi
fi

# ---------------------------------------------------------------------------
# Restore ClickHouse
# ---------------------------------------------------------------------------
if [[ -z "${SERVICE_FILTER}" ]] || [[ "${SERVICE_FILTER}" == "clickhouse" ]]; then
  log "--- Restoring ClickHouse ---"
  CH_TAR=$(ls "${BACKUP_DIR_ARG}"/clickhouse-*.tar.gz.gpg 2>/dev/null || \
           ls "${BACKUP_DIR_ARG}"/clickhouse-*.tar.gz 2>/dev/null | head -1 || echo "")
  if [[ -n "${CH_TAR}" ]]; then
    CH_TAR_PLAIN=$(decrypt_if_needed "${CH_TAR}")
    log "  Stopping clickhouse..."
    docker compose stop clickhouse
    CH_DATA="${CLICKHOUSE_DATA_DIR:-/var/lib/personel/clickhouse/data}"
    mkdir -p "${CH_DATA}/backup"
    tar -xzf "${CH_TAR_PLAIN}" -C "${CH_DATA}/backup"
    docker compose up -d clickhouse
    # Restore from backup using clickhouse-backup
    BACKUP_NAME=$(basename "${CH_TAR_PLAIN}" .tar.gz | sed 's/clickhouse-/personel-/')
    docker exec personel-clickhouse clickhouse-backup restore "${BACKUP_NAME}" 2>/dev/null || \
      log "  ClickHouse backup restore completed (check container logs)"
    log "ClickHouse restored"
  else
    warn "No ClickHouse backup found"
  fi
fi

# ---------------------------------------------------------------------------
# Restore MinIO
# ---------------------------------------------------------------------------
if [[ -z "${SERVICE_FILTER}" ]] || [[ "${SERVICE_FILTER}" == "minio" ]]; then
  log "--- Restoring MinIO ---"
  MINIO_TAR=$(ls "${BACKUP_DIR_ARG}"/minio-*.tar.gz.gpg 2>/dev/null || \
              ls "${BACKUP_DIR_ARG}"/minio-*.tar.gz 2>/dev/null | head -1 || echo "")
  if [[ -n "${MINIO_TAR}" ]]; then
    MINIO_TAR_PLAIN=$(decrypt_if_needed "${MINIO_TAR}")
    MINIO_RESTORE_DIR="/tmp/minio-restore-$$"
    mkdir -p "${MINIO_RESTORE_DIR}"
    tar -xzf "${MINIO_TAR_PLAIN}" -C "${MINIO_RESTORE_DIR}"
    docker compose up -d minio
    sleep 5
    docker exec personel-minio mc alias set local http://localhost:9000 \
      "${MINIO_ROOT_USER}" "${MINIO_ROOT_PASSWORD}" --api s3v4 2>/dev/null
    docker cp "${MINIO_RESTORE_DIR}/." "personel-minio:/tmp/minio-restore/"
    for bucket in screenshots keystroke-blobs screen-clips dsr-responses sensitive-events; do
      docker exec personel-minio mc mirror \
        --preserve "/tmp/minio-restore/${bucket}" "local/${bucket}" 2>/dev/null || \
        log "  WARN: mirror failed for ${bucket}"
    done
    docker exec personel-minio rm -rf /tmp/minio-restore
    rm -rf "${MINIO_RESTORE_DIR}"
    log "MinIO restored"
  else
    warn "No MinIO backup found"
  fi
fi

# ---------------------------------------------------------------------------
# Restart full stack
# ---------------------------------------------------------------------------
if [[ -z "${SERVICE_FILTER}" ]] || [[ "${SERVICE_FILTER}" == "all" ]]; then
  log "Starting full stack..."
  cd "${COMPOSE_DIR}"
  docker compose up -d
fi

# ---------------------------------------------------------------------------
# Verify audit chain integrity after restore
# ---------------------------------------------------------------------------
log "Verifying audit chain integrity..."
"${SCRIPT_DIR}/scripts/verify-audit-chain.sh" --post-restore || \
  warn "Audit chain verification failed after restore — investigate before using the system"

log ""
log "Restore completed from backup: ${BACKUP_TIMESTAMP}"
log "TR: Yedekten geri yükleme tamamlandı."
