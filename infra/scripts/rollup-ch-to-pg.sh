#!/bin/bash
# CH→PG real-time rollup: aggregates ClickHouse events_raw into PG employee_daily_stats
# for every active endpoint with assigned_user_id. Runs idempotently (UPSERT).
#
# Usage: rollup-ch-to-pg.sh [YYYY-MM-DD]   (default: today UTC)
# Schedule via systemd timer or cron every 5 minutes.
set -euo pipefail

DAY=${1:-$(date -u +%Y-%m-%d)}
PG_USER=${PG_USER:-postgres}
CH_USER=${CH_USER:-personel_admin}
CH_PASS=${CH_PASS:-clickhouse_admin_pass}

PGEXEC='docker exec -i personel-postgres psql -U postgres -d personel -v ON_ERROR_STOP=1'
CHEXEC="docker exec -i personel-clickhouse clickhouse-client --user $CH_USER --password $CH_PASS"

# 1. Iterate active endpoints with an assigned user
$PGEXEC -tAc "SELECT id::text || '|' || assigned_user_id::text FROM endpoints WHERE is_active=true AND assigned_user_id IS NOT NULL" | while IFS='|' read -r EP_ID USER_ID; do
  [ -z "$EP_ID" ] && continue

  # 2. Per-endpoint metrics from CH for the day
  read -r SPAN IDLE_STARTS PROC_STARTS FG_CHANGES FILES NET KEYS SCREENS FIRST_AT LAST_AT < <($CHEXEC --query "
    SELECT
      toUInt32(dateDiff('minute', min(occurred_at), max(occurred_at))) AS span,
      toUInt32(countIf(event_type='session.idle_start')) AS idle_starts,
      toUInt32(countIf(event_type='process.start')) AS proc,
      toUInt32(countIf(event_type='process.foreground_change')) AS fg,
      toUInt32(countIf(event_type='file.created')) AS files,
      toUInt32(countIf(event_type='network.flow_summary')) AS net,
      toUInt32(countIf(event_type LIKE 'keystroke.%')) AS keys,
      toUInt32(countIf(event_type LIKE 'screenshot.%')) AS screens,
      toString(min(occurred_at)) AS first_at,
      toString(max(occurred_at)) AS last_at
    FROM personel.events_raw
    WHERE toDate(occurred_at) = '$DAY' AND endpoint_id='$EP_ID'
    FORMAT TabSeparated" 2>/dev/null)
  
  [ "$SPAN" = "0" ] && continue
  
  ACTIVE=$((SPAN > IDLE_STARTS*5 ? SPAN - IDLE_STARTS*5 : SPAN))
  IDLE=$((IDLE_STARTS*5))
  
  # 3. Top apps from process.start payload
  TOP_APPS=$($CHEXEC --query "
    SELECT toJSONString(arrayMap(t -> map('name', t.1, 'category', if(t.1 IN ('Code.exe','rustc.exe','cargo.exe','git.exe','docker.exe','psql.exe','clickhouse.exe','bash.exe','node.exe'),'productive',if(t.1 IN ('chrome.exe','msedge.exe','firefox.exe','plink.exe','ssh.exe','RuntimeBroker.exe'),'neutral','distracting')), 'minutes', toString(t.2)), groupArray((name, cnt))))
    FROM (
      SELECT JSONExtractString(payload, 'name') AS name, count() AS cnt
      FROM personel.events_raw
      WHERE event_type='process.start' AND payload != '{}' AND JSONExtractString(payload, 'name') != ''
        AND endpoint_id='$EP_ID' AND toDate(occurred_at) = '$DAY'
      GROUP BY name ORDER BY cnt DESC LIMIT 10
    )" 2>/dev/null)
  [ -z "$TOP_APPS" ] && TOP_APPS='[]'
  
  # 4. Filesystem hot paths
  FS_PATHS=$($CHEXEC --query "
    SELECT toJSONString(arrayMap(t -> map('path', t.1, 'events', toString(t.2)), groupArray((dir, cnt))))
    FROM (
      SELECT substring(JSONExtractString(payload, 'path'), 1, 80) AS dir, count() AS cnt
      FROM personel.events_raw
      WHERE event_type='file.created' AND payload != '{}'
        AND endpoint_id='$EP_ID' AND toDate(occurred_at) = '$DAY'
      GROUP BY dir ORDER BY cnt DESC LIMIT 5
    )" 2>/dev/null)
  [ -z "$FS_PATHS" ] && FS_PATHS='[]'
  
  # 5. Productivity score (simple heuristic: active - idle penalty)
  SCORE=$(( ACTIVE > 0 ? (ACTIVE * 80 / (ACTIVE + IDLE/2 + 1)) : 0 ))
  [ $SCORE -gt 100 ] && SCORE=100
  
  # 6. UPSERT
  $PGEXEC <<PSQL
INSERT INTO employee_daily_stats(
  user_id, day, active_minutes, idle_minutes, screenshot_count, keystroke_count,
  productivity_score, top_apps, rich_signals, first_activity_at, last_activity_at, updated_at
) VALUES (
  '$USER_ID'::uuid,
  '$DAY'::date,
  $ACTIVE,
  $IDLE,
  $SCREENS,
  $KEYS,
  $SCORE,
  '$TOP_APPS'::jsonb,
  jsonb_build_object(
    'filesystem', jsonb_build_object('created', $FILES, 'written', 0, 'deleted', 0, 'sensitive_hashed', 0, 'top_paths', '$FS_PATHS'::jsonb),
    'network', jsonb_build_object('flows', $NET, 'dns_queries', 0, 'top_hosts', '[]'::jsonb, 'geoip', '[]'::jsonb),
    'system', jsonb_build_object('locks', 0, 'unlocks', 0, 'sleeps', 0, 'wakes', 0, 'av_deactivated', 0),
    'tamper', jsonb_build_object('findings', 0, 'last_check', now()::text)
  ),
  '$FIRST_AT'::timestamptz,
  '$LAST_AT'::timestamptz,
  now()
)
ON CONFLICT (user_id, day) DO UPDATE SET
  active_minutes = EXCLUDED.active_minutes,
  idle_minutes = EXCLUDED.idle_minutes,
  screenshot_count = EXCLUDED.screenshot_count,
  keystroke_count = EXCLUDED.keystroke_count,
  productivity_score = EXCLUDED.productivity_score,
  top_apps = EXCLUDED.top_apps,
  rich_signals = EXCLUDED.rich_signals,
  first_activity_at = EXCLUDED.first_activity_at,
  last_activity_at = EXCLUDED.last_activity_at,
  updated_at = now();
PSQL
  echo "  rolled up endpoint=$EP_ID user=$USER_ID active=${ACTIVE}m idle=${IDLE}m score=$SCORE files=$FILES net=$NET"
done

echo "=== Rollup complete for $DAY ==="
