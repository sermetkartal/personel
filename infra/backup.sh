#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Backup Script
# TR: Tam şifreli yedekleme: Vault, Postgres, ClickHouse, MinIO, OpenSearch.
# EN: Full encrypted backup: Vault, Postgres, ClickHouse, MinIO, OpenSearch.
#
# Usage:
#   ./backup.sh                  — full backup (nightly)
#   ./backup.sh --vault-only     — Vault raft snapshot only
#   ./backup.sh --no-clickhouse  — skip ClickHouse (faster, smaller)
#
# Backup encryption:
#   Primary:  GPG symmetric with BACKUP_GPG_PASSPHRASE from .env
#   Optional: age asymmetric with BACKUP_AGE_RECIPIENT public key
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_DIR="${SCRIPT_DIR}/compose"

# Load environment
set -a
# shellcheck source=/dev/null
source "${COMPOSE_DIR}/.env"
set +a

BACKUP_DIR="${BACKUP_DIR:-/var/lib/personel/backups}"
TIMESTAMP=$(date -u +"%Y%m%dT%H%M%SZ")
BACKUP_PATH="${BACKUP_DIR}/daily/${TIMESTAMP}"
LOG_FILE="${BACKUP_DIR}/logs/backup-${TIMESTAMP}.log"

# Flags
VAULT_ONLY=false
SKIP_CLICKHOUSE=false
for arg in "$@"; do
  case "${arg}" in
    --vault-only)      VAULT_ONLY=true      ;;
    --no-clickhouse)   SKIP_CLICKHOUSE=true ;;
  esac
done

# ---------------------------------------------------------------------------
log() { echo "[$(date -u +%T)] [backup] $*" | tee -a "${LOG_FILE}"; }
die() { log "ERROR: $*"; exit 1; }

encrypt_file() {
  local input="$1"
  local output="${input}.gpg"
  if [[ -n "${BACKUP_GPG_PASSPHRASE:-}" ]]; then
    gpg --batch --yes --symmetric \
        --cipher-algo AES256 \
        --passphrase "${BACKUP_GPG_PASSPHRASE}" \
        --output "${output}" \
        "${input}"
    rm -f "${input}"
    echo "${output}"
  else
    log "WARNING: BACKUP_GPG_PASSPHRASE not set — backup is UNENCRYPTED"
    echo "${input}"
  fi
}

# ---------------------------------------------------------------------------
mkdir -p "${BACKUP_PATH}" "$(dirname "${LOG_FILE}")"
log "=========================================================="
log "Personel Platform Backup — ${TIMESTAMP}"
log "Destination: ${BACKUP_PATH}"
log "=========================================================="

# Verify Vault is unsealed before backup
VAULT_STATUS=$(docker exec personel-vault vault status -format=json 2>/dev/null || echo '{"sealed":true}')
if echo "${VAULT_STATUS}" | python3 -c "import json,sys; d=json.load(sys.stdin); sys.exit(0 if not d.get('sealed') else 1)" 2>/dev/null; then
  log "Vault status: unsealed"
else
  die "Vault is sealed. Unseal before running backup."
fi

# ---------------------------------------------------------------------------
# 1. Vault Raft Snapshot
# ---------------------------------------------------------------------------
log "--- Vault Raft Snapshot ---"
VAULT_SNAP="${BACKUP_PATH}/vault-raft-${TIMESTAMP}.snap"
docker exec personel-vault vault operator raft snapshot save \
  "/vault/data/vault-backup-tmp.snap" \
  || die "Vault snapshot failed"
docker cp "personel-vault:/vault/data/vault-backup-tmp.snap" "${VAULT_SNAP}"
docker exec personel-vault rm -f "/vault/data/vault-backup-tmp.snap"
VAULT_SNAP_ENC=$(encrypt_file "${VAULT_SNAP}")
log "Vault snapshot: $(basename "${VAULT_SNAP_ENC}") ($(du -sh "${VAULT_SNAP_ENC}" | cut -f1))"

if [[ "${VAULT_ONLY}" == "true" ]]; then
  log "Vault-only mode — done."
  exit 0
fi

# ---------------------------------------------------------------------------
# 2. PostgreSQL pg_basebackup
# ---------------------------------------------------------------------------
log "--- PostgreSQL Backup ---"
PG_BACKUP_DIR="${BACKUP_PATH}/postgres"
mkdir -p "${PG_BACKUP_DIR}"
docker exec -e PGPASSWORD="${POSTGRES_PASSWORD}" personel-postgres \
  pg_basebackup \
    -U postgres \
    -D /tmp/pg-backup-${TIMESTAMP} \
    --format=tar \
    --gzip \
    --compress=6 \
    --wal-method=stream \
    -P
docker cp "personel-postgres:/tmp/pg-backup-${TIMESTAMP}" "${PG_BACKUP_DIR}/"
docker exec personel-postgres rm -rf "/tmp/pg-backup-${TIMESTAMP}"

PG_TAR=$(ls "${PG_BACKUP_DIR}"/**/*.tar.gz 2>/dev/null | head -1 || \
         ls "${PG_BACKUP_DIR}"/*.tar.gz 2>/dev/null | head -1 || echo "")
if [[ -f "${PG_TAR}" ]]; then
  PG_ENC=$(encrypt_file "${PG_TAR}")
  log "PostgreSQL backup: $(basename "${PG_ENC}") ($(du -sh "${PG_ENC}" | cut -f1))"
else
  log "PostgreSQL backup files at: ${PG_BACKUP_DIR}"
fi

# ---------------------------------------------------------------------------
# 3. ClickHouse clickhouse-backup
# ---------------------------------------------------------------------------
if [[ "${SKIP_CLICKHOUSE}" != "true" ]]; then
  log "--- ClickHouse Backup ---"
  CH_BACKUP_NAME="personel-${TIMESTAMP}"

  # Create backup using clickhouse-backup tool (must be installed in the container)
  docker exec personel-clickhouse clickhouse-backup create "${CH_BACKUP_NAME}" \
    2>/dev/null || {
    # Fallback: use ClickHouse native backup
    docker exec personel-clickhouse bash -c "
      clickhouse-client \
        --user '${CLICKHOUSE_USER:-personel_app}' \
        --password '${CLICKHOUSE_PASSWORD}' \
        -q \"BACKUP DATABASE personel TO File('/var/lib/clickhouse/backups/${CH_BACKUP_NAME}')\"
    " 2>/dev/null || log "ClickHouse backup via native method"
  }

  CH_BACKUP_SRC="/var/lib/personel/clickhouse/data/backup/${CH_BACKUP_NAME}"
  CH_BACKUP_DST="${BACKUP_PATH}/clickhouse-${TIMESTAMP}.tar.gz"
  if [[ -d "${CH_BACKUP_SRC}" ]]; then
    tar -czf "${CH_BACKUP_DST}" -C "$(dirname "${CH_BACKUP_SRC}")" "$(basename "${CH_BACKUP_SRC}")"
    rm -rf "${CH_BACKUP_SRC}"
    CH_ENC=$(encrypt_file "${CH_BACKUP_DST}")
    log "ClickHouse backup: $(basename "${CH_ENC}") ($(du -sh "${CH_ENC}" | cut -f1))"
  else
    log "ClickHouse backup stored natively. Check /var/lib/clickhouse/backups/"
  fi
fi

# ---------------------------------------------------------------------------
# 4. MinIO mc mirror
# ---------------------------------------------------------------------------
log "--- MinIO Backup ---"
MINIO_BACKUP_DIR="${BACKUP_PATH}/minio"
mkdir -p "${MINIO_BACKUP_DIR}"
MC_ALIAS="personel-src"

# Configure mc alias within the minio container
docker exec personel-minio mc alias set "${MC_ALIAS}" \
  "http://localhost:9000" \
  "${MINIO_ROOT_USER}" \
  "${MINIO_ROOT_PASSWORD}" \
  --api s3v4 2>/dev/null

for bucket in screenshots keystroke-blobs screen-clips dsr-responses destruction-reports sensitive-events; do
  log "  Mirroring bucket: ${bucket}"
  docker exec personel-minio mc mirror \
    --preserve \
    "${MC_ALIAS}/${bucket}" \
    "/tmp/minio-backup/${bucket}" 2>/dev/null \
    || log "  WARN: mirror failed for ${bucket}"
done

docker cp "personel-minio:/tmp/minio-backup" "${MINIO_BACKUP_DIR}/"
docker exec personel-minio rm -rf /tmp/minio-backup

MINIO_TAR="${BACKUP_PATH}/minio-${TIMESTAMP}.tar.gz"
tar -czf "${MINIO_TAR}" -C "${MINIO_BACKUP_DIR}" .
rm -rf "${MINIO_BACKUP_DIR}"
MINIO_ENC=$(encrypt_file "${MINIO_TAR}")
log "MinIO backup: $(basename "${MINIO_ENC}") ($(du -sh "${MINIO_ENC}" | cut -f1))"

# ---------------------------------------------------------------------------
# 5. OpenSearch snapshot
# ---------------------------------------------------------------------------
log "--- OpenSearch Snapshot ---"
OS_SNAP_NAME="personel-backup-${TIMESTAMP}"
curl -sk -X PUT \
  "https://localhost:${OPENSEARCH_PORT:-9200}/_snapshot/personel-backup/${OS_SNAP_NAME}" \
  -H "Content-Type: application/json" \
  -u "admin:${OPENSEARCH_ADMIN_PASSWORD}" \
  -d '{"indices": "personel-*","ignore_unavailable": true,"include_global_state": false}' \
  >/dev/null 2>&1 || log "WARN: OpenSearch snapshot request failed (may not have snapshot repo configured)"

# ---------------------------------------------------------------------------
# 6. Manifest and checksum
# ---------------------------------------------------------------------------
log "--- Generating Manifest ---"
MANIFEST="${BACKUP_PATH}/MANIFEST.json"
python3 -c "
import json, os, hashlib, datetime

backup_path = '${BACKUP_PATH}'
files = {}
for fname in os.listdir(backup_path):
    fpath = os.path.join(backup_path, fname)
    if os.path.isfile(fpath):
        h = hashlib.sha256()
        with open(fpath, 'rb') as f:
            for chunk in iter(lambda: f.read(65536), b''):
                h.update(chunk)
        files[fname] = {
            'sha256': h.hexdigest(),
            'size_bytes': os.path.getsize(fpath)
        }

manifest = {
    'personel_version': '${PERSONEL_VERSION:-0.1.0}',
    'tenant_id': '${PERSONEL_TENANT_ID:-unknown}',
    'backup_timestamp': '${TIMESTAMP}',
    'encrypted': bool('${BACKUP_GPG_PASSPHRASE:-}'),
    'files': files
}
print(json.dumps(manifest, indent=2))
" > "${MANIFEST}"

log "Manifest: ${MANIFEST}"
log "Total backup size: $(du -sh "${BACKUP_PATH}" | cut -f1)"

# ---------------------------------------------------------------------------
# 7. Retain old backups
# ---------------------------------------------------------------------------
log "--- Pruning Old Backups ---"
KEEP_DAILY="${BACKUP_RETENTION_DAILY:-7}"
KEEP_WEEKLY="${BACKUP_RETENTION_WEEKLY:-4}"

# Keep last N daily backups
ls -1d "${BACKUP_DIR}/daily/"*/ 2>/dev/null | sort -r | tail -n "+$((KEEP_DAILY + 1))" | \
  xargs -r rm -rf && log "Pruned daily backups older than ${KEEP_DAILY} days"

# Weekly: copy today's backup if it's Sunday
if [[ "$(date +%u)" == "7" ]]; then
  WEEKLY_PATH="${BACKUP_DIR}/weekly/${TIMESTAMP}"
  cp -r "${BACKUP_PATH}" "${WEEKLY_PATH}"
  ls -1d "${BACKUP_DIR}/weekly/"*/ 2>/dev/null | sort -r | tail -n "+$((KEEP_WEEKLY + 1))" | \
    xargs -r rm -rf
  log "Weekly backup stored: ${WEEKLY_PATH}"
fi

# ---------------------------------------------------------------------------
log "=========================================================="
log "Backup complete. Timestamp: ${TIMESTAMP}"
log "Log: ${LOG_FILE}"
log "=========================================================="
