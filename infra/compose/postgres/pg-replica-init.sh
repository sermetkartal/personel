#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Postgres Replica Init Wrapper (Roadmap #43)
# =============================================================================
# Runs as the container's ENTRYPOINT on vm5. Three phases:
#
#   1. First boot (empty PGDATA): pg_basebackup from primary, drop
#      standby.signal, write postgresql.auto.conf with primary_conninfo,
#      then exec postgres.
#   2. Subsequent boots (PGDATA already restored): exec postgres directly,
#      standby.signal is still there from phase 1 so it comes up as a
#      hot standby again.
#   3. Failure paths are LOUD and exit non-zero — the operator must
#      inspect before retrying.
#
# This script is idempotent by construction: the presence of PG_VERSION
# inside PGDATA is the flag that first-boot is already done.
#
# Required at runtime:
#   * Environment: PRIMARY_HOST, PRIMARY_PORT, REPLICATION_USER, PGSSLMODE,
#     PGSSLROOTCERT
#   * Secret:     /run/secrets/replicator_password
#   * Certs:      /etc/personel/tls/root_ca.crt (read-only mount)
#
# =============================================================================
set -euo pipefail
IFS=$'\n\t'

PGDATA="${PGDATA:-/var/lib/postgresql/data}"
PRIMARY_HOST="${PRIMARY_HOST:?PRIMARY_HOST must be set}"
PRIMARY_PORT="${PRIMARY_PORT:-5432}"
REPLICATION_USER="${REPLICATION_USER:-replicator}"
PWFILE="/run/secrets/replicator_password"
ROOT_CA="${PGSSLROOTCERT:-/etc/personel/tls/root_ca.crt}"

log() {
  printf '[pg-replica-init %s] %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*"
}

die() {
  log "FATAL: $*"
  exit 1
}

# ---------------------------------------------------------------------------
# Pre-flight
# ---------------------------------------------------------------------------
[[ -r "${ROOT_CA}" ]]        || die "root CA not readable at ${ROOT_CA}"
[[ -r "${PWFILE}" ]]         || die "replicator password secret missing at ${PWFILE}"
[[ -n "${PRIMARY_HOST}" ]]   || die "PRIMARY_HOST is empty"

# Export libpq credentials so pg_basebackup can authenticate without a
# prompt. These variables are intentionally scoped to this script — they
# are NOT passed to the eventual postgres server process.
export PGPASSWORD
PGPASSWORD="$(tr -d '\n' < "${PWFILE}")"
[[ -n "${PGPASSWORD}" ]]     || die "replicator password file is empty"

# ---------------------------------------------------------------------------
# Phase 1 — pg_basebackup on empty PGDATA
# ---------------------------------------------------------------------------
if [[ ! -s "${PGDATA}/PG_VERSION" ]]; then
  log "PGDATA is empty — starting pg_basebackup from ${PRIMARY_HOST}:${PRIMARY_PORT}"

  # PGDATA must be owned by postgres and be empty/near-empty. The bind-mount
  # from the host may leave a `lost+found` directory behind — pg_basebackup
  # tolerates that only if it is the single entry. We scrub everything to
  # be safe, then recreate.
  find "${PGDATA}" -mindepth 1 -maxdepth 1 -exec rm -rf {} + || true

  # --wal-method=stream opens a second replication connection that pipelines
  # WAL in parallel with the base file copy, so the resulting snapshot is
  # consistent without needing WAL archives.
  # -R writes standby.signal + primary_conninfo for us, but we overwrite
  # primary_conninfo ourselves afterwards to force sslmode=verify-full
  # regardless of what pg_basebackup inferred from the connect string.
  pg_basebackup \
    --host="${PRIMARY_HOST}" \
    --port="${PRIMARY_PORT}" \
    --username="${REPLICATION_USER}" \
    --pgdata="${PGDATA}" \
    --wal-method=stream \
    --write-recovery-conf \
    --checkpoint=fast \
    --progress \
    --verbose \
    || die "pg_basebackup failed — see stderr, data dir left in partial state"

  log "pg_basebackup complete"

  # Force verify-full regardless of what -R inferred. Append rather than
  # overwrite so pg_basebackup's own recovery lines (restore_command etc.)
  # stay intact if it added any.
  cat >> "${PGDATA}/postgresql.auto.conf" <<EOF

# --- Personel replication override (pg-replica-init.sh) ---
primary_conninfo = 'host=${PRIMARY_HOST} port=${PRIMARY_PORT} user=${REPLICATION_USER} sslmode=verify-full sslrootcert=${ROOT_CA} application_name=personel-replica-vm5'
# --- end Personel replication override ---
EOF

  # Ensure standby.signal exists (pg_basebackup -R should create it, but
  # be defensive — older postgres versions and custom builds sometimes skip)
  touch "${PGDATA}/standby.signal"

  # Tighten permissions — postgres refuses to start if PGDATA is group-readable
  chmod 0700 "${PGDATA}"

  log "standby.signal + primary_conninfo written — handoff to postgres"
else
  log "PGDATA already provisioned (PG_VERSION found) — skipping pg_basebackup"
  if [[ ! -f "${PGDATA}/standby.signal" ]]; then
    log "WARNING: PGDATA exists but standby.signal missing — replica may have been promoted; refusing to start as standby"
    die "promoted replica detected; operator must decide (resync or accept as primary)"
  fi
fi

# Scrub the password from the environment before exec'ing postgres so it
# never appears in /proc/<pid>/environ of the long-running process.
unset PGPASSWORD

# ---------------------------------------------------------------------------
# Phase 2 — exec postgres with the replica-tuned config
# ---------------------------------------------------------------------------
log "exec postgres -c config_file=/etc/postgresql/postgresql.conf"
exec postgres -c config_file=/etc/postgresql/postgresql.conf
