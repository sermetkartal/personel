#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Postgres Streaming Replication Bootstrap
# Roadmap #43 — Faz 5 Wave 2 (vm3 → vm5)
# =============================================================================
# Runs ON vm3 (the Postgres primary host). Prepares the primary for streaming
# replication and emits the operator-runnable handoff commands for vm5.
#
# What this script DOES:
#   1. Pre-flight: primary is up, vm5 is reachable, replication TLS cert is
#      issued, Vault PKI is reachable, /etc/personel/secrets exists
#   2. Generates a random 48-byte replicator password, writes it to
#      /etc/personel/secrets/postgres-replicator-password (mode 0600, root:root)
#   3. Creates the `replicator` role via psql over the Unix socket (or skips
#      if already present + idempotent)
#   4. Issues a postgres-replica server cert from Vault PKI with
#      CN=postgres-replica.personel.internal + SAN IP 192.168.5.32 and stages
#      the artifacts under /tmp/personel-replica-bootstrap.<ts>/
#   5. Prints the exact scp + docker compose commands the operator must run
#      on vm5 (this script NEVER touches vm5 directly — no outbound SSH here)
#
# What this script DOES NOT do:
#   * Deploy anything to vm5 (operator runs the printed commands manually
#     after reviewing the artifacts)
#   * Restart the primary container (operator flips the compose overlay by
#     hand — see docs/operations/postgres-replication.md)
#   * Drop the replicator role (use --teardown for that, gated behind a
#     confirmation prompt)
#
# Usage:
#   sudo VAULT_TOKEN=hvs.xxxx ./postgres-replica-bootstrap.sh
#   sudo VAULT_TOKEN=hvs.xxxx ./postgres-replica-bootstrap.sh --dry-run
#   sudo VAULT_TOKEN=hvs.xxxx ./postgres-replica-bootstrap.sh --force-password
#   sudo                          ./postgres-replica-bootstrap.sh --teardown
#
# Environment variables:
#   VAULT_TOKEN      (required unless --teardown) Vault token with
#                    pki/issue/server-cert capability
#   VAULT_ADDR       (optional) default https://127.0.0.1:8200
#   PKI_MOUNT        (optional) default pki
#   PKI_ROLE         (optional) default server-cert
#   PRIMARY_CONTAINER (optional) default personel-postgres
#   PRIMARY_DB_USER  (optional) default postgres
#   PRIMARY_DB_NAME  (optional) default personel
#   REPLICA_IP       (optional) default 192.168.5.32
#   REPLICA_CN       (optional) default postgres-replica.personel.internal
#   SECRETS_DIR      (optional) default /etc/personel/secrets
#   STAGE_DIR        (optional) default /tmp/personel-replica-bootstrap.<ts>
#
# Exit codes:
#   0 — primary ready, handoff printed
#   1 — usage / pre-flight error
#   2 — role creation or cert issuance failed (partial state — inspect log)
# =============================================================================
set -euo pipefail
IFS=$'\n\t'

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}"
PKI_MOUNT="${PKI_MOUNT:-pki}"
PKI_ROLE="${PKI_ROLE:-server-cert}"
PRIMARY_CONTAINER="${PRIMARY_CONTAINER:-personel-postgres}"
PRIMARY_DB_USER="${PRIMARY_DB_USER:-postgres}"
PRIMARY_DB_NAME="${PRIMARY_DB_NAME:-personel}"
REPLICA_IP="${REPLICA_IP:-192.168.5.32}"
REPLICA_CN="${REPLICA_CN:-postgres-replica.personel.internal}"
SECRETS_DIR="${SECRETS_DIR:-/etc/personel/secrets}"
PASSWORD_FILE="${SECRETS_DIR}/postgres-replicator-password"
TS="$(date -u +%Y%m%dT%H%M%SZ)"
STAGE_DIR="${STAGE_DIR:-/tmp/personel-replica-bootstrap.${TS}}"

DRY_RUN=false
FORCE_PASSWORD=false
TEARDOWN=false

log()  { printf '[replica-bootstrap] %s\n' "$*"; }
warn() { printf '[replica-bootstrap] WARN: %s\n' "$*" >&2; }
err()  { printf '[replica-bootstrap] ERROR: %s\n' "$*" >&2; }
die()  { err "$*"; exit 1; }

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)        DRY_RUN=true; shift ;;
    --force-password) FORCE_PASSWORD=true; shift ;;
    --teardown)       TEARDOWN=true; shift ;;
    -h|--help)
      sed -n '2,55p' "$0"
      exit 0
      ;;
    *)
      err "unknown flag: $1"
      exit 1
      ;;
  esac
done

# ---------------------------------------------------------------------------
# Teardown path — drops replicator role + archives password file. Does NOT
# revoke the issued cert (Vault rotation handles that on its own schedule).
# ---------------------------------------------------------------------------
if [[ "${TEARDOWN}" == "true" ]]; then
  warn "teardown mode — this will DROP the replicator role on the primary"
  warn "the replica on vm5 will lose its WAL stream within seconds"
  read -r -p "type 'TEARDOWN' to confirm: " confirm
  [[ "${confirm}" == "TEARDOWN" ]] || die "aborted"

  if docker exec -i "${PRIMARY_CONTAINER}" \
       psql -U "${PRIMARY_DB_USER}" -d "${PRIMARY_DB_NAME}" \
       -v ON_ERROR_STOP=1 \
       -c "DROP ROLE IF EXISTS replicator;" >/dev/null 2>&1; then
    log "replicator role dropped"
  else
    err "failed to drop replicator role (psql exited non-zero)"
    exit 2
  fi

  if [[ -f "${PASSWORD_FILE}" ]]; then
    archive="${PASSWORD_FILE}.revoked.${TS}"
    mv "${PASSWORD_FILE}" "${archive}"
    log "password archived → ${archive}"
  fi
  log "teardown complete — operator must also tear down the replica compose stack on vm5"
  exit 0
fi

# ---------------------------------------------------------------------------
# Pre-flight
# ---------------------------------------------------------------------------
[[ "$(id -u)" -eq 0 ]] || die "must run as root (writes to ${SECRETS_DIR})"

for bin in docker vault jq openssl psql ping; do
  command -v "${bin}" >/dev/null 2>&1 || die "required binary not found: ${bin}"
done

[[ -n "${VAULT_TOKEN:-}" ]] || die "VAULT_TOKEN is required (unless --teardown)"
export VAULT_ADDR VAULT_TOKEN

log "pre-flight: vault @ ${VAULT_ADDR}"
vault token lookup >/dev/null 2>&1 \
  || die "vault token invalid or vault unreachable at ${VAULT_ADDR}"

log "pre-flight: pki mount ${PKI_MOUNT} role ${PKI_ROLE}"
vault read "${PKI_MOUNT}/roles/${PKI_ROLE}" >/dev/null 2>&1 \
  || die "pki role ${PKI_MOUNT}/${PKI_ROLE} not accessible — run ca-bootstrap.sh first"

log "pre-flight: primary container ${PRIMARY_CONTAINER}"
docker inspect -f '{{.State.Running}}' "${PRIMARY_CONTAINER}" 2>/dev/null | grep -qx true \
  || die "primary container ${PRIMARY_CONTAINER} is not running"

log "pre-flight: primary accepts connections"
docker exec -i "${PRIMARY_CONTAINER}" \
  pg_isready -U "${PRIMARY_DB_USER}" -h 127.0.0.1 -p 5432 >/dev/null 2>&1 \
  || die "primary pg_isready failed"

log "pre-flight: vm5 (${REPLICA_IP}) reachable"
ping -c 2 -W 2 "${REPLICA_IP}" >/dev/null 2>&1 \
  || warn "vm5 not responding to ping — continuing (ICMP may be blocked, but check before you scp)"

log "pre-flight: secrets dir ${SECRETS_DIR}"
if [[ ! -d "${SECRETS_DIR}" ]]; then
  if [[ "${DRY_RUN}" == "false" ]]; then
    install -d -m 0700 -o root -g root "${SECRETS_DIR}"
    log "created ${SECRETS_DIR}"
  else
    log "(dry-run) would create ${SECRETS_DIR}"
  fi
fi

# ---------------------------------------------------------------------------
# Generate replicator password (or reuse existing)
# ---------------------------------------------------------------------------
if [[ -s "${PASSWORD_FILE}" && "${FORCE_PASSWORD}" == "false" ]]; then
  log "replicator password already present at ${PASSWORD_FILE} — reusing (pass --force-password to rotate)"
  REPLICATOR_PW="$(tr -d '\n' < "${PASSWORD_FILE}")"
else
  log "generating new replicator password (48 bytes, base64)"
  REPLICATOR_PW="$(openssl rand -base64 48 | tr -d '\n')"
  if [[ "${DRY_RUN}" == "false" ]]; then
    umask 0077
    printf '%s' "${REPLICATOR_PW}" > "${PASSWORD_FILE}"
    chmod 0600 "${PASSWORD_FILE}"
    chown root:root "${PASSWORD_FILE}"
    log "password written → ${PASSWORD_FILE}"
  else
    log "(dry-run) would write password to ${PASSWORD_FILE}"
  fi
fi

# ---------------------------------------------------------------------------
# Create (or update) the replicator role on the primary
# ---------------------------------------------------------------------------
role_exists() {
  docker exec -i "${PRIMARY_CONTAINER}" \
    psql -U "${PRIMARY_DB_USER}" -d "${PRIMARY_DB_NAME}" \
    -tAc "SELECT 1 FROM pg_roles WHERE rolname='replicator'" 2>/dev/null \
  | grep -qx 1
}

if role_exists; then
  log "replicator role already exists on primary"
  if [[ "${FORCE_PASSWORD}" == "true" && "${DRY_RUN}" == "false" ]]; then
    log "rotating replicator password via ALTER ROLE"
    docker exec -i "${PRIMARY_CONTAINER}" \
      psql -U "${PRIMARY_DB_USER}" -d "${PRIMARY_DB_NAME}" \
      -v ON_ERROR_STOP=1 \
      -v newpass="${REPLICATOR_PW}" \
      -c "ALTER ROLE replicator WITH LOGIN REPLICATION PASSWORD :'newpass';" >/dev/null \
      || die "ALTER ROLE replicator failed"
    log "replicator password rotated"
  elif [[ "${FORCE_PASSWORD}" == "true" ]]; then
    log "(dry-run) would rotate replicator password"
  fi
else
  if [[ "${DRY_RUN}" == "false" ]]; then
    log "creating replicator role on primary"
    docker exec -i "${PRIMARY_CONTAINER}" \
      psql -U "${PRIMARY_DB_USER}" -d "${PRIMARY_DB_NAME}" \
      -v ON_ERROR_STOP=1 \
      -v newpass="${REPLICATOR_PW}" \
      -c "CREATE ROLE replicator WITH LOGIN REPLICATION PASSWORD :'newpass';" >/dev/null \
      || die "CREATE ROLE replicator failed"
    log "replicator role created"
  else
    log "(dry-run) would CREATE ROLE replicator WITH LOGIN REPLICATION"
  fi
fi

# ---------------------------------------------------------------------------
# Issue the postgres-replica server cert from Vault PKI
# ---------------------------------------------------------------------------
mkdir -p "${STAGE_DIR}"
chmod 0700 "${STAGE_DIR}"

CERT_JSON="${STAGE_DIR}/pki-response.json"
CERT_PEM="${STAGE_DIR}/postgres-replica.crt"
KEY_PEM="${STAGE_DIR}/postgres-replica.key"
ROOT_CA_PEM="${STAGE_DIR}/root_ca.crt"

log "issuing replica server cert from vault pki (CN=${REPLICA_CN} ip_sans=${REPLICA_IP})"
if [[ "${DRY_RUN}" == "false" ]]; then
  vault write -format=json "${PKI_MOUNT}/issue/${PKI_ROLE}" \
    common_name="${REPLICA_CN}" \
    alt_names="postgres-replica,postgres-replica.personel.internal" \
    ip_sans="${REPLICA_IP},127.0.0.1" \
    ttl="720h" \
    > "${CERT_JSON}" \
    || die "vault pki issue failed"

  jq -r .data.certificate       < "${CERT_JSON}" > "${CERT_PEM}"
  jq -r .data.private_key       < "${CERT_JSON}" > "${KEY_PEM}"
  jq -r .data.issuing_ca        < "${CERT_JSON}" > "${ROOT_CA_PEM}"

  chmod 0644 "${CERT_PEM}" "${ROOT_CA_PEM}"
  chmod 0600 "${KEY_PEM}"
  log "cert staged in ${STAGE_DIR}"
else
  log "(dry-run) would issue cert to ${STAGE_DIR}"
fi

# ---------------------------------------------------------------------------
# Stage the compose + conf + init script for scp to vm5
# ---------------------------------------------------------------------------
REPO_POSTGRES_DIR="${SCRIPT_DIR}/../compose/postgres"
for f in \
  docker-compose.replication-replica.yaml \
  postgresql.conf.replication-replica \
  pg-replica-init.sh \
; do
  src="${REPO_POSTGRES_DIR}/${f}"
  [[ -f "${src}" ]] || die "missing repo file: ${src}"
  cp -a "${src}" "${STAGE_DIR}/${f}"
done
chmod 0755 "${STAGE_DIR}/pg-replica-init.sh"

# Stage the password file alongside the certs — operator scp's the bundle
# once, then moves the password into /etc/personel/secrets on vm5.
cp -a "${PASSWORD_FILE}" "${STAGE_DIR}/postgres-replicator-password" 2>/dev/null || true
chmod 0600 "${STAGE_DIR}/postgres-replicator-password" 2>/dev/null || true

log "stage directory: ${STAGE_DIR}"

# ---------------------------------------------------------------------------
# Handoff instructions
# ---------------------------------------------------------------------------
cat <<EOF

=============================================================================
 Bootstrap complete on primary (vm3 / 192.168.5.44)
=============================================================================

Next steps — run from your workstation (NOT from this script):

 1. Apply the replication-primary compose overlay on vm3:

      ssh kartal@192.168.5.44 \\
        'cd /home/kartal/personel/infra/compose && \\
         docker compose \\
           -f docker-compose.yaml \\
           -f postgres/docker-compose.tls-override.yaml \\
           -f postgres/docker-compose.replication-primary.yaml \\
           up -d postgres'

    This swaps the pg_hba + postgresql.conf so the primary starts accepting
    WAL stream connections from ${REPLICA_IP}.

 2. Copy the staged bundle to vm5:

      scp -r ${STAGE_DIR}/ kartal@${REPLICA_IP}:/tmp/personel-replica-bundle

 3. On vm5, install the files into their final locations:

      ssh kartal@${REPLICA_IP} 'sudo bash -s' <<'REMOTE'
        set -euo pipefail
        install -d -m 0700 -o root -g root /etc/personel/secrets /etc/personel/tls /opt/personel/replica
        install -m 0600 -o root -g root /tmp/personel-replica-bundle/postgres-replicator-password /etc/personel/secrets/
        install -m 0644 -o root -g root /tmp/personel-replica-bundle/postgres-replica.crt       /etc/personel/tls/
        install -m 0600 -o 999  -g 999  /tmp/personel-replica-bundle/postgres-replica.key       /etc/personel/tls/
        install -m 0644 -o root -g root /tmp/personel-replica-bundle/root_ca.crt                /etc/personel/tls/
        install -m 0755 -o root -g root /tmp/personel-replica-bundle/pg-replica-init.sh         /opt/personel/replica/
        install -m 0644 -o root -g root /tmp/personel-replica-bundle/postgresql.conf.replication-replica /opt/personel/replica/
        install -m 0644 -o root -g root /tmp/personel-replica-bundle/docker-compose.replication-replica.yaml /opt/personel/replica/
        install -d -m 0700 -o 999  -g 999  /var/lib/personel/postgres-replica/data
        rm -rf /tmp/personel-replica-bundle
REMOTE

 4. Bring up the replica container on vm5:

      ssh kartal@${REPLICA_IP} \\
        'cd /opt/personel/replica && \\
         docker compose -f docker-compose.replication-replica.yaml up -d'

    First start runs pg_basebackup against ${REPLICA_IP%.*}.44 — expect
    2-10 minutes depending on database size.

 5. Validate from your workstation:

      ssh kartal@192.168.5.44 \\
        '/home/kartal/personel/infra/scripts/postgres-replication-test.sh'

    Success criteria: lag < 10 MB, apply within 5 s, pg_is_in_recovery()
    returns true on the replica.

=============================================================================
 Troubleshooting + rollback: docs/operations/postgres-replication.md
=============================================================================
EOF

exit 0
