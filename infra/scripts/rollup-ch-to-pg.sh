#!/bin/bash
# CH→PG production rollup: aggregates ClickHouse events_raw into PG
# employee_daily_stats + employee_hourly_stats for every active endpoint.
#
# Features:
# - Daily per-user aggregation (active_minutes, idle_minutes, top_apps, rich_signals)
# - Hourly per-user aggregation (24 rows/day) for detail page bar chart
# - Real idle computation from paired session.idle_start / session.idle_end events
# - Policy-driven app category mapping (reads latest policies.rules for tenant)
# - UPSERT-based, runs idempotently every 5 minutes
#
# Usage: rollup-ch-to-pg.sh [YYYY-MM-DD]
# Schedule: systemd timer personel-rollup.timer every 5min
set -euo pipefail

DAY=${1:-$(date -u +%Y-%m-%d)}
CH_USER=${CH_USER:-personel_admin}
CH_PASS=${CH_PASS:-clickhouse_admin_pass}

PGEXEC='docker exec -i personel-postgres psql -U postgres -d personel -v ON_ERROR_STOP=1'
CHEXEC="docker exec -i personel-clickhouse clickhouse-client --user $CH_USER --password $CH_PASS"

echo "=== Personel rollup day=$DAY ==="

# Load policy-driven category map for each tenant (latest published policy
# JSON, extract app_allowlist/app_distracting/app_neutral rule sets).
# Written to /tmp/rollup-category-map-<tenant>.txt as "name,category".
build_category_map() {
  local tenant="$1"
  local outfile="/tmp/rollup-category-$tenant.txt"
  : > "$outfile"
  # Policy JSON shape: {"rules": [{"kind": "app_allowlist", "apps": [...]}, ...]}
  # Defensive: if no policy, leave file empty and fall back to default.
  $PGEXEC -tAc "
    SELECT COALESCE(rules::text, '{}')
    FROM policies
    WHERE tenant_id = '$tenant'::uuid
    ORDER BY created_at DESC NULLS LAST
    LIMIT 1" 2>/dev/null | \
  python3 -c "
import sys, json
try:
    raw = sys.stdin.read().strip()
    if not raw or raw == '{}':
        sys.exit(0)
    policy = json.loads(raw)
    rules = policy.get('rules', []) if isinstance(policy, dict) else []
    for r in rules:
        kind = r.get('kind', '')
        apps = r.get('apps', []) or r.get('patterns', [])
        if kind == 'app_allowlist':
            cat = 'productive'
        elif kind == 'app_distracting':
            cat = 'distracting'
        elif kind == 'app_neutral':
            cat = 'neutral'
        else:
            continue
        for a in apps:
            print(f'{a},{cat}')
except Exception as e:
    sys.stderr.write(f'policy parse skipped: {e}\n')
" > "$outfile" 2>/dev/null || true
}

# Classify an app name using the tenant's category map.
# Falls back to a hardcoded heuristic for common dev / productivity tools.
classify_app() {
  local app="$1"
  local tenant="$2"
  local mapfile="/tmp/rollup-category-$tenant.txt"
  if [ -f "$mapfile" ] && [ -s "$mapfile" ]; then
    local hit=$(grep -F "$app," "$mapfile" | head -1 | cut -d, -f2)
    if [ -n "$hit" ]; then
      echo "$hit"
      return
    fi
  fi
  # Fallback heuristic — dev/IDE tools + browsers + shells = productive,
  # social/media = distracting, everything else = neutral.
  case "$app" in
    Code.exe|rustc.exe|cargo.exe|git.exe|docker.exe|psql.exe|\
    clickhouse.exe|node.exe|python.exe|powershell.exe|pwsh.exe|\
    bash.exe|sh.exe|make.exe|msbuild.exe|devenv.exe|rustup.exe|\
    Excel.exe|Word.exe|PowerPoint.exe|Outlook.exe|Teams.exe|\
    SAP*|LogoTiger.exe|Mikro.exe|Netsis.exe|BordroPlus.exe)
      echo "productive" ;;
    chrome.exe|msedge.exe|firefox.exe|brave.exe|opera.exe|\
    slack.exe|Discord.exe)
      echo "neutral" ;;
    tiktok*|twitter*|instagram*|facebook*|netflix*|spotify*|\
    Steam.exe|Battle.net.exe|EpicGamesLauncher.exe)
      echo "distracting" ;;
    *)
      echo "neutral" ;;
  esac
}

# Real idle minute computation: pairs session.idle_start with the next
# session.idle_end (or end-of-day) per endpoint, sums the gap seconds,
# divides by 60. If a start has no matching end within the window we cap
# at 15 minutes (config: IDLE_CAP_SEC).
IDLE_CAP_SEC=${IDLE_CAP_SEC:-900}
compute_real_idle() {
  local ep="$1"
  $CHEXEC --query "
    WITH idle_events AS (
      SELECT occurred_at, event_type
      FROM personel.events_raw
      WHERE endpoint_id = '$ep'
        AND toDate(occurred_at) = '$DAY'
        AND event_type IN ('session.idle_start', 'session.idle_end')
      ORDER BY occurred_at
    ),
    with_next AS (
      SELECT
        occurred_at AS ts,
        event_type AS kind,
        leadInFrame(occurred_at) OVER (ORDER BY occurred_at) AS next_ts,
        leadInFrame(event_type) OVER (ORDER BY occurred_at) AS next_kind
      FROM idle_events
    )
    SELECT toInt32(floor(coalesce(sum(
      CASE
        WHEN kind='session.idle_start' AND next_kind='session.idle_end'
          THEN least(dateDiff('second', ts, next_ts), $IDLE_CAP_SEC)
        WHEN kind='session.idle_start' AND next_ts IS NULL
          THEN $IDLE_CAP_SEC
        ELSE 0
      END
    ), 0) / 60))
    FROM with_next" 2>/dev/null || echo "0"
}

# Query CH for per-hour aggregation over the day for one endpoint.
# Emits TSV: hour,active_min,idle_min,top_app,screens
# Heuristic: active_min = min(60, distinct_minute_buckets_with_activity).
build_hourly_tsv() {
  local ep="$1"
  $CHEXEC --query "
    SELECT
      toHour(occurred_at) AS hour,
      toUInt8(least(60, uniqExact(toUInt32(toDateTime(occurred_at)) / 60))) AS active_min,
      toUInt8(0) AS idle_min_placeholder,
      argMaxIf(
        JSONExtractString(payload, 'name'),
        occurred_at,
        event_type='process.start' AND JSONExtractString(payload, 'name') != ''
      ) AS top_app,
      toUInt8(countIf(event_type LIKE 'screenshot.%')) AS screens
    FROM personel.events_raw
    WHERE endpoint_id = '$ep'
      AND toDate(occurred_at) = '$DAY'
    GROUP BY hour
    ORDER BY hour
    FORMAT TabSeparated" 2>/dev/null
}

# Iterate active endpoints with an assigned user.
endpoints_sql="SELECT e.id::text || '|' || e.tenant_id::text || '|' || e.assigned_user_id::text
FROM endpoints e
WHERE e.is_active=true AND e.assigned_user_id IS NOT NULL"

# Collect unique tenants to build category maps once.
tenants=$($PGEXEC -tAc "SELECT DISTINCT tenant_id::text FROM endpoints WHERE is_active=true AND assigned_user_id IS NOT NULL")
for t in $tenants; do
  build_category_map "$t"
done

# Main loop — snapshot endpoints to a temp file so the while loop runs in
# the main shell (avoids nested process-substitution issues with the
# per-iteration `read -r ... < <($CHEXEC ...)` calls).
endpoints_file=$(mktemp)
trap 'rm -f "$endpoints_file" /tmp/rollup-category-*.txt' EXIT
$PGEXEC -tAc "$endpoints_sql" > "$endpoints_file"

while IFS='|' read -r EP_ID TENANT_ID USER_ID <&3; do
  [ -z "$EP_ID" ] && continue

  # Daily metrics — use TAB-separated read because the timestamps include spaces
  IFS=$'\t' read -r SPAN PROC_STARTS FILES NET KEYS SCREENS FIRST_AT LAST_AT < <($CHEXEC --query "
    SELECT
      toUInt32(dateDiff('minute', min(occurred_at), max(occurred_at))),
      toUInt32(countIf(event_type='process.start')),
      toUInt32(countIf(event_type='file.created')),
      toUInt32(countIf(event_type='network.flow_summary')),
      toUInt32(countIf(event_type LIKE 'keystroke.%')),
      toUInt32(countIf(event_type LIKE 'screenshot.%')),
      toString(min(occurred_at)),
      toString(max(occurred_at))
    FROM personel.events_raw
    WHERE toDate(occurred_at)='$DAY' AND endpoint_id='$EP_ID'
    FORMAT TabSeparated" 2>/dev/null)

  [ -z "${SPAN:-}" ] || [ "$SPAN" = "0" ] && continue

  IDLE=$(compute_real_idle "$EP_ID")
  ACTIVE=$((SPAN > IDLE ? SPAN - IDLE : SPAN))

  # Top apps with policy-driven category mapping
  # Python does the heavy lifting so we can use the classify_app via a
  # map lookup rather than shelling out per line.
  MAP_FILE="/tmp/rollup-category-$TENANT_ID.txt"
  TOP_APPS=$($CHEXEC --query "
    SELECT JSONExtractString(payload, 'name') AS name, count() AS cnt
    FROM personel.events_raw
    WHERE event_type='process.start' AND payload != '{}'
      AND JSONExtractString(payload, 'name') != ''
      AND endpoint_id='$EP_ID' AND toDate(occurred_at)='$DAY'
    GROUP BY name ORDER BY cnt DESC LIMIT 10
    FORMAT TSV" 2>/dev/null | python3 -c "
import sys, json, os
category_map = {}
try:
    with open('$MAP_FILE') as f:
        for line in f:
            parts = line.strip().split(',')
            if len(parts) == 2:
                category_map[parts[0]] = parts[1]
except FileNotFoundError:
    pass

def classify(name):
    if name in category_map:
        return category_map[name]
    n = name.lower()
    if any(x in n for x in ['code.exe','rustc','cargo','git.','docker','psql','node.','python','powershell','bash','excel','word','outlook','teams','sap','logotiger','mikro','bordroplus']):
        return 'productive'
    if any(x in n for x in ['chrome','edge','firefox','slack','discord']):
        return 'neutral'
    if any(x in n for x in ['tiktok','twitter','instagram','facebook','netflix','spotify','steam','battle.net','epicgames']):
        return 'distracting'
    return 'neutral'

apps = []
for line in sys.stdin:
    parts = line.strip().split('\t')
    if len(parts) == 2 and parts[0]:
        apps.append({'name': parts[0], 'category': classify(parts[0]), 'minutes': int(parts[1])})
print(json.dumps(apps))
")
  [ -z "$TOP_APPS" ] && TOP_APPS='[]'

  # Rich signals — filesystem top paths
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

  # Rich signals — network top hosts (from payload host field)
  NET_HOSTS=$($CHEXEC --query "
    SELECT toJSONString(arrayMap(t -> map('host', t.1, 'bytes', toString(t.2)), groupArray((h, b))))
    FROM (
      SELECT JSONExtractString(payload, 'host') AS h,
             sum(toUInt64OrZero(JSONExtractString(payload, 'bytes'))) AS b
      FROM personel.events_raw
      WHERE event_type='network.flow_summary' AND payload != '{}'
        AND JSONExtractString(payload, 'host') != ''
        AND endpoint_id='$EP_ID' AND toDate(occurred_at) = '$DAY'
      GROUP BY h ORDER BY b DESC LIMIT 5
    )" 2>/dev/null)
  [ -z "$NET_HOSTS" ] && NET_HOSTS='[]'

  # Productivity score — simple heuristic close to scoring.ComputeProductivity
  # (active penalised by distraction ratio + idle). Policy-driven top_apps
  # already informs category split; we approximate productive/distracting
  # minutes proportional to count.
  SCORE=$(python3 -c "
import json
apps = json.loads('''$TOP_APPS''') if '''$TOP_APPS''' != '[]' else []
total = sum(a['minutes'] for a in apps) or 1
prod = sum(a['minutes'] for a in apps if a['category']=='productive')
dist = sum(a['minutes'] for a in apps if a['category']=='distracting')
active = $ACTIVE
idle = $IDLE
baseline = (prod * 1.0 + (total - prod - dist) * 0.5) / (total + idle/2 + 1) * 100 if total > 0 else 50
if total > 0 and dist / total > 0.3:
    baseline -= 10
if idle > 0.4 * max(active, 1):
    baseline -= 10
print(int(max(0, min(100, baseline))))
")
  [ -z "$SCORE" ] && SCORE=50

  # Daily UPSERT
  $PGEXEC <<PSQL
INSERT INTO employee_daily_stats(
  user_id, day, active_minutes, idle_minutes, screenshot_count, keystroke_count,
  productivity_score, top_apps, rich_signals, first_activity_at, last_activity_at, updated_at
) VALUES (
  '$USER_ID'::uuid, '$DAY'::date, $ACTIVE, $IDLE, $SCREENS, $KEYS, $SCORE,
  '$TOP_APPS'::jsonb,
  jsonb_build_object(
    'filesystem', jsonb_build_object('created', $FILES, 'written', 0, 'deleted', 0,
                                     'sensitive_hashed', 0, 'top_paths', '$FS_PATHS'::jsonb),
    'network', jsonb_build_object('flows', $NET, 'dns_queries', 0, 'top_hosts', '$NET_HOSTS'::jsonb, 'geoip', '[]'::jsonb),
    'system', jsonb_build_object('locks', 0, 'unlocks', 0, 'sleeps', 0, 'wakes', 0, 'av_deactivated', 0),
    'tamper', jsonb_build_object('findings', 0, 'last_check', now()::text)
  ),
  '$FIRST_AT'::timestamptz, '$LAST_AT'::timestamptz, now()
)
ON CONFLICT (user_id, day) DO UPDATE SET
  active_minutes=EXCLUDED.active_minutes, idle_minutes=EXCLUDED.idle_minutes,
  screenshot_count=EXCLUDED.screenshot_count, keystroke_count=EXCLUDED.keystroke_count,
  productivity_score=EXCLUDED.productivity_score, top_apps=EXCLUDED.top_apps,
  rich_signals=EXCLUDED.rich_signals,
  first_activity_at=EXCLUDED.first_activity_at, last_activity_at=EXCLUDED.last_activity_at,
  updated_at=now();
PSQL

  # Hourly UPSERT (up to 24 rows/day)
  hourly_file=$(mktemp)
  build_hourly_tsv "$EP_ID" > "$hourly_file"
  while IFS=$'\t' read -r HOUR ACTIVE_MIN IDLE_MIN_PH TOP_APP SCREENS_H; do
    [ -z "${HOUR:-}" ] && continue
    : "${ACTIVE_MIN:=0}"
    : "${SCREENS_H:=0}"
    : "${TOP_APP:=}"
    TOP_APP_ESC=${TOP_APP//\'/\'\'}
    $PGEXEC <<PSQL2
INSERT INTO employee_hourly_stats(user_id, day, hour, active_minutes, idle_minutes, top_app, screenshot_count)
VALUES ('$USER_ID'::uuid, '$DAY'::date, $HOUR, $ACTIVE_MIN, 0, '${TOP_APP_ESC}'::text, $SCREENS_H)
ON CONFLICT (user_id, day, hour) DO UPDATE SET
  active_minutes=EXCLUDED.active_minutes,
  top_app=EXCLUDED.top_app,
  screenshot_count=EXCLUDED.screenshot_count;
PSQL2
  done < "$hourly_file"
  rm -f "$hourly_file"

  echo "  rolled up endpoint=$EP_ID user=$USER_ID active=${ACTIVE}m idle=${IDLE}m score=$SCORE files=$FILES net=$NET"
done 3< "$endpoints_file"

echo "=== Rollup complete for $DAY ==="
