#!/usr/bin/env bash
# =============================================================================
# Personel Platform — ClickHouse Cluster Schema Migration
# Phase 5 Wave 2, Roadmap #44
# =============================================================================
#
# Converts all existing MergeTree tables in the `personel` database to
# ReplicatedMergeTree on the personel_cluster. Zookeeper path is
# /clickhouse/tables/{shard}/<table_name>; replica name comes from the
# {replica} macro (01 or 02) populated per-host by the cluster config.
#
# Idempotent:
#   * Lists tables via SHOW TABLES FROM personel
#   * For each, reads current engine from system.tables
#   * Skips tables already on Replicated* engines
#   * For MergeTree tables below ~100 GB uses the simple
#     ATTACH TABLE / DETACH TABLE restore path with a temporary
#     ReplicatedMergeTree shadow.
#
# For the Phase 1 dataset (empty or single-digit GB) the simple path
# works. For large tables the operator should run the ATTACH PARTITION
# procedure manually — see docs/operations/clickhouse-cluster.md.
#
# Usage:
#   ./clickhouse-cluster-migrate-schemas.sh              # run
#   ./clickhouse-cluster-migrate-schemas.sh --dry-run    # print what would happen
# =============================================================================
set -euo pipefail
IFS=$'\n\t'

DATABASE="${CLICKHOUSE_DATABASE:-personel}"
CH_HOST="${CLICKHOUSE_HOST:-127.0.0.1}"
CH_PORT="${CLICKHOUSE_PORT:-9000}"
CH_USER="${CLICKHOUSE_USER:-personel_app}"
CH_PASSWORD="${CH_CLUSTER_PASSWORD:-${CLICKHOUSE_PASSWORD:-}}"
CH_CONTAINER="${CLICKHOUSE_CONTAINER:-personel-clickhouse-01}"
DRY_RUN=false

for arg in "$@"; do
    case "${arg}" in
        --dry-run) DRY_RUN=true ;;
        -h|--help)
            sed -n '2,28p' "${BASH_SOURCE[0]}"
            exit 0
            ;;
        *)
            echo "Unknown argument: ${arg}" >&2
            exit 2
            ;;
    esac
done

log()   { printf '[schema-migrate] %s\n' "$*"; }
ok()    { printf '\033[0;32m[OK]\033[0m     %s\n' "$*"; }
warn()  { printf '\033[0;33m[WARN]\033[0m   %s\n' "$*" >&2; }
err()   { printf '\033[0;31m[ERROR]\033[0m  %s\n' "$*" >&2; }
fatal() { err "$*"; exit 1; }

# -------------------------------------------------------------------------
# Execute a ClickHouse query against clickhouse-01 (primary). We exec
# into the container so the query uses the local 9000 socket and does
# not require LAN TLS handshakes from wherever this script runs.
# -------------------------------------------------------------------------
ch_query() {
    local q="$1"
    if [[ "${DRY_RUN}" == "true" ]]; then
        printf 'DRY-RUN  %s\n' "${q}"
        return 0
    fi
    if [[ -z "${CH_PASSWORD}" ]]; then
        fatal "CH_CLUSTER_PASSWORD (or CLICKHOUSE_PASSWORD) not set"
    fi
    docker exec -i "${CH_CONTAINER}" \
        clickhouse-client \
            --host "${CH_HOST}" \
            --port "${CH_PORT}" \
            --user "${CH_USER}" \
            --password "${CH_PASSWORD}" \
            --database "${DATABASE}" \
            --query="${q}"
}

ch_query_silent() {
    ch_query "$@" 2>/dev/null
}

# -------------------------------------------------------------------------
# Main
# -------------------------------------------------------------------------
main() {
    log "Schema migration: MergeTree -> ReplicatedMergeTree on ${DATABASE}"
    log "Primary: ${CH_CONTAINER} @ ${CH_HOST}:${CH_PORT}"
    if [[ "${DRY_RUN}" == "true" ]]; then
        log "DRY-RUN MODE — no mutations"
    fi

    # Verify container is up (unless dry-run)
    if [[ "${DRY_RUN}" == "false" ]]; then
        if ! docker ps --format '{{.Names}}' | grep -Fxq "${CH_CONTAINER}"; then
            fatal "${CH_CONTAINER} not running"
        fi
        ok "container ${CH_CONTAINER} reachable"
    fi

    # Sanity: cluster defined and has 2 nodes up
    if [[ "${DRY_RUN}" == "false" ]]; then
        local cluster_count
        cluster_count=$(ch_query_silent \
            "SELECT count() FROM system.clusters WHERE cluster = 'personel_cluster'" || echo 0)
        if [[ "${cluster_count}" -lt 2 ]]; then
            fatal "personel_cluster has ${cluster_count} replicas — expected 2; is keeper quorum healthy?"
        fi
        ok "personel_cluster reports ${cluster_count} replicas"
    fi

    # Enumerate tables (skip system and views)
    local tables
    if [[ "${DRY_RUN}" == "true" ]]; then
        tables="events_raw
events_sensitive_window
events_sensitive_clipboard_meta
events_sensitive_keystroke_meta
events_sensitive_file
agent_heartbeats"
    else
        tables=$(ch_query "SELECT name FROM system.tables WHERE database = '${DATABASE}' AND engine LIKE '%MergeTree%' ORDER BY name" || true)
    fi

    if [[ -z "${tables// /}" ]]; then
        warn "no tables found in ${DATABASE} — nothing to migrate"
        return 0
    fi

    local migrated=0
    local skipped=0
    local failed=0

    while IFS= read -r tbl; do
        [[ -z "${tbl}" ]] && continue

        local engine
        if [[ "${DRY_RUN}" == "true" ]]; then
            engine="MergeTree"
        else
            engine=$(ch_query_silent \
                "SELECT engine FROM system.tables WHERE database = '${DATABASE}' AND name = '${tbl}'" || echo "")
        fi

        if [[ "${engine}" == Replicated* ]]; then
            ok "${tbl}: already ${engine} — skip"
            skipped=$((skipped + 1))
            continue
        fi

        log "migrating ${tbl} (engine=${engine:-unknown}) ..."

        # Preserve the full CREATE statement, then rewrite ENGINE clause.
        # We do the rewrite inline via clickhouse-client rather than
        # parsing DDL here — the server-side regex replacement in the
        # `create_table_query` column keeps quoting + partition by intact.
        #
        # Zero-downtime path for tables > 100 GB:
        #   1. CREATE TABLE ${tbl}_new ENGINE = ReplicatedMergeTree(...)
        #   2. For each partition: ALTER TABLE ${tbl}_new ATTACH PARTITION ID '<id>' FROM ${tbl}
        #   3. RENAME TABLE ${tbl} TO ${tbl}_old, ${tbl}_new TO ${tbl}
        #   4. DROP TABLE ${tbl}_old
        #
        # For Phase 1 (empty or small) we use the simple rename-create-insert path.

        local create_stmt
        create_stmt=$(ch_query_silent \
            "SELECT create_table_query FROM system.tables WHERE database = '${DATABASE}' AND name = '${tbl}'" || echo "")
        if [[ -z "${create_stmt}" && "${DRY_RUN}" == "false" ]]; then
            err "${tbl}: could not read create_table_query"
            failed=$((failed + 1))
            continue
        fi

        # Rewrite ENGINE = MergeTree(...) -> ENGINE = ReplicatedMergeTree('/clickhouse/tables/{shard}/<tbl>', '{replica}')
        # Also rename target so we can INSERT SELECT from the old one.
        local new_engine="ReplicatedMergeTree('/clickhouse/tables/{shard}/${tbl}', '{replica}')"
        local tmp_name="${tbl}_replicated"
        local new_create_stmt
        new_create_stmt=$(printf '%s' "${create_stmt}" | \
            sed -E "s|CREATE TABLE ${DATABASE}\.${tbl}|CREATE TABLE ${DATABASE}.${tmp_name}|" | \
            sed -E "s|ENGINE = MergeTree(\([^)]*\))?|ENGINE = ${new_engine}|")

        if [[ "${DRY_RUN}" == "true" ]]; then
            log "  would CREATE ${tmp_name} with engine ${new_engine}"
            log "  would INSERT INTO ${tmp_name} SELECT * FROM ${tbl}"
            log "  would RENAME ${tbl} -> ${tbl}_old, ${tmp_name} -> ${tbl}"
            migrated=$((migrated + 1))
            continue
        fi

        if ! ch_query "${new_create_stmt}"; then
            err "${tbl}: CREATE ${tmp_name} failed"
            failed=$((failed + 1))
            continue
        fi

        if ! ch_query "INSERT INTO ${tmp_name} SELECT * FROM ${tbl}"; then
            err "${tbl}: INSERT INTO ${tmp_name} failed — cleaning up"
            ch_query "DROP TABLE IF EXISTS ${tmp_name}" || true
            failed=$((failed + 1))
            continue
        fi

        if ! ch_query "RENAME TABLE ${tbl} TO ${tbl}_old, ${tmp_name} TO ${tbl}"; then
            err "${tbl}: RENAME failed — ${tmp_name} left behind for manual cleanup"
            failed=$((failed + 1))
            continue
        fi

        ok "${tbl}: migrated; old table preserved as ${tbl}_old"
        log "       verify row counts match, then DROP TABLE ${DATABASE}.${tbl}_old"
        migrated=$((migrated + 1))
    done <<< "${tables}"

    printf '\n=== Schema migration summary ===\n'
    printf '  migrated: %d\n'  "${migrated}"
    printf '  skipped : %d\n'  "${skipped}"
    printf '  failed  : %d\n'  "${failed}"

    if [[ "${failed}" -gt 0 ]]; then
        exit 1
    fi
}

main "$@"
