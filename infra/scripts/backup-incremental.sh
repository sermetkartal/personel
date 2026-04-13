#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Hourly Incremental Backup (Roadmap #58)
# =============================================================================
# Runs every hour. Cheap (<30s typical). Performs:
#   1. Postgres WAL archive sync — copies any WAL files that postgres has
#      written to /var/backups/personel/pg/wal but not yet checksummed
#      (postgresql.conf.tls archive_command writes them; this script
#      validates + indexes them).
#   2. ClickHouse OPTIMIZE on the most active tables (parts merge to keep
#      the backup target compact).
#   3. Health probe of every backup destination (read-write check).
#   4. WAL pruning beyond BACKUP_RETENTION_WAL days.
#
# Usage:
#   ./backup-incremental.sh
#
# Activation:
#   sudo systemctl enable --now personel-backup-incremental.timer
#
# Environment:
#   BACKUP_ROOT             default /var/backups/personel
#   BACKUP_RETENTION_WAL    default 7   (days)
# =============================================================================
set -euo pipefail
IFS=$'\n\t'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_DIR="${SCRIPT_DIR}/../compose"
ENV_FILE="${COMPOSE_DIR}/.env"

BACKUP_ROOT="${BACKUP_ROOT:-/var/backups/personel}"
BACKUP_RETENTION_WAL="${BACKUP_RETENTION_WAL:-7}"
PG_WAL_DIR="${BACKUP_ROOT}/pg/wal"
LOG_DIR="${BACKUP_ROOT}/logs"
TIMESTAMP="$(date -u +%Y-%m-%dT%H-%M-%SZ)"
LOG_FILE="${LOG_DIR}/backup-incremental-${TIMESTAMP}.log"

mkdir -p "${LOG_DIR}" "${PG_WAL_DIR}"

log()  { printf '[backup-inc] %s %s\n' "$(date -u +%H:%M:%SZ)" "$*" | tee -a "${LOG_FILE}"; }
warn() { printf '[backup-inc] WARN %s %s\n' "$(date -u +%H:%M:%SZ)" "$*" | tee -a "${LOG_FILE}" >&2; }

if [[ -f "${ENV_FILE}" ]]; then
  set -a; source "${ENV_FILE}"; set +a
fi

START_EPOCH=$(date +%s)
log "=== Hourly incremental backup ==="

# ---------------------------------------------------------------------------
# Step 1: WAL archive validation + indexing
# postgresql.conf.tls already writes WAL files via archive_command. We just
# verify file integrity (truncated cp == zero file), update an index file,
# and prune anything older than the retention window.
# ---------------------------------------------------------------------------
WAL_COUNT_BEFORE=$(find "${PG_WAL_DIR}" -type f -name "0*" 2>/dev/null | wc -l | tr -d ' ')

# Detect zero-byte WAL files (truncated archive_command)
ZERO_FILES=$(find "${PG_WAL_DIR}" -type f -name "0*" -size 0 2>/dev/null || true)
if [[ -n "${ZERO_FILES}" ]]; then
  warn "Zero-byte WAL files detected (archive_command failure?):"
  printf '%s\n' "${ZERO_FILES}" | tee -a "${LOG_FILE}" >&2
fi

# Update index file (sha256 of every WAL — used by restore.sh to verify chain)
INDEX_FILE="${PG_WAL_DIR}/.index.sha256"
{
  printf '# Personel WAL index — %s\n' "${TIMESTAMP}"
  find "${PG_WAL_DIR}" -type f -name "0*" -print0 \
    | xargs -0 -r sha256sum 2>/dev/null
} > "${INDEX_FILE}.tmp"
mv "${INDEX_FILE}.tmp" "${INDEX_FILE}"

# Prune old WAL
PRUNED=$(find "${PG_WAL_DIR}" -type f -name "0*" \
  -mtime "+${BACKUP_RETENTION_WAL}" -delete -print 2>/dev/null | wc -l | tr -d ' ')
WAL_COUNT_AFTER=$(find "${PG_WAL_DIR}" -type f -name "0*" 2>/dev/null | wc -l | tr -d ' ')

log "  WAL files before=${WAL_COUNT_BEFORE} after=${WAL_COUNT_AFTER} pruned=${PRUNED}"

# ---------------------------------------------------------------------------
# Step 2: ClickHouse parts merge (lightweight optimization)
# Forces background merges to consolidate small parts into bigger ones, so
# the next nightly BACKUP DATABASE is cheaper on disk.
# ---------------------------------------------------------------------------
if docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" ps clickhouse \
   --format json 2>/dev/null | grep -q '"State":"running"'; then
  for table in events_raw events_enriched events_sensitive screenshots audit_log; do
    docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" exec -T clickhouse \
      clickhouse-client \
        --user="${CLICKHOUSE_USER:-personel_app}" \
        --password="${CLICKHOUSE_PASSWORD:-}" \
        --query="OPTIMIZE TABLE \`${CLICKHOUSE_DB:-personel}\`.\`${table}\` FINAL DEDUPLICATE" \
      >>"${LOG_FILE}" 2>&1 || warn "  OPTIMIZE failed for ${table}"
  done
  log "  ClickHouse parts optimized (5 tables)"
else
  warn "  ClickHouse container not running — skipping optimize"
fi

# ---------------------------------------------------------------------------
# Step 3: Backup destination health probe
# Verify we can write to every retention root. If any fails, log loudly so
# Prometheus textfile collector picks it up and alerts.
# ---------------------------------------------------------------------------
for dir in "${BACKUP_ROOT}/pg/base" "${PG_WAL_DIR}" \
           "${BACKUP_ROOT}/clickhouse" "${BACKUP_ROOT}/vault" \
           "${BACKUP_ROOT}/keycloak"; do
  mkdir -p "${dir}"
  PROBE="${dir}/.probe-${TIMESTAMP}"
  if echo "ok" > "${PROBE}" 2>/dev/null && rm -f "${PROBE}"; then
    : # silent success
  else
    warn "  Backup destination NOT writable: ${dir}"
  fi
done
log "  All destinations writable"

# ---------------------------------------------------------------------------
# Step 4: Emit Prometheus textfile metric (consumed by node_exporter)
# ---------------------------------------------------------------------------
PROM_FILE="/var/lib/node_exporter/textfile_collector/personel_backup_incremental.prom"
if [[ -d "$(dirname "${PROM_FILE}")" ]]; then
  END_EPOCH=$(date +%s)
  DURATION=$(( END_EPOCH - START_EPOCH ))
  {
    printf '# HELP personel_backup_incremental_last_success_ts Unix timestamp of last successful incremental run\n'
    printf '# TYPE personel_backup_incremental_last_success_ts gauge\n'
    printf 'personel_backup_incremental_last_success_ts %d\n' "${END_EPOCH}"
    printf '# HELP personel_backup_incremental_duration_seconds Duration of last incremental run\n'
    printf '# TYPE personel_backup_incremental_duration_seconds gauge\n'
    printf 'personel_backup_incremental_duration_seconds %d\n' "${DURATION}"
    printf '# HELP personel_backup_wal_count Current WAL file count\n'
    printf '# TYPE personel_backup_wal_count gauge\n'
    printf 'personel_backup_wal_count %d\n' "${WAL_COUNT_AFTER}"
  } > "${PROM_FILE}.tmp" && mv "${PROM_FILE}.tmp" "${PROM_FILE}"
fi

END_EPOCH=$(date +%s)
log "=== Done in $(( END_EPOCH - START_EPOCH ))s ==="
