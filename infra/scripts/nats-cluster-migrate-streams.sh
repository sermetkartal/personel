#!/usr/bin/env bash
# =============================================================================
# Personel Platform — NATS JetStream Stream Replica Migration (Faz 5 #46)
#
# After cluster formation (nats-cluster-bootstrap + node-01 + node-02 are
# all running), JetStream streams created in the single-node era still
# have replicas=1 — they exist on only one node. This script bumps each
# Personel stream to replicas=2 so that loss of one node leaves the data
# intact on the other.
#
# Streams managed:
#   events_raw          — high-volume raw event ingest (gateway -> enricher)
#   events_sensitive    — sensitive event subset (gateway -> enricher)
#   live_view_control   — HR-gated live view command bus
#   agent_health        — heartbeat (Flow 7 employee-disable detection)
#   pki_events          — PKI lifecycle (cert issue / revoke / rotate)
#
# Idempotent: streams already at replicas=2 are skipped. Streams not yet
# created are skipped with a warning (the gateway provisions them on
# first publish).
#
# Prerequisites:
#   - Both NATS nodes are up and have formed a cluster
#   - nats CLI installed
#   - /etc/personel/nats-creds/api.creds exists (used to authenticate)
#
# Run on vm3.
# =============================================================================
set -euo pipefail

SCRIPT_NAME="nats-cluster-migrate-streams"

NATS_URL="${NATS_URL:-tls://192.168.5.44:4222,tls://192.168.5.32:4222}"
NATS_CREDS="${NATS_CREDS:-/etc/personel/nats-creds/api.creds}"
NATS_CA="${NATS_CA:-/etc/personel/tls/root_ca.crt}"

STREAMS=(
  "events_raw"
  "events_sensitive"
  "live_view_control"
  "agent_health"
  "pki_events"
)

DESIRED_REPLICAS=2

log()  { printf '[%s] %s\n' "${SCRIPT_NAME}" "$*"; }
warn() { printf '[%s] WARN: %s\n' "${SCRIPT_NAME}" "$*" >&2; }
die()  { printf '[%s] ERROR: %s\n' "${SCRIPT_NAME}" "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Prerequisites
# ---------------------------------------------------------------------------
command -v nats >/dev/null 2>&1 || die "nats CLI not installed (https://github.com/nats-io/natscli)"
command -v jq   >/dev/null 2>&1 || die "jq required"
[[ -f "${NATS_CREDS}" ]] || die "creds file missing: ${NATS_CREDS}"
[[ -f "${NATS_CA}" ]] || die "CA bundle missing: ${NATS_CA}"

NATS_FLAGS=(
  --server "${NATS_URL}"
  --creds  "${NATS_CREDS}"
  --tlsca  "${NATS_CA}"
)

# ---------------------------------------------------------------------------
# Verify cluster has both peers before mutating anything
# ---------------------------------------------------------------------------
log "Checking cluster peer count..."
peer_json="$(nats "${NATS_FLAGS[@]}" server list --json 2>/dev/null || true)"
peer_count="$(jq -r 'length' <<<"${peer_json}" 2>/dev/null || echo 0)"
if [[ "${peer_count}" -lt 2 ]]; then
  die "Cluster reports ${peer_count} peer(s); expected 2. Verify node-02 is up before migrating streams."
fi
log "  ${peer_count} peers active — proceeding"

# ---------------------------------------------------------------------------
# Migrate one stream
# ---------------------------------------------------------------------------
migrate_stream() {
  local stream="$1"

  if ! nats "${NATS_FLAGS[@]}" stream info "${stream}" --json >/dev/null 2>&1; then
    warn "stream '${stream}' does not exist yet — skipping (gateway will create it on first publish)"
    return 0
  fi

  local current_replicas
  current_replicas="$(nats "${NATS_FLAGS[@]}" stream info "${stream}" --json \
    | jq -r '.config.num_replicas // 1')"

  if [[ "${current_replicas}" == "${DESIRED_REPLICAS}" ]]; then
    log "  ${stream}: already at replicas=${DESIRED_REPLICAS} — skipping"
    return 0
  fi

  log "  ${stream}: replicas=${current_replicas} -> ${DESIRED_REPLICAS}"
  nats "${NATS_FLAGS[@]}" stream edit "${stream}" \
    --replicas "${DESIRED_REPLICAS}" \
    --force \
    >/dev/null

  # Verify after edit
  local new_replicas
  new_replicas="$(nats "${NATS_FLAGS[@]}" stream info "${stream}" --json \
    | jq -r '.config.num_replicas')"
  if [[ "${new_replicas}" != "${DESIRED_REPLICAS}" ]]; then
    die "${stream}: edit applied but replicas=${new_replicas} (expected ${DESIRED_REPLICAS})"
  fi

  # Wait for the second peer to catch up
  log "    waiting for second replica to become current..."
  for i in $(seq 1 30); do
    local cluster_info
    cluster_info="$(nats "${NATS_FLAGS[@]}" stream info "${stream}" --json | jq -c '.cluster // {}')"
    local replica_count
    replica_count="$(jq -r '.replicas | length // 0' <<<"${cluster_info}")"
    local current_count
    current_count="$(jq -r '[.replicas[]? | select(.current==true)] | length' <<<"${cluster_info}")"
    if [[ "${replica_count}" -ge 1 ]] && [[ "${current_count}" -ge 1 ]]; then
      log "    ${stream}: ${current_count}/${replica_count} replicas current"
      return 0
    fi
    sleep 2
  done
  warn "    ${stream}: replicas not fully current after 60s — check 'nats stream info ${stream}'"
}

log "Migrating ${#STREAMS[@]} streams to replicas=${DESIRED_REPLICAS}..."
for s in "${STREAMS[@]}"; do
  migrate_stream "${s}"
done

log ""
log "Stream migration complete."
log ""
log "Verify with:"
log "  nats --server '${NATS_URL}' \\"
log "       --creds ${NATS_CREDS} \\"
log "       --tlsca ${NATS_CA} \\"
log "       stream list"
log ""
log "Each Personel stream should now report 'Replicas: 2' and the cluster"
log "section should list both personel-nats-01 and personel-nats-02."
