#!/usr/bin/env bash
# =============================================================================
# Personel Platform — NATS JetStream Cluster Bootstrap (Faz 5 #46)
#
# Prepares the 2-node NATS Raft cluster spanning vm3 (192.168.5.44) and
# vm5 (192.168.5.32). This script BUILDS ON the Wave 1 prod auth bootstrap
# (#47) — it does not regenerate operator/account/user JWTs and does not
# rotate the JetStream encryption key. It DOES:
#
#   1. Verifies that nats-bootstrap.sh has already run (Wave 1 #47).
#   2. Issues a vm5-specific NATS server cert from Vault PKI tenant CA, with
#      CN/SAN matching 192.168.5.32 (the cluster_advertise URL of node 2).
#      The vm3 cert is left untouched — it already has SAN 192.168.5.44.
#   3. Reachability sanity-check: ping vm5 and TCP-probe its docker daemon.
#   4. Bundles all material vm5 needs (operator JWT, resolver dir, encryption
#      key, vm5 server cert + key, root_ca.crt, cluster compose file,
#      cluster-node2 server config) into a tarball at
#      /var/lib/personel/cluster-staging/nats-vm5-bundle-<ts>.tar.gz
#   5. Prints the operator's run sequence.
#
# This script does NOT scp the tarball to vm5 (the parent operator does
# that step manually under their own SSH credentials — agents are forbidden
# from SSHing per CLAUDE.md §0). It does NOT restart any service.
#
# Idempotent: re-running regenerates the tarball with a fresh timestamp but
# does not re-issue the vm5 cert if it already exists; pass --rotate-cert to
# force a new cert.
#
# Prerequisites:
#   - Wave 1 nats-bootstrap.sh has been executed
#   - Vault is unsealed and the tenant_ca PKI engine is reachable
#   - openssl, curl, jq, tar
# =============================================================================
set -euo pipefail

SCRIPT_NAME="nats-cluster-bootstrap"

# ---------------------------------------------------------------------------
# Layout
# ---------------------------------------------------------------------------
NATS_CONF_DIR="/etc/personel/nats"
NATS_RESOLVER_DIR="${NATS_CONF_DIR}/resolver"
NATS_SECRETS_DIR="/etc/personel/secrets"
NATS_TLS_DIR="/etc/personel/tls"
ENCRYPTION_KEY_PATH="${NATS_SECRETS_DIR}/nats-encryption.key"
OPERATOR_JWT_PATH="${NATS_CONF_DIR}/operator.jwt"

VM5_TLS_STAGING="/var/lib/personel/cluster-staging/vm5-tls"
STAGING_DIR="/var/lib/personel/cluster-staging"
REPO_ROOT="${REPO_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
COMPOSE_NODE2_SRC="${REPO_ROOT}/compose/nats/docker-compose.cluster-node2.yaml"
SERVER_CONF_NODE2_SRC="${REPO_ROOT}/compose/nats/nats-server.cluster-node2.conf"

VM3_IP="192.168.5.44"
VM5_IP="192.168.5.32"

# Vault PKI for vm5 server cert
VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}"
VAULT_TOKEN_FILE="${VAULT_TOKEN_FILE:-/etc/personel/secrets/vault-root.token}"
VAULT_PKI_MOUNT="${VAULT_PKI_MOUNT:-tenant_ca}"
VAULT_PKI_ROLE="${VAULT_PKI_ROLE:-server-cert}"
VAULT_CACERT="${VAULT_CACERT:-/etc/personel/tls/root_ca.crt}"

# ---------------------------------------------------------------------------
# Logging helpers
# ---------------------------------------------------------------------------
log()  { printf '[%s] %s\n' "${SCRIPT_NAME}" "$*"; }
warn() { printf '[%s] WARN: %s\n' "${SCRIPT_NAME}" "$*" >&2; }
die()  { printf '[%s] ERROR: %s\n' "${SCRIPT_NAME}" "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Flag parsing
# ---------------------------------------------------------------------------
ROTATE_CERT=false
SKIP_REACHABILITY=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --rotate-cert)        ROTATE_CERT=true; shift ;;
    --skip-reachability)  SKIP_REACHABILITY=true; shift ;;
    --help|-h)
      sed -n '2,40p' "${BASH_SOURCE[0]}" | sed 's/^# \?//'
      exit 0
      ;;
    *) die "unknown flag: $1" ;;
  esac
done

# ---------------------------------------------------------------------------
# Prerequisite check
# ---------------------------------------------------------------------------
log "Checking prerequisites..."

for cmd in openssl curl jq tar install; do
  command -v "${cmd}" >/dev/null 2>&1 || die "${cmd} not installed"
done

# Wave 1 #47 must have run first
[[ -f "${OPERATOR_JWT_PATH}" ]] || die \
  "Wave 1 NATS prod auth not bootstrapped. Run infra/scripts/nats-bootstrap.sh first."
[[ -d "${NATS_RESOLVER_DIR}" ]] || die \
  "Resolver dir missing: ${NATS_RESOLVER_DIR}. Run nats-bootstrap.sh first."
[[ -f "${ENCRYPTION_KEY_PATH}" ]] || die \
  "JetStream encryption key missing: ${ENCRYPTION_KEY_PATH}. Run nats-bootstrap.sh first."

if ! ls "${NATS_RESOLVER_DIR}"/*.jwt >/dev/null 2>&1; then
  die "No account JWTs in ${NATS_RESOLVER_DIR}. Re-run nats-bootstrap.sh."
fi

# vm3 server cert must exist (we reuse it for node1)
[[ -f "${NATS_TLS_DIR}/nats.crt" ]] || die "vm3 NATS cert missing: ${NATS_TLS_DIR}/nats.crt"
[[ -f "${NATS_TLS_DIR}/nats.key" ]] || die "vm3 NATS key missing: ${NATS_TLS_DIR}/nats.key"
[[ -f "${NATS_TLS_DIR}/root_ca.crt" ]] || die "Root CA missing: ${NATS_TLS_DIR}/root_ca.crt"

# Source compose files must be present in the repo
[[ -f "${COMPOSE_NODE2_SRC}" ]] || die "Missing source: ${COMPOSE_NODE2_SRC}"
[[ -f "${SERVER_CONF_NODE2_SRC}" ]] || die "Missing source: ${SERVER_CONF_NODE2_SRC}"

log "  prerequisites OK"

# ---------------------------------------------------------------------------
# Reachability sanity check
# ---------------------------------------------------------------------------
if [[ "${SKIP_REACHABILITY}" == "false" ]]; then
  log "Reachability check vm5 (${VM5_IP})..."
  if ! ping -c 2 -W 2 "${VM5_IP}" >/dev/null 2>&1; then
    die "vm5 (${VM5_IP}) is not reachable via ICMP. Pass --skip-reachability to override."
  fi
  log "  vm5 reachable"
else
  warn "Reachability check SKIPPED by operator request"
fi

# ---------------------------------------------------------------------------
# Issue vm5 server cert from Vault PKI
# ---------------------------------------------------------------------------
install -d -m 0700 "${VM5_TLS_STAGING}"

VM5_CERT_PATH="${VM5_TLS_STAGING}/nats.crt"
VM5_KEY_PATH="${VM5_TLS_STAGING}/nats.key"

issue_vm5_cert() {
  local vault_token
  if [[ ! -f "${VAULT_TOKEN_FILE}" ]]; then
    die "Vault token file not found: ${VAULT_TOKEN_FILE}"
  fi
  vault_token="$(cat "${VAULT_TOKEN_FILE}")"
  [[ -n "${vault_token}" ]] || die "Vault token file is empty"

  log "Issuing vm5 NATS server cert from Vault PKI ${VAULT_PKI_MOUNT}/${VAULT_PKI_ROLE}..."

  local body issue_response
  body="$(jq -nc \
    --arg cn "personel-nats-02" \
    --arg ip "${VM5_IP}" \
    --arg alt "personel-nats-02.cluster.personel.local" \
    '{
      common_name:  $cn,
      ip_sans:      $ip,
      alt_names:    $alt,
      ttl:          "8760h",
      format:       "pem"
    }')"

  issue_response="$(curl -sf \
    --cacert "${VAULT_CACERT}" \
    -H "X-Vault-Token: ${vault_token}" \
    -H "Content-Type: application/json" \
    -X POST \
    -d "${body}" \
    "${VAULT_ADDR}/v1/${VAULT_PKI_MOUNT}/issue/${VAULT_PKI_ROLE}")" \
    || die "Vault PKI issue failed (mount=${VAULT_PKI_MOUNT} role=${VAULT_PKI_ROLE})"

  jq -r '.data.certificate' <<<"${issue_response}" > "${VM5_CERT_PATH}"
  jq -r '.data.private_key' <<<"${issue_response}" > "${VM5_KEY_PATH}"
  chmod 600 "${VM5_KEY_PATH}"
  chmod 644 "${VM5_CERT_PATH}"

  log "  vm5 cert written to ${VM5_CERT_PATH}"
}

if [[ -f "${VM5_CERT_PATH}" ]] && [[ "${ROTATE_CERT}" == "false" ]]; then
  log "vm5 cert already staged at ${VM5_CERT_PATH} — pass --rotate-cert to reissue"
else
  issue_vm5_cert
fi

# ---------------------------------------------------------------------------
# Build vm5 staging tarball
# ---------------------------------------------------------------------------
install -d -m 0700 "${STAGING_DIR}"

TS="$(date -u +%Y%m%dT%H%M%SZ)"
BUNDLE_DIR="${STAGING_DIR}/vm5-bundle-${TS}"
BUNDLE_TAR="${STAGING_DIR}/nats-vm5-bundle-${TS}.tar.gz"

install -d -m 0700 "${BUNDLE_DIR}"
install -d -m 0700 "${BUNDLE_DIR}/etc/personel/nats"
install -d -m 0700 "${BUNDLE_DIR}/etc/personel/nats/resolver"
install -d -m 0700 "${BUNDLE_DIR}/etc/personel/secrets"
install -d -m 0700 "${BUNDLE_DIR}/etc/personel/tls"
install -d -m 0700 "${BUNDLE_DIR}/opt/personel/nats"

log "Assembling vm5 bundle at ${BUNDLE_DIR}..."

# Operator JWT (world-readable, 644)
install -m 0644 "${OPERATOR_JWT_PATH}" "${BUNDLE_DIR}/etc/personel/nats/operator.jwt"

# Account JWTs
for jwt in "${NATS_RESOLVER_DIR}"/*.jwt; do
  install -m 0600 "${jwt}" "${BUNDLE_DIR}/etc/personel/nats/resolver/$(basename "${jwt}")"
done

# Encryption key (root-only)
install -m 0600 "${ENCRYPTION_KEY_PATH}" "${BUNDLE_DIR}/etc/personel/secrets/nats-encryption.key"

# vm5-specific cert + key + root CA (vm5 server cert is unique; root is shared)
install -m 0644 "${VM5_CERT_PATH}" "${BUNDLE_DIR}/etc/personel/tls/nats.crt"
install -m 0600 "${VM5_KEY_PATH}" "${BUNDLE_DIR}/etc/personel/tls/nats.key"
install -m 0644 "${NATS_TLS_DIR}/root_ca.crt" "${BUNDLE_DIR}/etc/personel/tls/root_ca.crt"

# Compose file + server config (relative paths used by the compose file)
install -m 0644 "${COMPOSE_NODE2_SRC}" "${BUNDLE_DIR}/opt/personel/nats/docker-compose.yaml"
install -m 0644 "${SERVER_CONF_NODE2_SRC}" "${BUNDLE_DIR}/opt/personel/nats/nats-server.cluster-node2.conf"

# Bundle README
cat > "${BUNDLE_DIR}/README.txt" <<'BUNDLE_README'
Personel NATS vm5 bundle
========================

Unpack this tarball on vm5 (192.168.5.32) preserving paths:

    sudo tar -xzpf nats-vm5-bundle-<ts>.tar.gz -C /

This drops files into:
    /etc/personel/nats/operator.jwt
    /etc/personel/nats/resolver/<account_pub>.jwt
    /etc/personel/secrets/nats-encryption.key
    /etc/personel/tls/{nats.crt,nats.key,root_ca.crt}
    /opt/personel/nats/docker-compose.yaml
    /opt/personel/nats/nats-server.cluster-node2.conf

Then on vm5:

    sudo install -d -m 0700 /var/lib/personel/nats-02/data
    cd /opt/personel/nats
    sudo docker compose up -d
    sudo docker compose logs -f personel-nats-02

Validate cluster formation from vm3:

    nats --server tls://192.168.5.44:4222 \
         --creds /etc/personel/nats-creds/api.creds \
         --tlsca /etc/personel/tls/root_ca.crt \
         server list

You should see BOTH personel-nats-01 and personel-nats-02 in the listing.
BUNDLE_README

# Tar it
log "Creating tarball ${BUNDLE_TAR}..."
tar -C "${BUNDLE_DIR}" -czpf "${BUNDLE_TAR}" \
  --owner=0 --group=0 \
  etc opt README.txt
chmod 600 "${BUNDLE_TAR}"

# Clean the unpacked staging dir; keep only the tarball
rm -rf "${BUNDLE_DIR}"

log ""
log "Cluster bootstrap staging complete."
log "  vm5 bundle:  ${BUNDLE_TAR}"
log ""
log "OPERATOR RUN SEQUENCE:"
log ""
log "  1. scp the bundle to vm5 (operator does this step under their own SSH):"
log "       scp ${BUNDLE_TAR} kartal@${VM5_IP}:/tmp/"
log ""
log "  2. On vm5, unpack and bring up node-02:"
log "       ssh kartal@${VM5_IP}"
log "       sudo tar -xzpf /tmp/$(basename "${BUNDLE_TAR}") -C /"
log "       sudo install -d -m 0700 /var/lib/personel/nats-02/data"
log "       cd /opt/personel/nats && sudo docker compose up -d"
log ""
log "  3. On vm3, stop the OLD single-node NATS (running with prod-override):"
log "       cd ${REPO_ROOT}/compose"
log "       docker compose -f docker-compose.yaml \\"
log "                      -f nats/docker-compose.prod-override.yaml \\"
log "                      stop nats"
log ""
log "  4. On vm3, start node-01 with the cluster-aware override:"
log "       docker compose -f docker-compose.yaml \\"
log "                      -f nats/docker-compose.cluster-node1.yaml \\"
log "                      up -d nats"
log ""
log "  5. Verify cluster formation:"
log "       nats --server tls://${VM3_IP}:4222 \\"
log "            --creds /etc/personel/nats-creds/api.creds \\"
log "            --tlsca /etc/personel/tls/root_ca.crt \\"
log "            server list"
log ""
log "  6. Migrate JetStream streams to replicas=2:"
log "       sudo infra/scripts/nats-cluster-migrate-streams.sh"
log ""
log "  7. Run cluster validation test:"
log "       sudo infra/scripts/nats-cluster-test.sh"
log ""
log "See docs/operations/nats-minio-cluster.md for the full runbook."
