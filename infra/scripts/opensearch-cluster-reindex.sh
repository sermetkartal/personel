#!/usr/bin/env bash
# =============================================================================
# Personel Platform — OpenSearch Cluster Reindex / Replica Promotion
# =============================================================================
# Faz 5 Madde 51
#
# After the two-node cluster forms, existing indices that were created on the
# single-node deployment still carry number_of_replicas=0. This script walks
# every user-facing index and raises replicas to 1 so the second node receives
# copies. The cluster then re-shards into HA state (yellow → green).
#
# System indices (.opensearch_security, .opendistro_security, leading dot) are
# SKIPPED — OpenSearch manages their replication internally based on number
# of master-eligible nodes.
#
# Idempotent: indices already at >=1 replicas are left alone.
#
# Usage:
#   OPENSEARCH_ADMIN_PASSWORD=... ./opensearch-cluster-reindex.sh
#   OPENSEARCH_ADMIN_PASSWORD=... OS_URL=https://127.0.0.1:9200 ./opensearch-cluster-reindex.sh --dry-run
# =============================================================================
set -euo pipefail

OS_URL="${OS_URL:-https://127.0.0.1:9200}"
OS_USER="${OS_USER:-admin}"
OS_PASSWORD="${OPENSEARCH_ADMIN_PASSWORD:-}"

DRY_RUN=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=true; shift ;;
    -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
    *) printf 'unknown flag: %s\n' "$1" >&2; exit 1 ;;
  esac
done

log()  { printf '[os-reindex] %s\n' "$*"; }
warn() { printf '[os-reindex] WARN: %s\n' "$*" >&2; }
err()  { printf '[os-reindex] ERROR: %s\n' "$*" >&2; }

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

curl_os() {
  curl -sk --fail -u "${OS_USER}:${OS_PASSWORD}" "$@"
}

log "fetching cluster health"
if ! health=$(curl_os "${OS_URL}/_cluster/health"); then
  err "cannot reach ${OS_URL} — cluster may still be forming"
  exit 2
fi
node_count=$(echo "${health}" | jq -r '.number_of_nodes')
status=$(echo "${health}"     | jq -r '.status')
log "cluster status=${status} nodes=${node_count}"

if [[ "${node_count}" -lt 2 ]]; then
  err "cluster only has ${node_count} node(s); refusing to increase replicas."
  err "Both nodes must be UP before running reindex."
  exit 2
fi

log "listing indices"
indices=$(curl_os "${OS_URL}/_cat/indices?h=index&format=json" | jq -r '.[].index' | sort)

promoted=0
skipped=0
failed=0

while IFS= read -r idx; do
  # Skip system and hidden indices
  if [[ "${idx}" == .* ]]; then
    skipped=$((skipped + 1))
    continue
  fi

  current=$(curl_os "${OS_URL}/${idx}/_settings" | jq -r ".[\"${idx}\"].settings.index.number_of_replicas")
  if [[ "${current}" -ge 1 ]]; then
    log "  SKIP ${idx} — already replicas=${current}"
    skipped=$((skipped + 1))
    continue
  fi

  if [[ "${DRY_RUN}" == "true" ]]; then
    log "  DRY-RUN ${idx}: would set replicas=1"
    continue
  fi

  log "  PATCH ${idx}: replicas 0 → 1"
  if curl_os -X PUT \
       -H 'Content-Type: application/json' \
       -d '{"index":{"number_of_replicas":1}}' \
       "${OS_URL}/${idx}/_settings" >/dev/null; then
    promoted=$((promoted + 1))
  else
    err "  FAIL ${idx}"
    failed=$((failed + 1))
  fi
done <<< "${indices}"

log "waiting for replica shards to place (max 120s)"
if ! curl_os "${OS_URL}/_cluster/health?wait_for_status=green&timeout=120s" | jq -e '.status == "green"' >/dev/null; then
  warn "cluster did not reach green within 120s — this is OK if indices are large;"
  warn "check progress with: GET /_cluster/health"
fi

log "done — promoted=${promoted} skipped=${skipped} failed=${failed}"

if [[ "${failed}" -gt 0 ]]; then
  exit 2
fi
exit 0
