#!/usr/bin/env bash
# Personel Platform - ClickHouse cluster dev migration (vm3 + vm5)
# Converts 6 MergeTree tables in 'personel' database to ReplicatedMergeTree.
# Run on vm3 (primary writer) first, then vm5 attaches as replica via ZK.
#
# Invocation:
#   NODE=1 ./clickhouse-cluster-migrate-dev.sh    # on vm3
#   NODE=2 ./clickhouse-cluster-migrate-dev.sh    # on vm5 (creates table only)
#
# On NODE=1: for each existing MergeTree table
#   1. RENAME table TO table_old
#   2. CREATE TABLE table ENGINE=ReplicatedMergeTree(zk_path, '{replica}')
#   3. INSERT INTO table SELECT * FROM table_old
#   4. DROP TABLE table_old
#
# On NODE=2: just CREATE TABLE (ReplicatedMergeTree will attach from ZK and
# backfill via interserver fetch from node-1).

set -euo pipefail

NODE=${NODE:-1}
if [[ "$NODE" == "1" ]]; then
    CH_CONTAINER=personel-clickhouse
    CH_PORT=9000
elif [[ "$NODE" == "2" ]]; then
    CH_CONTAINER=personel-clickhouse-02
    CH_PORT=19000
else
    echo "NODE must be 1 or 2" >&2
    exit 1
fi

exec_ch() {
    docker exec "$CH_CONTAINER" clickhouse-client --port "$CH_PORT" --query "$1"
}

# Schema catalogue (table name, replicated DDL body, old-table engine clause replaced)
declare -A TABLES
TABLES[agent_heartbeats]="(tenant_id UUID, endpoint_id UUID, occurred_at DateTime64(9,'UTC'), received_at DateTime64(9,'UTC'), cpu_percent Float32, rss_bytes UInt64, queue_depth UInt64, blob_queue_depth UInt64, drops_since_last UInt64, policy_version String, legal_hold Bool DEFAULT false)
ENGINE = ReplicatedMergeTree('/clickhouse/tables/{shard}/agent_heartbeats','{replica}')
PARTITION BY toYYYYMM(occurred_at)
ORDER BY (tenant_id, endpoint_id, occurred_at)
TTL toDateTime(occurred_at) + toIntervalDay(30) WHERE legal_hold = false
SETTINGS index_granularity = 8192"

TABLES[events_raw]="(tenant_id UUID, endpoint_id UUID, occurred_at DateTime64(9,'UTC'), event_id String, event_type LowCardinality(String), schema_version UInt8 DEFAULT 1, user_sid String, agent_version_major UInt8, agent_version_minor UInt8, agent_version_patch UInt8, seq UInt64, pii_class LowCardinality(String), retention_class LowCardinality(String), received_at DateTime64(9,'UTC'), payload String, sensitive Bool DEFAULT false, legal_hold Bool DEFAULT false, batch_id UInt64, batch_hmac String)
ENGINE = ReplicatedMergeTree('/clickhouse/tables/{shard}/events_raw','{replica}')
PARTITION BY toYYYYMM(occurred_at)
ORDER BY (tenant_id, endpoint_id, occurred_at, event_type)
TTL toDateTime(occurred_at) + toIntervalDay(90) WHERE legal_hold = false
SETTINGS index_granularity = 8192"

for sens in clipboard_meta file keystroke_meta window; do
    TABLES[events_sensitive_${sens}]="(tenant_id UUID, endpoint_id UUID, occurred_at DateTime64(9,'UTC'), event_id String, user_sid String, seq UInt64, received_at DateTime64(9,'UTC'), payload String, legal_hold Bool DEFAULT false)
ENGINE = ReplicatedMergeTree('/clickhouse/tables/{shard}/events_sensitive_${sens}','{replica}')
PARTITION BY toYYYYMM(occurred_at)
ORDER BY (tenant_id, endpoint_id, occurred_at)
TTL toDateTime(occurred_at) + toIntervalDay(15) WHERE legal_hold = false
SETTINGS index_granularity = 8192"
done

# Ensure database exists (for vm5)
exec_ch "CREATE DATABASE IF NOT EXISTS personel"

for table in agent_heartbeats events_raw events_sensitive_clipboard_meta events_sensitive_file events_sensitive_keystroke_meta events_sensitive_window; do
    body="${TABLES[$table]}"
    if [[ "$NODE" == "1" ]]; then
        echo "[node1] Migrating $table..."
        engine=$(exec_ch "SELECT engine FROM system.tables WHERE database='personel' AND name='${table}'" || echo "")
        if [[ "$engine" == "ReplicatedMergeTree" ]]; then
            echo "  already ReplicatedMergeTree, skipping"
            continue
        fi
        if [[ -z "$engine" ]]; then
            echo "  table missing, creating fresh"
            exec_ch "CREATE TABLE personel.${table} ${body}"
            continue
        fi
        count=$(exec_ch "SELECT count() FROM personel.${table}" 2>/dev/null || echo 0)
        echo "  old engine=$engine rows=$count"
        exec_ch "RENAME TABLE personel.${table} TO personel.${table}_old"
        exec_ch "CREATE TABLE personel.${table} ${body}"
        if [[ "$count" != "0" ]]; then
            exec_ch "INSERT INTO personel.${table} SELECT * FROM personel.${table}_old"
        fi
        exec_ch "DROP TABLE personel.${table}_old SYNC"
        echo "  done"
    else
        echo "[node2] Creating $table replica..."
        exists=$(exec_ch "SELECT count() FROM system.tables WHERE database='personel' AND name='${table}'")
        if [[ "$exists" == "1" ]]; then
            echo "  already exists"
            continue
        fi
        exec_ch "CREATE TABLE personel.${table} ${body}"
        echo "  created"
    fi
done

echo "[node$NODE] Migration complete."
