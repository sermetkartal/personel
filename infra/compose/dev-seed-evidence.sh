#!/usr/bin/env bash
# =============================================================================
# dev-seed-evidence.sh — populate the dev stack with varied evidence items
# for a visual dashboard preview. Covers 4 TSC controls over 6 months:
#   A1.2 Backup Run        ×15 (weekly-ish, past 6 months)
#   CC6.3 Access Review    ×5  (quarterly cadence)
#   CC7.3 Incident Closure ×6  (varied severity)
#   CC9.1 BCP Drill        ×4  (live + tabletop)
#
# The 3 remaining expected controls (CC6.1 privileged access, CC7.1/CC8.1
# policy push, P5.1/P7.1 DSR fulfilment) require domain resources
# (live view sessions, policies, DSRs) that aren't trivial to seed via
# curl alone — they're left as gaps in the demo matrix so the gap UI
# has something to surface.
# =============================================================================
set -euo pipefail

TOKEN=$(curl -sS -X POST "http://localhost:8080/realms/personel/protocol/openid-connect/token" \
  -d "client_id=console" -d "username=dpo-test" -d "password=dpo-test-pass" -d "grant_type=password" \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['access_token'])")

API="http://localhost:8001"

post() {
  local path="$1" body="$2"
  curl -sS -X POST "$API$path" \
    -H "Authorization: Bearer $TOKEN" \
    -H "Content-Type: application/json" \
    -d "$body" -w " %{http_code}\n" 2>&1 | tr '\n' ' '
  echo
}

# macOS portable date offsets
iso_days_ago() {
  date -u -v"-${1}d" +"%Y-%m-%dT%H:%M:%SZ"
}
iso_days_ago_plus_min() {
  date -u -v"-${1}d" -v"+${2}M" +"%Y-%m-%dT%H:%M:%SZ"
}

rand_sha() { openssl rand -hex 32; }
rand_size() { echo $((RANDOM*10000 + 200000000)); }

echo "=== A1.2 Backup Runs (15 × past 6 months) ==="
KINDS=("postgres" "clickhouse" "minio" "vault")
for OFFSET in 180 165 150 135 120 105 90 75 60 45 30 20 14 7 2; do
  START=$(iso_days_ago $OFFSET)
  END=$(iso_days_ago_plus_min $OFFSET 18)
  KIND=${KINDS[$((RANDOM % 4))]}
  SIZE=$(rand_size)
  SHA=$(rand_sha)
  post /v1/system/backup-runs "$(cat <<EOF
{"kind":"$KIND","target_path":"minio://backups/$KIND/$START.dump","size_bytes":$SIZE,"sha256":"$SHA","started_at":"$START","finished_at":"$END","source_host":"db-primary-$((RANDOM%3+1)).internal"}
EOF
)"
done

echo
echo "=== CC6.3 Access Reviews (quarterly + recent) ==="
SCOPES=("admin_role" "dpo_role" "investigator_role" "legal_hold_owners" "regular_users")
for OFFSET in 170 110 55 28 5; do
  START=$(iso_days_ago $OFFSET)
  END=$(iso_days_ago_plus_min $OFFSET 45)
  SCOPE=${SCOPES[$((RANDOM % 5))]}
  post /v1/system/access-reviews "$(cat <<EOF
{
  "scope": "$SCOPE",
  "started_at": "$START",
  "completed_at": "$END",
  "decisions": [
    {"user_id": "u-$RANDOM", "username": "alice$RANDOM", "action": "retained"},
    {"user_id": "u-$RANDOM", "username": "bob$RANDOM", "action": "revoked", "reason": "rol değişikliği"},
    {"user_id": "u-$RANDOM", "username": "carol$RANDOM", "action": "retained"},
    {"user_id": "u-$RANDOM", "username": "dave$RANDOM", "action": "reduced", "reason": "kapsam daraltma"}
  ],
  "notes": "Çeyreklik gözden geçirme — demo seed"
}
EOF
)"
done

echo
echo "=== CC7.3 Incident Closures (6 × varied severity) ==="
SEVS=("low" "medium" "medium" "high" "high" "critical")
SUMMARIES=(
  "Proaktif DLP testi — gerçek ihlal yok"
  "Başarısız admin login brute force — MFA bloke etti"
  "Yanlış tenant'a kısa bir metadata sızıntısı — 5 dakika içinde kapatıldı"
  "Yetkisiz erişim denemesi, hesap kilitlendi"
  "Upstream vendor API key sızıntısı, anahtar rotate edildi"
  "3 çalışan screenshot yanlış tenant'a kopyalandı — KVKK bildirimli"
)
for i in 0 1 2 3 4 5; do
  OFFSET=$((150 - i*25))
  DETECTED=$(iso_days_ago $OFFSET)
  CONTAINED=$(iso_days_ago_plus_min $OFFSET 30)
  CLOSED=$(iso_days_ago_plus_min $((OFFSET - 2)) 0)
  SEV=${SEVS[$i]}
  SUMMARY=${SUMMARIES[$i]}
  KVKK_NOTIFY=""
  if [[ "$SEV" == "critical" ]]; then
    KVKK_NOTIFY="\"kvkk_notified_at\": \"$(iso_days_ago_plus_min $OFFSET 120)\","
  fi
  post /v1/system/incident-closures "$(cat <<EOF
{
  "incident_id": "INC-2026-$(printf '%03d' $((i+1)))",
  "severity": "$SEV",
  "detected_at": "$DETECTED",
  "contained_at": "$CONTAINED",
  "closed_at": "$CLOSED",
  $KVKK_NOTIFY
  "lead_responder_id": "sec-lead-1",
  "summary": "$SUMMARY",
  "root_cause": "Konfigürasyon kayması / beklenmeyen kullanıcı davranışı",
  "remediation_actions": [
    "Runbook güncellendi",
    "Alert threshold düşürüldü",
    "Policy CAB'ye eskalasyon"
  ]
}
EOF
)"
done

echo
echo "=== CC9.1 BCP Drills (4 × live+tabletop over 6 months) ==="
SCENARIOS=("ransomware" "vault_compromise" "clickhouse_loss" "az_failure")
TYPES=("tabletop" "live" "tabletop" "live")
for i in 0 1 2 3; do
  OFFSET=$((150 - i*40))
  START=$(iso_days_ago $OFFSET)
  END=$(iso_days_ago_plus_min $OFFSET 180)
  SC=${SCENARIOS[$i]}
  TP=${TYPES[$i]}
  ALL_MET="true"
  [[ $i -eq 2 ]] && ALL_MET="false"
  T2_MET="true"
  [[ $i -eq 2 ]] && T2_MET="false"
  post /v1/system/bcp-drills "$(cat <<EOF
{
  "drill_id": "BCP-Q$((i+1))-2026",
  "type": "$TP",
  "scenario": "$SC",
  "started_at": "$START",
  "completed_at": "$END",
  "facilitator_id": "cto-1",
  "tier_results": [
    {"tier": 0, "service": "vault+audit", "target_rto_seconds": 7200, "actual_rto_seconds": 5400, "met_rto": true, "notes": "Şamir unseal tamam"},
    {"tier": 1, "service": "postgres+api", "target_rto_seconds": 14400, "actual_rto_seconds": 12000, "met_rto": true},
    {"tier": 2, "service": "clickhouse", "target_rto_seconds": 28800, "actual_rto_seconds": 32000, "met_rto": $T2_MET, "notes": "MinIO lifecycle yavaş"}
  ],
  "lessons_learned": "Tier 2 RTO marjinal — MinIO lifecycle path optimize edilecek. Action: backup-restore runbook §5."
}
EOF
)"
done

echo
echo "=== DONE ==="
echo "Query coverage:"
curl -sS "$API/v1/system/evidence-coverage?period=$(date -u +%Y-%m)" \
  -H "Authorization: Bearer $TOKEN" | python3 -m json.tool
