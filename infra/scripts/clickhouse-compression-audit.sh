#!/usr/bin/env bash
# clickhouse-compression-audit.sh
#
# Faz 7 / Roadmap item #77: ZSTD tuning audit.
#
# Queries system.columns for the current CODEC per table/column and emits
# a simple text report showing which columns still use the default LZ4
# codec vs an explicit ZSTD codec. Large text columns (payload,
# window_title, exe_name, image_path, etc.) typically compress ~25%
# better under ZSTD(3) than under default LZ4, at the cost of ~2x CPU
# on writes (read path is unaffected — decompression cost is negligible
# for ZSTD vs LZ4 on modern hardware).
#
# Usage:
#   PERSONEL_CLICKHOUSE_DSN=clickhouse://default:pass@localhost:9000/personel \
#     ./infra/scripts/clickhouse-compression-audit.sh
#
# Or, with docker exec:
#   docker exec personel-clickhouse clickhouse-client --query "$(cat << 'SQL'
#   ... report query below
#   SQL
#   )"

set -euo pipefail

: "${PERSONEL_CLICKHOUSE_DSN:=tcp://localhost:9000?database=personel}"
DATABASE="${PERSONEL_CLICKHOUSE_DATABASE:-personel}"

echo "==============================================================================="
echo "  Personel — ClickHouse Compression Codec Audit"
echo "  database: ${DATABASE}"
echo "  Roadmap item #77"
echo "==============================================================================="
echo

clickhouse-client --query "
SELECT
    table,
    name                                            AS column,
    type                                            AS type,
    compression_codec                                AS codec,
    formatReadableSize(data_compressed_bytes)        AS compressed,
    formatReadableSize(data_uncompressed_bytes)      AS uncompressed,
    round(data_compressed_bytes
          / nullIf(data_uncompressed_bytes, 0), 3)   AS ratio
  FROM system.columns
 WHERE database = '${DATABASE}'
   AND table IN (
       'events_raw',
       'events_sensitive_window',
       'events_sensitive_clipboard_meta',
       'events_sensitive_keystroke_meta',
       'events_sensitive_file',
       'agent_heartbeats'
   )
 ORDER BY table, data_uncompressed_bytes DESC
 FORMAT Pretty
"

echo
echo "-------------------------------------------------------------------------------"
echo "  Recommended ZSTD targets"
echo "-------------------------------------------------------------------------------"
echo
echo "Columns that SHOULD use CODEC(ZSTD(3)) — high-entropy text / blob refs:"
echo "  - events_raw.payload"
echo "  - events_raw.user_sid"
echo "  - events_raw.batch_hmac"
echo "  - events_sensitive_window.payload"
echo "  - events_sensitive_clipboard_meta.payload"
echo "  - events_sensitive_keystroke_meta.payload"
echo "  - events_sensitive_file.payload"
echo
echo "Columns that should STAY on default LZ4 — already well-compressed or small:"
echo "  - tenant_id, endpoint_id (UUID — 16 bytes)"
echo "  - event_type (LowCardinality — dictionary-encoded)"
echo "  - pii_class, retention_class (LowCardinality)"
echo "  - occurred_at, received_at (DateTime64 — delta coding is separate)"
echo
echo "Apply via: infra/compose/clickhouse/schemas-zstd-tuning.sql"
