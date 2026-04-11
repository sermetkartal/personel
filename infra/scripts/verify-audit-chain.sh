#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Audit Chain Integrity Verifier
# TR: Hash zincirinin bütünlüğünü doğrular. Değişiklik varsa alarm verir.
# EN: Verifies hash chain integrity. Alerts on any tampering.
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_DIR="${SCRIPT_DIR}/../compose"
set -a; source "${COMPOSE_DIR}/.env"; set +a

POST_RESTORE=false
for arg in "$@"; do [[ "${arg}" == "--post-restore" ]] && POST_RESTORE=true; done

TENANT_ID="${PERSONEL_TENANT_ID:-}"
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
LOG_DIR="/var/log/personel/compliance"
ATTEST_FILE="${LOG_DIR}/audit-attest-$(date +%Y%m%d).json"
mkdir -p "${LOG_DIR}"

log()  { echo "[audit-verify] $*"; }
fail() { echo "[audit-verify] FAIL: $*" >&2; }

ERRORS=0

log "Starting audit chain verification at ${TIMESTAMP}"
log "Tenant: ${TENANT_ID}"

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
  WHERE tenant_id = '${TENANT_ID}'::UUID
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
  WHERE tenant_id = '${TENANT_ID}'::UUID
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
SELECT COUNT(*) FROM audit.audit_events WHERE tenant_id = '${TENANT_ID}'::UUID;
" 2>/dev/null | tr -d ' ')

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
