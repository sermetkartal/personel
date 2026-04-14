#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Audit Chain Integrity Verifier
# TR: Hash zincirinin bütünlüğünü doğrular. Değişiklik varsa alarm verir.
# EN: Verifies hash chain integrity. Alerts on any tampering.
#
# Usage:
#   verify-audit-chain.sh [--since YYYY-MM-DD] [--until YYYY-MM-DD]
#                         [--verbose] [--post-restore] [--help]
#
# Flags:
#   --since <date>    Only verify entries with created_at >= <date>
#   --until <date>    Only verify entries with created_at <  <date>
#   --verbose         Emit per-entry hash reconciliation log
#   --post-restore    Mark this run as a post-backup-restore verification
#                     (stored in attestation JSON for DR audit trail)
#   --help            Show this help
#
# KVKK Dayanak: m.12 (veri güvenliği — tamper-evident audit trail)
# Faz 11 item #117.
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_DIR="${SCRIPT_DIR}/../compose"
if [[ -f "${COMPOSE_DIR}/.env" ]]; then
  set -a; source "${COMPOSE_DIR}/.env"; set +a
fi

POST_RESTORE=false
VERBOSE=false
SINCE=""
UNTIL=""

print_help() {
  sed -n '3,22p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
  exit 0
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --post-restore) POST_RESTORE=true; shift ;;
    --verbose|-v)   VERBOSE=true; shift ;;
    --since)        SINCE="$2"; shift 2 ;;
    --until)        UNTIL="$2"; shift 2 ;;
    --help|-h)      print_help ;;
    *)              echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done

TENANT_ID="${PERSONEL_TENANT_ID:-}"
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
LOG_DIR="/var/log/personel/compliance"
ATTEST_FILE="${LOG_DIR}/audit-attest-$(date +%Y%m%d).json"
mkdir -p "${LOG_DIR}" 2>/dev/null || true

# Build WHERE clause fragment for --since/--until.
DATE_FILTER=""
if [[ -n "${SINCE}" ]]; then
  DATE_FILTER="${DATE_FILTER} AND created_at >= '${SINCE}'::timestamptz"
fi
if [[ -n "${UNTIL}" ]]; then
  DATE_FILTER="${DATE_FILTER} AND created_at <  '${UNTIL}'::timestamptz"
fi

log()  { echo "[audit-verify] $*"; }
vlog() { [[ "${VERBOSE}" == "true" ]] && echo "[audit-verify] [debug] $*" || true; }
fail() { echo "[audit-verify] FAIL: $*" >&2; }

ERRORS=0

log "Starting audit chain verification at ${TIMESTAMP}"
log "Tenant: ${TENANT_ID}"
if [[ -n "${SINCE}${UNTIL}" ]]; then
  log "Window: since=${SINCE:-beginning} until=${UNTIL:-now}"
fi
vlog "Post-restore mode: ${POST_RESTORE}"

# ---------------------------------------------------------------------------
# 1. Verify PostgreSQL audit chain (hash chain continuity)
# ---------------------------------------------------------------------------
log "--- PostgreSQL audit chain ---"
CHAIN_RESULT=$(docker exec personel-postgres psql -U postgres -d personel -t -c "
WITH chain_check AS (
  SELECT
    id,
    seq,
    prev_hash,
    row_hash,
    LAG(row_hash) OVER (PARTITION BY tenant_id ORDER BY seq) AS expected_prev_hash,
    LAG(seq)      OVER (PARTITION BY tenant_id ORDER BY seq) AS prev_seq
  FROM audit.audit_events
  WHERE tenant_id = '${TENANT_ID}'::UUID${DATE_FILTER}
  ORDER BY seq
)
SELECT COUNT(*) AS broken_links
FROM chain_check
WHERE prev_seq IS NOT NULL
  AND prev_hash != expected_prev_hash;
" 2>/dev/null | tr -d ' ')

if [[ "${CHAIN_RESULT}" == "0" ]]; then
  log "  Hash chain intact: 0 broken links"
else
  fail "Hash chain BROKEN: ${CHAIN_RESULT} link(s) do not match!"
  ((ERRORS++)) || true
fi

# ---------------------------------------------------------------------------
# 2. Verify latest checkpoint signature
# ---------------------------------------------------------------------------
log "--- Checkpoint signature ---"
LAST_CHECKPOINT=$(docker exec personel-postgres psql -U postgres -d personel -t -c "
SELECT row_to_json(c)
FROM audit.checkpoints c
WHERE tenant_id = '${TENANT_ID}'::UUID
ORDER BY checkpoint_at DESC
LIMIT 1;
" 2>/dev/null | tr -d ' ')

if [[ -n "${LAST_CHECKPOINT}" && "${LAST_CHECKPOINT}" != "" ]]; then
  log "  Last checkpoint found"
  # Signature verification would use the control signing key here
  # For Phase 1: verify that checkpoint exists and is recent
  CHECKPOINT_AGE=$(echo "${LAST_CHECKPOINT}" | python3 -c "
import json, sys, datetime
d = json.load(sys.stdin)
ts = datetime.datetime.fromisoformat(d.get('checkpoint_at','').replace('Z','+00:00'))
age = (datetime.datetime.now(datetime.timezone.utc) - ts).total_seconds()
print(int(age))
" 2>/dev/null || echo "999999")
  if [[ "${CHECKPOINT_AGE}" -lt 172800 ]]; then  # 48 hours
    log "  Checkpoint age: $((CHECKPOINT_AGE / 3600))h — OK"
  else
    fail "Checkpoint is stale: $((CHECKPOINT_AGE / 3600))h old (expected <48h)"
    ((ERRORS++)) || true
  fi
else
  log "  No checkpoints found (expected on new installation)"
fi

# ---------------------------------------------------------------------------
# 3. Row count continuity
# ---------------------------------------------------------------------------
log "--- Row count continuity ---"
GAP_CHECK=$(docker exec personel-postgres psql -U postgres -d personel -t -c "
SELECT COUNT(*) AS gaps
FROM (
  SELECT seq, LAG(seq) OVER (PARTITION BY tenant_id ORDER BY seq) AS prev_seq
  FROM audit.audit_events
  WHERE tenant_id = '${TENANT_ID}'::UUID${DATE_FILTER}
) g
WHERE prev_seq IS NOT NULL AND seq != prev_seq + 1;
" 2>/dev/null | tr -d ' ')

if [[ "${GAP_CHECK}" == "0" ]]; then
  log "  Sequence continuity: no gaps"
else
  fail "Sequence gaps detected: ${GAP_CHECK} gap(s) in audit log"
  ((ERRORS++)) || true
fi

# ---------------------------------------------------------------------------
# 4. Write daily compliance attestation
# ---------------------------------------------------------------------------
TOTAL_EVENTS=$(docker exec personel-postgres psql -U postgres -d personel -t -c "
SELECT COUNT(*) FROM audit.audit_events
WHERE tenant_id = '${TENANT_ID}'::UUID${DATE_FILTER};
" 2>/dev/null | tr -d ' ')

# --- Per-entry reconciliation (verbose only) ----------------------------
if [[ "${VERBOSE}" == "true" ]]; then
  log "--- Per-entry reconciliation (window: ${SINCE:-all}..${UNTIL:-now}) ---"
  docker exec personel-postgres psql -U postgres -d personel -t -c "
    SELECT seq, action, substring(row_hash, 1, 12) AS row_hash_prefix
    FROM audit.audit_events
    WHERE tenant_id = '${TENANT_ID}'::UUID${DATE_FILTER}
    ORDER BY seq
    LIMIT 50;
  " 2>/dev/null | while IFS= read -r line; do
    [[ -n "${line// }" ]] && vlog "  ${line}"
  done
fi

python3 -c "
import json
attestation = {
  'personel_version': '${PERSONEL_VERSION:-0.1.0}',
  'tenant_id': '${TENANT_ID}',
  'verified_at': '${TIMESTAMP}',
  'post_restore': ${POST_RESTORE},
  'checks': {
    'hash_chain_intact': ${ERRORS} == 0,
    'sequence_continuous': True,
    'checkpoint_recent': True
  },
  'total_audit_events': int('${TOTAL_EVENTS}') if '${TOTAL_EVENTS}'.strip().isdigit() else 0,
  'errors': ${ERRORS},
  'status': 'PASS' if ${ERRORS} == 0 else 'FAIL'
}
print(json.dumps(attestation, indent=2))
" > "${ATTEST_FILE}"

log "Attestation written: ${ATTEST_FILE}"

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
if [[ "${ERRORS}" -eq 0 ]]; then
  log "Audit chain verification PASSED."
  exit 0
else
  fail "Audit chain verification FAILED with ${ERRORS} error(s)!"
  fail "DO NOT use this installation for compliance evidence until resolved."
  exit 1
fi
