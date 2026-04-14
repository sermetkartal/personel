#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Post-Install Validation (Faz 13 #135)
# TR: Kurulum sonrası tüm servislerin sağlıklı olduğunu doğrular + JSON rapor.
# EN: Validates all services after install + emits JSON report.
#
# Exit codes:
#   0 — all checks pass (or warn)
#   1 — one or more critical checks failed
#
# Flags:
#   --report=PATH   write JSON report (default stdout)
#   --quick         skip end-to-end enroll + NATS/ClickHouse flow test
# =============================================================================
set -euo pipefail

COMPOSE_DIR="${COMPOSE_DIR:-/opt/personel/infra/compose}"
SCRIPTS_DIR="${SCRIPTS_DIR:-/opt/personel/infra/scripts}"
REPORT_FILE=""
QUICK=false

for arg in "$@"; do
  case "${arg}" in
    --report=*) REPORT_FILE="${arg#*=}" ;;
    --quick)    QUICK=true ;;
  esac
done

PASS=0; WARN=0; FAIL=0
RESULTS="["
SEP=""

record() {
  local status="$1" name="$2" msg="$3" remediation="${4:-}"
  RESULTS="${RESULTS}${SEP}{\"check\":\"${name}\",\"status\":\"${status}\",\"message\":\"${msg//\"/\\\"}\",\"remediation\":\"${remediation//\"/\\\"}\"}"
  SEP=","
  case "${status}" in
    pass) PASS=$((PASS+1)) ;;
    warn) WARN=$((WARN+1)) ;;
    fail) FAIL=$((FAIL+1)) ;;
  esac
}

log()  { echo -e "\033[0;32m[validate]\033[0m $*"; }
warn() { echo -e "\033[1;33m[validate WARN]\033[0m $*" >&2; }
err()  { echo -e "\033[0;31m[validate FAIL]\033[0m $*" >&2; }

check_http() {
  local name="$1" url="$2" expected_code="${3:-200}" remediation="${4:-}"
  local code
  code=$(curl -sk -o /dev/null -w '%{http_code}' --max-time 10 "${url}" 2>/dev/null || echo "000")
  if [[ "${code}" == "${expected_code}" ]]; then
    log "${name} → HTTP ${code}"
    record pass "${name}" "HTTP ${code}" ""
  else
    err "${name} → HTTP ${code} (expected ${expected_code})"
    record fail "${name}" "HTTP ${code} (expected ${expected_code})" "${remediation}"
  fi
}

# ---------------------------------------------------------------------------
# Health checks
# ---------------------------------------------------------------------------
log "=== Service health ==="
check_http api.healthz "http://localhost:8000/healthz" 200 "docker compose restart api"
check_http api.readyz  "http://localhost:8000/readyz"  200 "check dependencies"
check_http gateway.healthz "http://localhost:9443/healthz" 200 "docker compose restart gateway"
check_http console.home "http://localhost:3000/tr" 200 "docker compose restart console"
check_http portal.home  "http://localhost:3001/tr" 200 "docker compose restart portal"
check_http keycloak.ready "http://localhost:8080/realms/personel/.well-known/openid-configuration" 200 "docker compose restart keycloak"

# ---------------------------------------------------------------------------
# Vault sealed state
# ---------------------------------------------------------------------------
log "=== Vault ==="
if docker exec personel-vault sh -c "VAULT_SKIP_VERIFY=true vault status -format=json" 2>/dev/null \
    | python3 -c "import json,sys; d=json.load(sys.stdin); sys.exit(0 if not d.get('sealed') else 1)"; then
  log "Vault unsealed"
  record pass vault.sealed "unsealed" ""
else
  err "Vault sealed or unreachable"
  record fail vault.sealed "sealed or unreachable" "vault-unseal.sh"
fi

# ---------------------------------------------------------------------------
# Postgres replication lag (if replica exists)
# ---------------------------------------------------------------------------
log "=== Postgres replication ==="
LAG=$(docker exec personel-postgres psql -U postgres -d personel -tAc \
  "SELECT COALESCE(EXTRACT(EPOCH FROM (now()-pg_last_xact_replay_timestamp())),0)::int FROM pg_stat_replication LIMIT 1" 2>/dev/null || echo "")
if [[ -z "${LAG}" ]]; then
  log "Postgres replica not configured (single-node)"
  record pass postgres.replication "single-node" ""
elif [[ "${LAG}" -le 1 ]]; then
  log "Postgres replication lag ${LAG}s"
  record pass postgres.replication "lag ${LAG}s" ""
else
  warn "Postgres replication lag ${LAG}s (>1s)"
  record warn postgres.replication "lag ${LAG}s" "check replica bandwidth"
fi

# ---------------------------------------------------------------------------
# NATS streams
# ---------------------------------------------------------------------------
log "=== NATS streams ==="
STREAMS_JSON=$(docker exec personel-nats wget -qO- "http://127.0.0.1:8222/jsz?streams=1" 2>/dev/null || echo "")
for s in events_raw events_sensitive live_view_control agent_health pki_events; do
  if echo "${STREAMS_JSON}" | grep -q "\"name\":\"${s}\""; then
    log "Stream ${s} exists"
    record pass "nats.${s}" "stream present" ""
  else
    err "Stream ${s} missing"
    record fail "nats.${s}" "stream missing" "bootstrap-nats.sh"
  fi
done

# ---------------------------------------------------------------------------
# MinIO buckets
# ---------------------------------------------------------------------------
log "=== MinIO buckets ==="
EXPECTED_BUCKETS="screenshots audit-worm evidence dsr-artifacts backups logs config"
BUCKETS=$(docker exec personel-minio sh -c \
  "mc alias set local http://localhost:9000 \$MINIO_ROOT_USER \$MINIO_ROOT_PASSWORD >/dev/null 2>&1; mc ls local 2>/dev/null | awk '{print \$NF}' | tr -d /" || echo "")
for b in ${EXPECTED_BUCKETS}; do
  if echo "${BUCKETS}" | grep -qw "${b}"; then
    log "Bucket ${b} exists"
    record pass "minio.${b}" "bucket present" ""
  else
    err "Bucket ${b} missing"
    record fail "minio.${b}" "bucket missing" "minio-worm-bootstrap.sh"
  fi
done

# ---------------------------------------------------------------------------
# ClickHouse schemas
# ---------------------------------------------------------------------------
log "=== ClickHouse ==="
CH_TABLES=$(docker exec personel-clickhouse clickhouse-client --query \
  "SELECT count() FROM system.tables WHERE database='personel'" 2>/dev/null || echo 0)
if [[ "${CH_TABLES}" -ge 5 ]]; then
  log "ClickHouse: ${CH_TABLES} tables in personel database"
  record pass clickhouse.schemas "${CH_TABLES} tables" ""
else
  err "ClickHouse: only ${CH_TABLES} tables (expected >=5)"
  record fail clickhouse.schemas "${CH_TABLES} tables" "gateway bootstraps schemas on first run"
fi

# ---------------------------------------------------------------------------
# Audit chain verification
# ---------------------------------------------------------------------------
log "=== Audit chain ==="
if [[ -x "${SCRIPTS_DIR}/verify-audit-chain.sh" ]]; then
  if "${SCRIPTS_DIR}/verify-audit-chain.sh" --latest >/dev/null 2>&1; then
    log "Audit chain latest segment verified"
    record pass audit.chain "latest segment valid" ""
  else
    err "Audit chain verification failed"
    record fail audit.chain "verification failed" "inspect audit.log + WORM sink"
  fi
else
  warn "verify-audit-chain.sh not found — skipping"
  record warn audit.chain "script missing" ""
fi

# ---------------------------------------------------------------------------
# End-to-end test (optional)
# ---------------------------------------------------------------------------
if [[ "${QUICK}" == false ]]; then
  log "=== End-to-end enroll + NATS flow ==="
  # Placeholder: real e2e requires a valid operator JWT.
  # For now we just check that /v1/endpoints/enroll endpoint is reachable.
  code=$(curl -sk -o /dev/null -w '%{http_code}' -X POST http://localhost:8000/v1/endpoints/enroll 2>/dev/null || echo 000)
  if [[ "${code}" == "401" || "${code}" == "403" ]]; then
    log "Enroll endpoint reachable (auth-gated as expected)"
    record pass e2e.enroll_reachable "auth-gated ${code}" ""
  else
    warn "Enroll endpoint HTTP ${code}"
    record warn e2e.enroll_reachable "HTTP ${code}" ""
  fi
fi

RESULTS="${RESULTS}]"
REPORT="{\"pass\":${PASS},\"warn\":${WARN},\"fail\":${FAIL},\"results\":${RESULTS}}"

if [[ -n "${REPORT_FILE}" ]]; then
  mkdir -p "$(dirname "${REPORT_FILE}")"
  echo "${REPORT}" > "${REPORT_FILE}"
  log "Report: ${REPORT_FILE}"
else
  echo "${REPORT}"
fi

echo ""
echo "====================================="
echo "  PASS: ${PASS}  WARN: ${WARN}  FAIL: ${FAIL}"
echo "====================================="

[[ "${FAIL}" -eq 0 ]] || exit 1
exit 0
