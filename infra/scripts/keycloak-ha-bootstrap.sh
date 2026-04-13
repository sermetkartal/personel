#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Keycloak 2-node HA Bootstrap
# =============================================================================
# Faz 5 Madde 52
#
# Prepares vm3 + vm5 to replace the standalone Keycloak container (per §0,
# Keycloak currently runs outside compose) with a two-node HA deployment
# using Infinispan + JDBC_PING2 discovery against the shared Postgres.
#
# Steps:
#   1. Pre-flight: Vault PKI reachable, Postgres reachable, existing
#      standalone Keycloak state, vm5 reachable
#   2. Ensure the Postgres `keycloak` database exists (create if missing)
#   3. Issue keycloak-01.crt + keycloak-02.crt from Vault PKI
#   4. Export the existing `personel` realm BEFORE the migration so we have
#      a known-good restore artifact and a fresh import JSON for the new
#      container
#   5. Stage a tarball for vm5 operator scp
#   6. Print the operator run sequence (stop old → start node-01 → wait
#      for cache cluster up → start node-02 → import realm → verify HA)
#
# Idempotent: cert issuance is skipped if certs are still valid; DB
# creation is a no-op if the database already exists; export file is
# timestamped.
#
# Usage:
#   VAULT_TOKEN=hvs.xxxx ./keycloak-ha-bootstrap.sh
#   VAULT_TOKEN=hvs.xxxx ./keycloak-ha-bootstrap.sh --force
#   VAULT_TOKEN=hvs.xxxx ./keycloak-ha-bootstrap.sh --dry-run
#
# Exit codes:
#   0 OK  1 usage/config  2 pre-flight failure  3 cert failure  4 export failure
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}"
PKI_MOUNT="${PKI_MOUNT:-pki}"
PKI_ROLE="${PKI_ROLE:-server-cert}"
TLS_DIR="${TLS_DIR:-/etc/personel/tls}"
MIN_DAYS_LEFT="${MIN_DAYS_LEFT:-7}"

VM3_IP="${VM3_IP:-192.168.5.44}"
VM5_IP="${VM5_IP:-192.168.5.32}"

KC_DB_NAME="${KC_DB_NAME:-keycloak}"
KC_DB_USER="${KC_DB_USER:-app_keycloak}"
KC_REALM="${KC_REALM:-personel}"

OLD_CONTAINER="${OLD_CONTAINER:-personel-keycloak}"
STAGE_DIR="${STAGE_DIR:-/var/lib/personel/staging/keycloak-ha}"

FORCE=false
DRY_RUN=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --force)    FORCE=true;   shift ;;
    --dry-run)  DRY_RUN=true; shift ;;
    -h|--help)  sed -n '2,35p' "$0"; exit 0 ;;
    *)          printf 'unknown flag: %s\n' "$1" >&2; exit 1 ;;
  esac
done

log()  { printf '[kc-ha] %s\n' "$*"; }
warn() { printf '[kc-ha] WARN: %s\n' "$*" >&2; }
err()  { printf '[kc-ha] ERROR: %s\n' "$*" >&2; }

# -----------------------------------------------------------------------------
# Pre-flight
# -----------------------------------------------------------------------------
preflight() {
  log "pre-flight checks"

  for bin in vault jq openssl curl tar docker; do
    if ! command -v "${bin}" >/dev/null 2>&1; then
      err "missing binary: ${bin}"
      return 2
    fi
  done

  if [[ -z "${VAULT_TOKEN:-}" ]]; then
    err "VAULT_TOKEN environment variable is required"
    return 1
  fi
  export VAULT_ADDR VAULT_TOKEN

  if ! vault token lookup >/dev/null 2>&1; then
    err "VAULT_TOKEN invalid or vault unreachable at ${VAULT_ADDR}"
    return 2
  fi
  if ! vault read -format=json "${PKI_MOUNT}/roles/${PKI_ROLE}" >/dev/null 2>&1; then
    err "Vault PKI role '${PKI_MOUNT}/roles/${PKI_ROLE}' not found."
    return 2
  fi

  if [[ ! -f "${TLS_DIR}/tenant_ca.crt" ]]; then
    err "${TLS_DIR}/tenant_ca.crt missing — run ca-bootstrap.sh first"
    return 2
  fi

  # Postgres container
  if ! docker ps --format '{{.Names}}' | grep -q '^personel-postgres$'; then
    err "personel-postgres container not running — needed for JDBC_PING + realm store"
    return 2
  fi

  # Old standalone Keycloak state (warn only — operator stops it in sequence)
  if docker ps --format '{{.Names}}' | grep -q "^${OLD_CONTAINER}$"; then
    warn "standalone Keycloak (${OLD_CONTAINER}) is still running."
    warn "Export runs against it now; stop it before starting keycloak-01."
  elif docker ps -a --format '{{.Names}}' | grep -q "^${OLD_CONTAINER}$"; then
    warn "standalone Keycloak (${OLD_CONTAINER}) exists but is stopped."
    warn "Realm export will be skipped — operator must supply realm-personel.json"
    warn "manually if the database is empty."
  fi

  # vm5 reachability
  if ! timeout 3 bash -c "cat < /dev/tcp/${VM5_IP}/22" >/dev/null 2>&1; then
    warn "vm5 (${VM5_IP}:22) not reachable over TCP — operator must scp the"
    warn "staging bundle manually."
  fi

  return 0
}

# -----------------------------------------------------------------------------
# Ensure Postgres `keycloak` database exists
# -----------------------------------------------------------------------------
ensure_keycloak_db() {
  log "ensuring Postgres database '${KC_DB_NAME}' exists"
  if [[ "${DRY_RUN}" == "true" ]]; then
    log "  DRY-RUN: would ensure database"
    return 0
  fi
  if docker exec personel-postgres psql -U postgres -tAc \
       "SELECT 1 FROM pg_database WHERE datname='${KC_DB_NAME}'" 2>/dev/null \
       | grep -q '^1$'; then
    log "  database already present"
    return 0
  fi
  log "  creating database ${KC_DB_NAME} owned by ${KC_DB_USER}"
  docker exec personel-postgres psql -U postgres -c \
    "CREATE DATABASE ${KC_DB_NAME} OWNER ${KC_DB_USER};"
}

# -----------------------------------------------------------------------------
# Cert issue helpers
# -----------------------------------------------------------------------------
days_remaining() {
  local crt="$1"
  [[ -f "${crt}" ]] || { echo "-1"; return; }
  local end_str end_epoch now_epoch
  if ! end_str=$(openssl x509 -enddate -noout -in "${crt}" 2>/dev/null); then
    echo "-1"; return
  fi
  end_str="${end_str#notAfter=}"
  if ! end_epoch=$(date -d "${end_str}" +%s 2>/dev/null); then
    echo "-1"; return
  fi
  now_epoch=$(date +%s)
  echo $(( (end_epoch - now_epoch) / 86400 ))
}

issue_node_cert() {
  local node_name="$1"  # keycloak-01 | keycloak-02
  local node_ip="$2"    # VM3_IP | VM5_IP

  local crt="${TLS_DIR}/${node_name}.crt"
  local key="${TLS_DIR}/${node_name}.key"

  if [[ "${FORCE}" == "false" && -f "${crt}" ]]; then
    local left
    left=$(days_remaining "${crt}")
    if [[ "${left}" -gt "${MIN_DAYS_LEFT}" ]]; then
      log "  SKIP ${node_name} — cert valid for ${left}d"
      return 0
    fi
  fi

  if [[ "${DRY_RUN}" == "true" ]]; then
    log "  DRY-RUN ${node_name}: would issue"
    return 0
  fi

  log "  ISSUE ${node_name}"
  local resp
  if ! resp=$(vault write -format=json "${PKI_MOUNT}/issue/${PKI_ROLE}" \
        common_name="${node_name}.personel.internal" \
        alt_names="${node_name},keycloak-01,keycloak-02,keycloak,keycloak.personel.internal,sso.personel.internal" \
        ip_sans="${node_ip},${VM3_IP},${VM5_IP},127.0.0.1" \
        ttl="720h" 2>&1); then
    err "  vault issue failed for ${node_name}:"
    err "  ${resp}"
    return 3
  fi

  umask 077
  echo "${resp}" | jq -r '.data.certificate' > "${crt}"
  echo "${resp}" | jq -r '.data.private_key' > "${key}"
  chmod 0640 "${crt}" "${key}"
  log "  wrote ${crt}"
}

# -----------------------------------------------------------------------------
# Realm export from existing standalone Keycloak
# -----------------------------------------------------------------------------
export_realm() {
  if ! docker ps --format '{{.Names}}' | grep -q "^${OLD_CONTAINER}$"; then
    warn "skipping realm export — ${OLD_CONTAINER} is not running"
    return 0
  fi

  local ts
  ts=$(date -u +%Y%m%dT%H%M%SZ)
  local out_dir="${STAGE_DIR}/realm-export-${ts}"

  if [[ "${DRY_RUN}" == "true" ]]; then
    log "DRY-RUN: would export realm ${KC_REALM} to ${out_dir}"
    return 0
  fi

  log "exporting realm ${KC_REALM} from ${OLD_CONTAINER}"
  mkdir -p "${out_dir}"

  # kc.sh export needs to write inside the container; copy out afterwards.
  if ! docker exec "${OLD_CONTAINER}" \
        /opt/keycloak/bin/kc.sh export \
          --dir /tmp/realm-export \
          --realm "${KC_REALM}" \
          --users realm_file 2>&1 | tail -n 20; then
    err "kc.sh export failed"
    return 4
  fi
  docker cp "${OLD_CONTAINER}:/tmp/realm-export/." "${out_dir}/"
  docker exec "${OLD_CONTAINER}" rm -rf /tmp/realm-export || true

  # Keep a flat copy at the well-known path for compose bind-mount
  if [[ -f "${out_dir}/${KC_REALM}-realm.json" ]]; then
    install -m 0644 "${out_dir}/${KC_REALM}-realm.json" \
      "${REPO_ROOT}/infra/compose/keycloak/realm-personel.exported.json"
    log "exported realm → ${out_dir}/${KC_REALM}-realm.json"
    log "flat copy     → infra/compose/keycloak/realm-personel.exported.json"
  else
    warn "expected ${KC_REALM}-realm.json not found in export directory"
  fi
}

# -----------------------------------------------------------------------------
# Stage vm5 bundle
# -----------------------------------------------------------------------------
stage_vm5_bundle() {
  if [[ "${DRY_RUN}" == "true" ]]; then
    log "DRY-RUN: would stage vm5 bundle"
    return 0
  fi

  log "staging vm5 bundle at ${STAGE_DIR}"
  mkdir -p "${STAGE_DIR}/tls" "${STAGE_DIR}/compose"

  install -m 0644 \
    "${REPO_ROOT}/infra/compose/keycloak/docker-compose.ha-node2.yaml" \
    "${STAGE_DIR}/compose/docker-compose.ha-node2.yaml"
  install -m 0644 \
    "${REPO_ROOT}/infra/compose/keycloak/cache-ispn-jdbc-ping.xml" \
    "${STAGE_DIR}/compose/cache-ispn-jdbc-ping.xml"
  install -m 0644 \
    "${REPO_ROOT}/infra/compose/keycloak/realm-personel.json" \
    "${STAGE_DIR}/compose/realm-personel.json"

  install -m 0640 "${TLS_DIR}/keycloak-02.crt" "${STAGE_DIR}/tls/keycloak-02.crt"
  install -m 0640 "${TLS_DIR}/keycloak-02.key" "${STAGE_DIR}/tls/keycloak-02.key"
  install -m 0644 "${TLS_DIR}/tenant_ca.crt"   "${STAGE_DIR}/tls/tenant_ca.crt"

  local tarball="${STAGE_DIR}/keycloak-ha-node2-bundle.tar.gz"
  tar -C "${STAGE_DIR}" -czf "${tarball}" tls compose
  log "bundle: ${tarball}"
}

# -----------------------------------------------------------------------------
# Runbook print
# -----------------------------------------------------------------------------
print_runbook() {
  cat <<RUNBOOK

================================================================================
KEYCLOAK 2-NODE HA — OPERATOR RUN SEQUENCE
================================================================================

1. On vm3 (${VM3_IP}) — stop the existing standalone Keycloak:
     docker stop ${OLD_CONTAINER} || true
     docker rm   ${OLD_CONTAINER} || true

2. On vm3 — start keycloak-01 via overlay:
     cd ${REPO_ROOT}/infra/compose
     docker compose \\
       -f docker-compose.yaml \\
       -f keycloak/docker-compose.ha-node1.yaml \\
       up -d keycloak-01
     docker logs -f personel-keycloak-01  # wait for 'Keycloak X started'

   Wait until JGroups reports the view is size 1 (sole member):
     docker logs personel-keycloak-01 2>&1 | grep -i 'ISPN000094\\|view:'

3. Copy the bundle to vm5:
     scp ${STAGE_DIR}/keycloak-ha-node2-bundle.tar.gz \\
         kartal@${VM5_IP}:/tmp/

4. On vm5 (${VM5_IP}):
     sudo mkdir -p /etc/personel/tls
     sudo tar -C /tmp -xzf /tmp/keycloak-ha-node2-bundle.tar.gz
     sudo install -m 0640 /tmp/tls/*              /etc/personel/tls/
     sudo install -m 0644 /tmp/tls/tenant_ca.crt  /etc/personel/tls/

     mkdir -p ~/personel-keycloak-node2
     cp /tmp/compose/* ~/personel-keycloak-node2/
     cd ~/personel-keycloak-node2

     cat > .env <<EOF
KEYCLOAK_DB_USER=${KC_DB_USER}
KEYCLOAK_DB_PASSWORD=<same-as-vm3>
KEYCLOAK_DB_URL=jdbc:postgresql://${VM3_IP}:5432/${KC_DB_NAME}
KEYCLOAK_HOSTNAME=keycloak.personel.internal
EOF

     docker compose -f docker-compose.ha-node2.yaml up -d

5. Verify both nodes joined the JGroups view (run on vm3):
     docker logs personel-keycloak-01 2>&1 | \\
       grep -E 'ISPN000094|view: \\[' | tail -n 3
     # Expect 'view: [keycloak-01|N, keycloak-02|N]'

6. Run HA validation:
     OPENSEARCH_ADMIN_PASSWORD=<irrelevant, not needed> \\
     KEYCLOAK_NODE1_URL=http://127.0.0.1:8080 \\
     KEYCLOAK_NODE2_URL=http://${VM5_IP}:8080 \\
     KEYCLOAK_ADMIN_USER=admin \\
     KEYCLOAK_ADMIN_PASSWORD=<pwd> \\
     ${SCRIPT_DIR}/keycloak-ha-test.sh

7. Rollback (emergency):
     docker compose -f ${REPO_ROOT}/infra/compose/docker-compose.yaml up -d keycloak
     docker stop personel-keycloak-01
     docker rm   personel-keycloak-01
     # On vm5:
     docker compose -f docker-compose.ha-node2.yaml down

================================================================================
RUNBOOK
}

# -----------------------------------------------------------------------------
# Main
# -----------------------------------------------------------------------------
main() {
  preflight || exit $?

  ensure_keycloak_db

  log "issuing node certs from Vault PKI"
  issue_node_cert "keycloak-01" "${VM3_IP}"
  issue_node_cert "keycloak-02" "${VM5_IP}"

  export_realm

  stage_vm5_bundle

  print_runbook

  log "bootstrap complete — see runbook above"
}

main "$@"
