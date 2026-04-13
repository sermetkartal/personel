#!/usr/bin/env bash
# =============================================================================
# Personel Platform — NATS JetStream Cluster Validation (Faz 5 #46)
#
# End-to-end pub/sub validation across the 2-node cluster:
#
#   1. Verifies cluster reports 2 active peers
#   2. Reports leader + followers + lag for the events_raw stream
#   3. Connects directly to node-01, publishes a known synthetic message to
#      events.raw.cluster_test.<ts>
#   4. Connects directly to node-02 and asserts the message arrives within
#      a 5s window (allows for Raft commit + replication propagation)
#   5. Exits 0 on success, 1 on any failure
#
# This test uses the api-controlplane creds for the publish path. The
# subject events.raw.cluster_test.* is permitted by gateway-publisher,
# but the test deliberately uses a client connection to make absolutely
# sure inter-node propagation works for the production subject namespace.
# We use api creds (which can sub agent.health.> + live_view.>) for the
# subscribe leg, so we publish to live_view.cluster_test.<ts> instead —
# which both api can publish AND api can subscribe — keeping the test
# self-contained against existing creds without requiring new permissions.
#
# NOTE: events.raw.* is publish-only for gateway-publisher and read-only
# for enricher-consumer. Neither single creds file can do both pub+sub on
# events.raw.*. live_view.> is the only subject api-controlplane can both
# publish AND subscribe to, so we use it for the round-trip test.
#
# Run on vm3.
# =============================================================================
set -euo pipefail

SCRIPT_NAME="nats-cluster-test"

NATS_NODE1="${NATS_NODE1:-tls://192.168.5.44:4222}"
NATS_NODE2="${NATS_NODE2:-tls://192.168.5.32:4222}"
NATS_CREDS="${NATS_CREDS:-/etc/personel/nats-creds/api.creds}"
NATS_CA="${NATS_CA:-/etc/personel/tls/root_ca.crt}"

TEST_SUBJECT_PREFIX="live_view.cluster_test"
WAIT_SECONDS=5

log()  { printf '[%s] %s\n' "${SCRIPT_NAME}" "$*"; }
warn() { printf '[%s] WARN: %s\n' "${SCRIPT_NAME}" "$*" >&2; }
fail() { printf '[%s] FAIL: %s\n' "${SCRIPT_NAME}" "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Prerequisites
# ---------------------------------------------------------------------------
command -v nats    >/dev/null 2>&1 || fail "nats CLI not installed"
command -v jq      >/dev/null 2>&1 || fail "jq required"
command -v mktemp  >/dev/null 2>&1 || fail "mktemp required"
[[ -f "${NATS_CREDS}" ]] || fail "creds file missing: ${NATS_CREDS}"
[[ -f "${NATS_CA}" ]] || fail "CA bundle missing: ${NATS_CA}"

NATS_FLAGS_NODE1=(--server "${NATS_NODE1}" --creds "${NATS_CREDS}" --tlsca "${NATS_CA}")
NATS_FLAGS_NODE2=(--server "${NATS_NODE2}" --creds "${NATS_CREDS}" --tlsca "${NATS_CA}")

# ---------------------------------------------------------------------------
# Step 1 + 2: cluster topology + leader / followers / lag
# ---------------------------------------------------------------------------
log "Step 1: cluster peer topology"
peer_json="$(nats "${NATS_FLAGS_NODE1[@]}" server list --json 2>/dev/null || echo '[]')"
peer_count="$(jq -r 'length' <<<"${peer_json}")"
if [[ "${peer_count}" -lt 2 ]]; then
  fail "expected 2 peers, got ${peer_count}"
fi
log "  cluster has ${peer_count} active peers"
jq -r '.[] | "    " + (.name // "?") + " " + (.cluster.cluster_size // 1 | tostring) + " peers, conn=" + (.conn // 0 | tostring)' <<<"${peer_json}" || true

log "Step 2: events_raw stream leader / followers / lag (if stream exists)"
if nats "${NATS_FLAGS_NODE1[@]}" stream info events_raw --json >/dev/null 2>&1; then
  stream_info="$(nats "${NATS_FLAGS_NODE1[@]}" stream info events_raw --json)"
  leader="$(jq -r '.cluster.leader // "n/a"' <<<"${stream_info}")"
  log "  events_raw leader: ${leader}"
  jq -r '.cluster.replicas[]? | "    follower=" + .name + " current=" + (.current|tostring) + " active_lag=" + (.active|tostring) + "ns"' <<<"${stream_info}" || true
else
  warn "  events_raw stream does not exist — skipping leader inspection"
fi

# ---------------------------------------------------------------------------
# Step 3-4: pub on node-01, sub on node-02
# ---------------------------------------------------------------------------
TS="$(date -u +%Y%m%dT%H%M%S%NZ)"
SUBJECT="${TEST_SUBJECT_PREFIX}.${TS}"
PAYLOAD="cluster-test-${TS}"

log "Step 3-4: round-trip pub on node-01, sub on node-02"
log "  subject: ${SUBJECT}"

# Subscribe on node-02 in background, write first message to a temp file
SUB_OUT="$(mktemp)"
trap 'rm -f "${SUB_OUT}"' EXIT

# nats sub --count 1 exits as soon as it receives the first message
( timeout "${WAIT_SECONDS}" nats "${NATS_FLAGS_NODE2[@]}" sub "${SUBJECT}" --count 1 --raw > "${SUB_OUT}" 2>/dev/null ) &
SUB_PID=$!

# Tiny grace so the subscriber is registered before publish
sleep 1

# Publish on node-01
if ! nats "${NATS_FLAGS_NODE1[@]}" pub "${SUBJECT}" "${PAYLOAD}" >/dev/null 2>&1; then
  kill "${SUB_PID}" >/dev/null 2>&1 || true
  fail "publish to node-01 failed (subject=${SUBJECT})"
fi
log "  published to node-01"

# Wait for the subscriber to finish (it will exit when it gets the message
# or when the timeout expires)
if ! wait "${SUB_PID}"; then
  fail "subscriber on node-02 did not receive message within ${WAIT_SECONDS}s"
fi

received="$(cat "${SUB_OUT}" || true)"
if [[ "${received}" != "${PAYLOAD}" ]]; then
  fail "subscriber received unexpected payload: '${received}' (expected '${PAYLOAD}')"
fi
log "  node-02 received the message and payload matches"

# ---------------------------------------------------------------------------
# Done
# ---------------------------------------------------------------------------
log ""
log "PASS: 2-node NATS cluster pub-on-1 / sub-on-2 round trip OK"
exit 0
