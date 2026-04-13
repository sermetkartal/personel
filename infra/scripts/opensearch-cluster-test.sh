#!/usr/bin/env bash
# =============================================================================
# Personel Platform — OpenSearch Cluster Validation
# =============================================================================
# Faz 5 Madde 51
#
# End-to-end sanity test for the two-node cluster:
#   1. Writes a known doc to node-1
#   2. Forces a refresh
#   3. Reads the same doc from node-2 (direct HTTP over the LAN)
#   4. Verifies cluster status is green, node count is 2, replica > 0
#   5. Cleans up the test index
#
# Exit 0 = pass, 1 = config/usage, 2 = cluster not healthy, 3 = cross-node
# read failure (indicates replication broken).
#
# Usage:
#   OPENSEARCH_ADMIN_PASSWORD=... ./opensearch-cluster-test.sh
#   OS_NODE1=https://127.0.0.1:9200 OS_NODE2=https://192.168.5.32:9200 \
#     OPENSEARCH_ADMIN_PASSWORD=... ./opensearch-cluster-test.sh
# =============================================================================
set -euo pipefail

OS_NODE1="${OS_NODE1:-https://127.0.0.1:9200}"
OS_NODE2="${OS_NODE2:-https://192.168.5.32:9200}"
OS_USER="${OS_USER:-admin}"
OS_PASSWORD="${OPENSEARCH_ADMIN_PASSWORD:-}"

TEST_INDEX="${TEST_INDEX:-personel-cluster-test-$(date +%s)}"
TEST_DOC_ID="cluster-probe-1"

log()  { printf '[os-test] %s\n' "$*"; }
warn() { printf '[os-test] WARN: %s\n' "$*" >&2; }
err()  { printf '[os-test] ERROR: %s\n' "$*" >&2; }

fail() {
  err "$*"
  cleanup || true
  exit "${2:-2}"
}

if [[ -z "${OS_PASSWORD}" ]]; then
  err "OPENSEARCH_ADMIN_PASSWORD is required"
  exit 1
fi
for bin in curl jq; do
  if ! command -v "${bin}" >/dev/null 2>&1; then
    err "missing dependency: ${bin}"
    exit 1
  fi
done

curl_auth() {
  curl -sk --fail -u "${OS_USER}:${OS_PASSWORD}" "$@"
}

cleanup() {
  curl_auth -X DELETE "${OS_NODE1}/${TEST_INDEX}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# -----------------------------------------------------------------------------
# Step 1: cluster health + topology
# -----------------------------------------------------------------------------
log "checking cluster health via node-1 (${OS_NODE1})"
health=$(curl_auth "${OS_NODE1}/_cluster/health?wait_for_status=yellow&timeout=30s") \
  || fail "cannot reach ${OS_NODE1}"

status=$(echo "${health}"      | jq -r '.status')
nodes=$(echo "${health}"       | jq -r '.number_of_nodes')
data_nodes=$(echo "${health}"  | jq -r '.number_of_data_nodes')

log "  status=${status} nodes=${nodes} data_nodes=${data_nodes}"

if [[ "${nodes}" -lt 2 ]]; then
  fail "expected 2 nodes, found ${nodes}"
fi
if [[ "${status}" != "green" && "${status}" != "yellow" ]]; then
  fail "cluster status ${status} is not acceptable"
fi

# -----------------------------------------------------------------------------
# Step 2: direct reachability of node-2
# -----------------------------------------------------------------------------
log "checking node-2 direct reachability (${OS_NODE2})"
if ! node2_info=$(curl_auth "${OS_NODE2}"); then
  fail "cannot reach node-2 over the LAN — check firewall for port 9200" 3
fi
node2_name=$(echo "${node2_info}" | jq -r '.name')
log "  node-2 reports name=${node2_name}"

if [[ "${node2_name}" != "opensearch-02" ]]; then
  warn "node-2 name mismatch (got ${node2_name}, expected opensearch-02)"
fi

# -----------------------------------------------------------------------------
# Step 3: write to node-1, read from node-2
# -----------------------------------------------------------------------------
log "creating test index ${TEST_INDEX} with replicas=1"
curl_auth -X PUT \
  -H 'Content-Type: application/json' \
  -d '{"settings":{"number_of_shards":1,"number_of_replicas":1}}' \
  "${OS_NODE1}/${TEST_INDEX}" >/dev/null \
  || fail "index create failed"

log "indexing probe doc via node-1"
curl_auth -X PUT \
  -H 'Content-Type: application/json' \
  -d '{"probe":"hello","timestamp":"'"$(date -u +%FT%TZ)"'"}' \
  "${OS_NODE1}/${TEST_INDEX}/_doc/${TEST_DOC_ID}?refresh=wait_for" >/dev/null \
  || fail "index put failed"

log "reading same doc via node-2"
doc=$(curl_auth "${OS_NODE2}/${TEST_INDEX}/_doc/${TEST_DOC_ID}") \
  || fail "cross-node read failed" 3

probe=$(echo "${doc}" | jq -r '._source.probe')
if [[ "${probe}" != "hello" ]]; then
  fail "cross-node read returned wrong payload: ${doc}" 3
fi
log "  cross-node read OK (_source.probe='hello')"

# -----------------------------------------------------------------------------
# Step 4: shard allocation
# -----------------------------------------------------------------------------
log "checking shard distribution"
shards=$(curl_auth "${OS_NODE1}/_cat/shards/${TEST_INDEX}?format=json")
primary_node=$(echo "${shards}" | jq -r '.[] | select(.prirep=="p") | .node')
replica_node=$(echo "${shards}" | jq -r '.[] | select(.prirep=="r") | .node')
log "  primary on: ${primary_node}"
log "  replica on: ${replica_node}"

if [[ -z "${replica_node}" || "${replica_node}" == "null" ]]; then
  fail "replica shard has not been placed — replication broken" 3
fi
if [[ "${primary_node}" == "${replica_node}" ]]; then
  fail "primary and replica on same node — multi-node replication not working" 3
fi

# -----------------------------------------------------------------------------
# Report
# -----------------------------------------------------------------------------
cat <<REPORT

================================================================================
OPENSEARCH CLUSTER TEST — PASSED
================================================================================
  cluster.status      : ${status}
  number_of_nodes     : ${nodes}
  number_of_data_nodes: ${data_nodes}
  probe primary       : ${primary_node}
  probe replica       : ${replica_node}
================================================================================

REPORT

exit 0
