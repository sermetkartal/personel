#!/usr/bin/env bash
# =============================================================================
# Personel Platform — OpenSearch 2-node Cluster Bootstrap
# =============================================================================
# Faz 5 Madde 51
#
# Prepares vm3 + vm5 for a two-node OpenSearch cluster:
#   1. Pre-flight: verify vm3 tools, vm5 reachable, existing single-node state
#   2. Issue opensearch-01.crt + opensearch-02.crt from Vault PKI server-cert
#      role. SAN lists carry both IPs + both hostnames so either cert can
#      terminate transport on any node.
#   3. Stage a tarball for vm5 operator scp: compose file, node-2 config,
#      node-2 cert/key, tenant CA.
#   4. Print the exact run sequence the operator must follow (this script does
#      NOT ssh to vm5; per §0 only local + explicit SSH allowed).
#
# Idempotent: skips cert issuance if existing cert has > MIN_DAYS_LEFT; skips
# staging dir creation if already present.
#
# Usage:
#   VAULT_TOKEN=hvs.xxxx ./opensearch-cluster-bootstrap.sh
#   VAULT_TOKEN=hvs.xxxx ./opensearch-cluster-bootstrap.sh --force
#   VAULT_TOKEN=hvs.xxxx ./opensearch-cluster-bootstrap.sh --dry-run
#
# Exit codes:
#   0 OK  1 config/usage error  2 pre-flight failure  3 cert failure
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

STAGE_DIR="${STAGE_DIR:-/var/lib/personel/staging/opensearch-cluster}"

FORCE=false
DRY_RUN=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --force)    FORCE=true;   shift ;;
    --dry-run)  DRY_RUN=true; shift ;;
    -h|--help)  sed -n '2,30p' "$0"; exit 0 ;;
    *)          printf 'unknown flag: %s\n' "$1" >&2; exit 1 ;;
  esac
done

log()  { printf '[os-cluster] %s\n' "$*"; }
warn() { printf '[os-cluster] WARN: %s\n' "$*" >&2; }
err()  { printf '[os-cluster] ERROR: %s\n' "$*" >&2; }

# -----------------------------------------------------------------------------
# Pre-flight
# -----------------------------------------------------------------------------
preflight() {
  log "pre-flight checks"

  for bin in vault jq openssl curl tar docker; do
    if ! command -v "${bin}" >/dev/null 2>&1; then
      err "required binary not found: ${bin}"
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
    err "Run infra/scripts/ca-bootstrap.sh first."
    return 2
  fi

  # vm5 reachability (ICMP is often blocked; prefer TCP probe on SSH:22)
  if ! timeout 3 bash -c "cat < /dev/tcp/${VM5_IP}/22" >/dev/null 2>&1; then
    warn "vm5 (${VM5_IP}:22) not reachable over TCP — operator must scp the"
    warn "staging bundle to vm5 manually after the script finishes."
  fi

  # existing single-node check — warn if still running
  if docker ps --format '{{.Names}}' 2>/dev/null | grep -q '^personel-opensearch$'; then
    warn "single-node personel-opensearch is still running."
    warn "It MUST be stopped before starting opensearch-01:"
    warn "  docker compose -f ${REPO_ROOT}/infra/compose/docker-compose.yaml stop opensearch"
  fi

  # tenant_ca.crt needed for bundle
  if [[ ! -f "${TLS_DIR}/tenant_ca.crt" ]]; then
    err "${TLS_DIR}/tenant_ca.crt missing — ca-bootstrap.sh has not been run"
    return 2
  fi

  return 0
}

# -----------------------------------------------------------------------------
# Cert expiry helper (days remaining; -1 if missing/invalid)
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

# -----------------------------------------------------------------------------
# Issue one node cert. Both nodes share the same SAN set so the same cert can
# reach the cluster over any of the hosts it's hosted on.
# -----------------------------------------------------------------------------
issue_node_cert() {
  local node_name="$1"   # opensearch-01 | opensearch-02
  local node_ip="$2"     # 192.168.5.44 | 192.168.5.32

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
    log "  DRY-RUN ${node_name}: would issue CN=${node_name}.personel.internal"
    return 0
  fi

  log "  ISSUE ${node_name}"
  local resp
  if ! resp=$(vault write -format=json "${PKI_MOUNT}/issue/${PKI_ROLE}" \
        common_name="${node_name}.personel.internal" \
        alt_names="${node_name},opensearch-01,opensearch-02,opensearch,opensearch.personel.internal" \
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
# Stage vm5 bundle
# -----------------------------------------------------------------------------
stage_vm5_bundle() {
  if [[ "${DRY_RUN}" == "true" ]]; then
    log "DRY-RUN: would stage vm5 bundle at ${STAGE_DIR}"
    return 0
  fi

  log "staging vm5 bundle at ${STAGE_DIR}"
  mkdir -p "${STAGE_DIR}/tls" "${STAGE_DIR}/compose"

  install -m 0644 \
    "${REPO_ROOT}/infra/compose/opensearch/opensearch.cluster-node2.yml" \
    "${STAGE_DIR}/compose/opensearch.cluster-node2.yml"
  install -m 0644 \
    "${REPO_ROOT}/infra/compose/opensearch/docker-compose.cluster-node2.yaml" \
    "${STAGE_DIR}/compose/docker-compose.cluster-node2.yaml"

  install -m 0640 "${TLS_DIR}/opensearch-02.crt" "${STAGE_DIR}/tls/opensearch-02.crt"
  install -m 0640 "${TLS_DIR}/opensearch-02.key" "${STAGE_DIR}/tls/opensearch-02.key"
  install -m 0644 "${TLS_DIR}/tenant_ca.crt"     "${STAGE_DIR}/tls/tenant_ca.crt"

  local tarball="${STAGE_DIR}/opensearch-cluster-node2-bundle.tar.gz"
  tar -C "${STAGE_DIR}" -czf "${tarball}" tls compose
  log "bundle: ${tarball}"
}

# -----------------------------------------------------------------------------
# Print the run sequence the operator must follow
# -----------------------------------------------------------------------------
print_runbook() {
  cat <<RUNBOOK

================================================================================
OPENSEARCH 2-NODE CLUSTER — OPERATOR RUN SEQUENCE
================================================================================

1. On vm3 (${VM3_IP}) — stop the existing single-node OpenSearch:
     cd ${REPO_ROOT}/infra/compose
     docker compose stop opensearch
     # OPTIONAL but recommended: warm-start the new volume
     sudo mkdir -p /var/lib/personel/opensearch-01/data
     sudo rsync -aHAX --numeric-ids \\
       /var/lib/personel/opensearch/data/ \\
       /var/lib/personel/opensearch-01/data/
     sudo chown -R 1000:1000 /var/lib/personel/opensearch-01/data

2. On vm3 — start opensearch-01 via the overlay compose:
     cd ${REPO_ROOT}/infra/compose
     docker compose \\
       -f docker-compose.yaml \\
       -f opensearch/docker-compose.cluster-node1.yaml \\
       up -d opensearch-01
     docker logs -f personel-opensearch-01  # until 'started' appears

3. Copy the bundle to vm5:
     scp ${STAGE_DIR}/opensearch-cluster-node2-bundle.tar.gz \\
         kartal@${VM5_IP}:/tmp/

4. On vm5 (${VM5_IP}):
     sudo mkdir -p /etc/personel/tls /var/lib/personel/opensearch-02/data
     sudo chown -R 1000:1000 /var/lib/personel/opensearch-02/data
     sudo tar -C /tmp -xzf /tmp/opensearch-cluster-node2-bundle.tar.gz
     sudo install -m 0640 /tmp/tls/*      /etc/personel/tls/
     sudo install -m 0644 /tmp/tls/tenant_ca.crt /etc/personel/tls/

     mkdir -p ~/personel-opensearch-node2
     cp /tmp/compose/* ~/personel-opensearch-node2/
     cd ~/personel-opensearch-node2

     # .env: OPENSEARCH_ADMIN_PASSWORD must match vm3's
     cat > .env <<EOF
OPENSEARCH_ADMIN_PASSWORD=<same-as-vm3>
EOF

     docker compose -f docker-compose.cluster-node2.yaml up -d

5. On either vm3 or vm5 — wait for cluster yellow:
     curl -sku admin:\$OPENSEARCH_ADMIN_PASSWORD \\
       https://127.0.0.1:9200/_cluster/health?wait_for_status=yellow\&timeout=60s

6. Patch existing indices to replicas=1 so HA copies spread across nodes:
     ${SCRIPT_DIR}/opensearch-cluster-reindex.sh

7. Run cross-node validation:
     ${SCRIPT_DIR}/opensearch-cluster-test.sh

================================================================================
RUNBOOK
}

# -----------------------------------------------------------------------------
# Main
# -----------------------------------------------------------------------------
main() {
  preflight || exit $?

  log "issuing node certs from Vault PKI (${PKI_MOUNT}/issue/${PKI_ROLE})"
  issue_node_cert "opensearch-01" "${VM3_IP}"
  issue_node_cert "opensearch-02" "${VM5_IP}"

  stage_vm5_bundle

  print_runbook

  log "bootstrap complete — see runbook above"
}

main "$@"
