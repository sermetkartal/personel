#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Postgres Streaming Replication Validator
# Roadmap #43 — Faz 5 Wave 2
# =============================================================================
# End-to-end replication sanity check:
#   1. Connect to primary (vm3), create a marker table if missing
#   2. Insert a uniquely-tagged row
#   3. Connect to replica (vm5), wait up to 5 s for the row to appear
#   4. Compute lag (bytes + seconds) from pg_stat_replication on the primary
#      and pg_last_wal_replay_lsn on the replica
#   5. Exit 0 on success, 1 on any failure or threshold violation
#
# Thresholds:
#   MAX_LAG_BYTES=10485760    # 10 MB
#   MAX_LAG_SECONDS=30
#   MAX_APPLY_WAIT=5          # how long we wait for the marker row to land
#
# Usage:
#   sudo ./postgres-replication-test.sh
#   sudo ./postgres-replication-test.sh --quiet   # summary only
#   sudo ./postgres-replication-test.sh --cleanup # drop the marker table
#
# Env vars:
#   PRIMARY_CONTAINER  (default: personel-postgres)
#   PRIMARY_DB_USER    (default: postgres)
#   PRIMARY_DB_NAME    (default: personel)
#   REPLICA_HOST       (default: 192.168.5.32)
#   REPLICA_PORT       (default: 5432)
#   REPLICA_DB_USER    (default: postgres)
#   REPLICA_DB_NAME    (default: personel)
#   REPLICA_PW_FILE    (default: /etc/personel/secrets/postgres-replica-admin-password)
#                      The replica runs with the same POSTGRES_PASSWORD as the
#                      primary (inherited via pg_basebackup) — this file holds
#                      the primary superuser password for psql on the replica
#                      endpoint. If missing, the test falls back to asking
#                      the primary container to psql via its Unix socket and
#                      skips the direct replica query (lag-only mode).
#
# =============================================================================
set -euo pipefail
IFS=$'\n\t'

PRIMARY_CONTAINER="${PRIMARY_CONTAINER:-personel-postgres}"
PRIMARY_DB_USER="${PRIMARY_DB_USER:-postgres}"
PRIMARY_DB_NAME="${PRIMARY_DB_NAME:-personel}"
REPLICA_HOST="${REPLICA_HOST:-192.168.5.32}"
REPLICA_PORT="${REPLICA_PORT:-5432}"
REPLICA_DB_USER="${REPLICA_DB_USER:-postgres}"
REPLICA_DB_NAME="${REPLICA_DB_NAME:-personel}"
REPLICA_PW_FILE="${REPLICA_PW_FILE:-/etc/personel/secrets/postgres-replica-admin-password}"
TLS_ROOT_CA="${TLS_ROOT_CA:-/etc/personel/tls/root_ca.crt}"

MAX_LAG_BYTES="${MAX_LAG_BYTES:-10485760}"
MAX_LAG_SECONDS="${MAX_LAG_SECONDS:-30}"
MAX_APPLY_WAIT="${MAX_APPLY_WAIT:-5}"

QUIET=false
CLEANUP=false

log()  { [[ "${QUIET}" == "true" ]] || printf '[replica-test] %s\n' "$*"; }
warn() { printf '[replica-test] WARN: %s\n' "$*" >&2; }
err()  { printf '[replica-test] ERROR: %s\n' "$*" >&2; }
die()  { err "$*"; exit 1; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --quiet)   QUIET=true; shift ;;
    --cleanup) CLEANUP=true; shift ;;
    -h|--help) sed -n '2,45p' "$0"; exit 0 ;;
    *) die "unknown flag: $1" ;;
  esac
done

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
primary_psql() {
  docker exec -i "${PRIMARY_CONTAINER}" \
    psql -U "${PRIMARY_DB_USER}" -d "${PRIMARY_DB_NAME}" \
    -v ON_ERROR_STOP=1 -tA "$@"
}

replica_psql() {
  local pw=""
  if [[ -s "${REPLICA_PW_FILE}" ]]; then
    pw="$(tr -d '\n' < "${REPLICA_PW_FILE}")"
  fi
  if [[ -z "${pw}" ]]; then
    return 1
  fi
  PGPASSWORD="${pw}" psql \
    "host=${REPLICA_HOST} port=${REPLICA_PORT} dbname=${REPLICA_DB_NAME} user=${REPLICA_DB_USER} sslmode=verify-full sslrootcert=${TLS_ROOT_CA}" \
    -v ON_ERROR_STOP=1 -tA "$@"
}

# ---------------------------------------------------------------------------
# Pre-flight
# ---------------------------------------------------------------------------
docker inspect -f '{{.State.Running}}' "${PRIMARY_CONTAINER}" 2>/dev/null | grep -qx true \
  || die "primary container ${PRIMARY_CONTAINER} not running"

primary_psql -c "SELECT 1" >/dev/null \
  || die "primary psql connect failed"

# ---------------------------------------------------------------------------
# Cleanup path
# ---------------------------------------------------------------------------
if [[ "${CLEANUP}" == "true" ]]; then
  log "dropping marker table personel_replica_heartbeat"
  primary_psql -c "DROP TABLE IF EXISTS personel_replica_heartbeat;" >/dev/null
  log "cleanup done"
  exit 0
fi

# ---------------------------------------------------------------------------
# 1. Ensure marker table exists on primary
# ---------------------------------------------------------------------------
log "ensuring marker table exists on primary"
primary_psql <<'SQL' >/dev/null
CREATE TABLE IF NOT EXISTS personel_replica_heartbeat (
  id          bigserial PRIMARY KEY,
  token       text        NOT NULL UNIQUE,
  written_at  timestamptz NOT NULL DEFAULT now()
);
SQL

# ---------------------------------------------------------------------------
# 2. Insert unique marker row
# ---------------------------------------------------------------------------
TOKEN="test-$(date -u +%Y%m%dT%H%M%SZ)-$$-${RANDOM}"
log "inserting marker token ${TOKEN}"
WRITTEN_AT="$(primary_psql -c "INSERT INTO personel_replica_heartbeat(token) VALUES ('${TOKEN}') RETURNING written_at")"
[[ -n "${WRITTEN_AT}" ]] || die "insert returned empty written_at"
log "primary wrote at ${WRITTEN_AT}"

# ---------------------------------------------------------------------------
# 3. Poll replica for the row
# ---------------------------------------------------------------------------
REPLICA_OK=false
REPLICA_MODE="full"

replica_recovery=""
if ! replica_recovery="$(replica_psql -c "SELECT pg_is_in_recovery()" 2>/dev/null)"; then
  warn "replica direct query failed — falling back to lag-only mode (pg_stat_replication on primary)"
  REPLICA_MODE="lag-only"
fi

if [[ "${REPLICA_MODE}" == "full" ]]; then
  [[ "${replica_recovery}" == "t" ]] \
    || die "replica is NOT in recovery (pg_is_in_recovery=${replica_recovery}) — promoted or broken"
  log "replica pg_is_in_recovery() = t"

  deadline=$(( $(date +%s) + MAX_APPLY_WAIT ))
  while (( $(date +%s) < deadline )); do
    if replica_psql -c "SELECT 1 FROM personel_replica_heartbeat WHERE token='${TOKEN}'" 2>/dev/null | grep -qx 1; then
      REPLICA_OK=true
      break
    fi
    sleep 1
  done

  if [[ "${REPLICA_OK}" != "true" ]]; then
    err "marker row ${TOKEN} did NOT appear on replica within ${MAX_APPLY_WAIT}s"
    exit 1
  fi
  log "marker row visible on replica within budget"
fi

# ---------------------------------------------------------------------------
# 4. Compute lag from primary's pg_stat_replication view
# ---------------------------------------------------------------------------
log "computing lag from pg_stat_replication"
STAT="$(primary_psql <<'SQL'
SELECT
  coalesce(application_name, '-'),
  client_addr,
  state,
  sync_state,
  pg_wal_lsn_diff(pg_current_wal_lsn(), flush_lsn) AS flush_bytes,
  pg_wal_lsn_diff(pg_current_wal_lsn(), replay_lsn) AS replay_bytes,
  coalesce(extract(epoch from replay_lag), 0)::int AS replay_seconds
FROM pg_stat_replication
LIMIT 1;
SQL
)"

if [[ -z "${STAT}" ]]; then
  err "no row in pg_stat_replication — primary does not see any connected replica"
  exit 1
fi

IFS='|' read -r APP_NAME CLIENT_ADDR STATE SYNC_STATE FLUSH_BYTES REPLAY_BYTES REPLAY_SECONDS <<<"${STAT}"

log "replica app=${APP_NAME} addr=${CLIENT_ADDR} state=${STATE} sync=${SYNC_STATE}"
log "flush_lag=${FLUSH_BYTES} bytes  replay_lag=${REPLAY_BYTES} bytes  replay_seconds=${REPLAY_SECONDS}"

# ---------------------------------------------------------------------------
# 5. Threshold checks
# ---------------------------------------------------------------------------
fail=false

if [[ "${STATE}" != "streaming" ]]; then
  err "replica state is '${STATE}' not 'streaming'"
  fail=true
fi

if (( REPLAY_BYTES > MAX_LAG_BYTES )); then
  err "replay lag ${REPLAY_BYTES} bytes exceeds MAX_LAG_BYTES ${MAX_LAG_BYTES}"
  fail=true
fi

if (( REPLAY_SECONDS > MAX_LAG_SECONDS )); then
  err "replay lag ${REPLAY_SECONDS} s exceeds MAX_LAG_SECONDS ${MAX_LAG_SECONDS}"
  fail=true
fi

if [[ "${fail}" == "true" ]]; then
  exit 1
fi

# ---------------------------------------------------------------------------
# 6. Summary
# ---------------------------------------------------------------------------
cat <<EOF
-----------------------------------------------------------------------------
 Replication OK
-----------------------------------------------------------------------------
 Replica addr     : ${CLIENT_ADDR}
 Replica app      : ${APP_NAME}
 State            : ${STATE} / ${SYNC_STATE}
 Flush lag        : ${FLUSH_BYTES} bytes
 Replay lag       : ${REPLAY_BYTES} bytes
 Replay seconds   : ${REPLAY_SECONDS} s
 Mode             : ${REPLICA_MODE}
 Marker token     : ${TOKEN}
 Thresholds       : max_bytes=${MAX_LAG_BYTES}  max_seconds=${MAX_LAG_SECONDS}
-----------------------------------------------------------------------------
EOF

exit 0
