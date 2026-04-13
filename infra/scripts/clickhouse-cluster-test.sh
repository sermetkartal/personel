#!/usr/bin/env bash
# =============================================================================
# Personel Platform — ClickHouse Cluster Replication Validation
# Phase 5 Wave 2, Roadmap #44
# =============================================================================
#
# Run AFTER clickhouse-cluster-bootstrap.sh and clickhouse-cluster-migrate-schemas.sh.
# Exercises the live cluster:
#   1. Creates a small ReplicatedMergeTree test table ON CLUSTER
#   2. Inserts one row with a unique marker on clickhouse-01
#   3. Polls clickhouse-02 until the row appears (max 30 s)
#   4. Reports replication lag and any system.replication_queue entries
#   5. Drops the test table ON CLUSTER
#   6. Exits 0 (pass) or 1 (fail)
#
# Usage:
#   ./clickhouse-cluster-test.sh
#
# Env overrides:
#   VM3_IP                defaults 192.168.5.44
#   VM5_IP                defaults 192.168.5.32
#   CH_CLUSTER_PASSWORD   required (or CLICKHOUSE_PASSWORD)
# =============================================================================
set -euo pipefail
IFS=$'\n\t'

VM3_IP="${VM3_IP:-192.168.5.44}"
VM5_IP="${VM5_IP:-192.168.5.32}"
CH_USER="${CLICKHOUSE_USER:-personel_app}"
CH_PASSWORD="${CH_CLUSTER_PASSWORD:-${CLICKHOUSE_PASSWORD:-}}"
DATABASE="${CLICKHOUSE_DATABASE:-personel}"
TEST_TABLE="__cluster_test_$(date +%s)"
MARKER="marker-$(openssl rand -hex 8)"
MAX_WAIT_SECONDS="${MAX_WAIT_SECONDS:-30}"

log()   { printf '[cluster-test] %s\n' "$*"; }
pass()  { printf '\033[0;32m[PASS]\033[0m %s\n' "$*"; }
fail()  { printf '\033[0;31m[FAIL]\033[0m %s\n' "$*" >&2; FAIL_COUNT=$((FAIL_COUNT + 1)); }
fatal() { printf '\033[0;31m[FATAL]\033[0m %s\n' "$*" >&2; exit 1; }

FAIL_COUNT=0

if [[ -z "${CH_PASSWORD}" ]]; then
    fatal "CH_CLUSTER_PASSWORD (or CLICKHOUSE_PASSWORD) not set"
fi

# Query helpers: prefer docker exec on vm3 for clickhouse-01, ssh for clickhouse-02.
q_ch01() {
    local query="$1"
    docker exec -i personel-clickhouse-01 clickhouse-client \
        --user "${CH_USER}" --password "${CH_PASSWORD}" \
        --database "${DATABASE}" --query="${query}"
}

q_ch02() {
    local query="$1"
    ssh -o BatchMode=yes "kartal@${VM5_IP}" \
        "docker exec -i personel-clickhouse-02 clickhouse-client \
            --user ${CH_USER} --password '${CH_PASSWORD}' \
            --database ${DATABASE} --query=\"${query}\""
}

cleanup() {
    local rc=$?
    log "cleanup: dropping test table on cluster"
    q_ch01 "DROP TABLE IF EXISTS ${TEST_TABLE} ON CLUSTER personel_cluster SYNC" >/dev/null 2>&1 || true
    exit "${rc}"
}
trap cleanup EXIT

log "=== Personel CH Cluster Replication Test ==="
log "vm3=${VM3_IP}  vm5=${VM5_IP}  database=${DATABASE}"
log "test_table=${TEST_TABLE}  marker=${MARKER}"
echo ""

# ---------------------------------------------------------------------------
# Step 1: Pre-checks — both nodes respond, cluster has 2 replicas
# ---------------------------------------------------------------------------
log "--- Step 1: Pre-checks ---"

if ! q_ch01 "SELECT 1" >/dev/null 2>&1; then
    fatal "clickhouse-01 query failed — check that the container is up on vm3"
fi
pass "clickhouse-01 responding"

if ! q_ch02 "SELECT 1" >/dev/null 2>&1; then
    fatal "clickhouse-02 query failed — check that the container is up on vm5"
fi
pass "clickhouse-02 responding"

replica_count=$(q_ch01 "SELECT count() FROM system.clusters WHERE cluster = 'personel_cluster'")
if [[ "${replica_count}" -lt 2 ]]; then
    fail "personel_cluster reports ${replica_count} replicas (expected 2)"
else
    pass "personel_cluster has ${replica_count} replicas"
fi

keeper_status=$(q_ch01 "SELECT count() FROM system.zookeeper WHERE path = '/'" 2>/dev/null || echo 0)
if [[ "${keeper_status}" -eq 0 ]]; then
    fail "clickhouse-01 cannot reach keeper quorum (system.zookeeper empty)"
else
    pass "clickhouse-01 sees keeper (${keeper_status} root znodes)"
fi

# ---------------------------------------------------------------------------
# Step 2: Create a ReplicatedMergeTree test table ON CLUSTER
# ---------------------------------------------------------------------------
log "--- Step 2: Create ReplicatedMergeTree test table ON CLUSTER ---"

if ! q_ch01 "
CREATE TABLE ${TEST_TABLE} ON CLUSTER personel_cluster (
    event_id UUID DEFAULT generateUUIDv4(),
    marker   String,
    ts       DateTime64(3) DEFAULT now64()
) ENGINE = ReplicatedMergeTree('/clickhouse/tables/{shard}/${TEST_TABLE}', '{replica}')
ORDER BY (ts, marker)
TTL toDateTime(ts) + INTERVAL 1 HOUR
"; then
    fatal "CREATE TABLE ON CLUSTER failed"
fi
pass "test table created on both replicas"

# ---------------------------------------------------------------------------
# Step 3: Insert marker row on clickhouse-01, poll clickhouse-02 for it
# ---------------------------------------------------------------------------
log "--- Step 3: Insert on ch-01, poll ch-02 ---"

start_ns=$(date +%s%N)
q_ch01 "INSERT INTO ${TEST_TABLE} (marker) VALUES ('${MARKER}')"
insert_done_ns=$(date +%s%N)
insert_elapsed_ms=$(( (insert_done_ns - start_ns) / 1000000 ))
pass "inserted marker (took ${insert_elapsed_ms} ms)"

log "polling clickhouse-02 for replicated row (up to ${MAX_WAIT_SECONDS} s)..."
replicated=false
for i in $(seq 1 "${MAX_WAIT_SECONDS}"); do
    count=$(q_ch02 "SELECT count() FROM ${TEST_TABLE} WHERE marker = '${MARKER}'" 2>/dev/null || echo 0)
    if [[ "${count}" -ge 1 ]]; then
        replicated=true
        replication_elapsed_ms=$(( ( $(date +%s%N) - insert_done_ns ) / 1000000 ))
        pass "row visible on clickhouse-02 after ${replication_elapsed_ms} ms (${i} s polling)"
        break
    fi
    sleep 1
done

if [[ "${replicated}" == "false" ]]; then
    fail "row did not replicate to clickhouse-02 within ${MAX_WAIT_SECONDS} s"
fi

# ---------------------------------------------------------------------------
# Step 4: Report on replication_queue + is_readonly
# ---------------------------------------------------------------------------
log "--- Step 4: Replication queue health ---"

queue_size=$(q_ch01 "SELECT count() FROM system.replication_queue WHERE database = '${DATABASE}'" || echo 0)
if [[ "${queue_size}" -gt 0 ]]; then
    fail "ch-01 replication_queue has ${queue_size} pending entries"
    q_ch01 "SELECT table, type, num_tries, last_exception FROM system.replication_queue WHERE database = '${DATABASE}' LIMIT 5 FORMAT Vertical"
else
    pass "ch-01 replication_queue empty"
fi

queue_size_2=$(q_ch02 "SELECT count() FROM system.replication_queue WHERE database = '${DATABASE}'" || echo 0)
if [[ "${queue_size_2}" -gt 0 ]]; then
    fail "ch-02 replication_queue has ${queue_size_2} pending entries"
else
    pass "ch-02 replication_queue empty"
fi

readonly_01=$(q_ch01 "SELECT any(is_readonly) FROM system.replicas WHERE database = '${DATABASE}'" || echo 1)
readonly_02=$(q_ch02 "SELECT any(is_readonly) FROM system.replicas WHERE database = '${DATABASE}'" || echo 1)
if [[ "${readonly_01}" == "0" ]]; then
    pass "ch-01 replicas is_readonly=0"
else
    fail "ch-01 replicas is_readonly=${readonly_01} — keeper connectivity issue"
fi
if [[ "${readonly_02}" == "0" ]]; then
    pass "ch-02 replicas is_readonly=0"
else
    fail "ch-02 replicas is_readonly=${readonly_02} — keeper connectivity issue"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "=== Cluster Test Summary ==="
printf '  failures: %d\n' "${FAIL_COUNT}"
if [[ "${FAIL_COUNT}" -eq 0 ]]; then
    printf '\033[0;32m=== CLUSTER TEST: PASS ===\033[0m\n'
    exit 0
fi
printf '\033[0;31m=== CLUSTER TEST: FAIL (%d) ===\033[0m\n' "${FAIL_COUNT}"
exit 1
