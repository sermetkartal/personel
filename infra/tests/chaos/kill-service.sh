#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Chaos Drill: Kill Service and Verify Recovery
# TR: Servis çökmesi senaryolarını test eder.
# EN: Tests service crash scenarios and automatic recovery.
#
# Usage:
#   ./kill-service.sh --service api
#   ./kill-service.sh --service dlp  (verify agents keep queuing)
#   ./kill-service.sh --service nats (verify agent offline buffer)
# =============================================================================
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_DIR="${SCRIPT_DIR}/../../compose"
set -a; source "${COMPOSE_DIR}/.env" 2>/dev/null || true; set +a

SERVICE=""
for arg in "$@"; do
  case "${arg}" in
    --service=*) SERVICE="${arg#--service=}" ;;
  esac
done

[[ -n "${SERVICE}" ]] || { echo "Usage: $0 --service SERVICE_NAME"; exit 1; }

pass() { echo -e "\033[0;32m[PASS]\033[0m $*"; }
fail() { echo -e "\033[0;31m[FAIL]\033[0m $*" >&2; }
log()  { echo "[chaos-kill] $*"; }

log "=== Chaos Drill: Kill ${SERVICE} ==="

BEFORE_COUNT=$(docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" ps "${SERVICE}" \
  2>/dev/null | grep -c "running\|healthy" || echo "0")
[[ "${BEFORE_COUNT}" -gt 0 ]] || { fail "${SERVICE} not running — cannot test"; exit 1; }

log "Killing container: personel-${SERVICE}"
docker kill "personel-${SERVICE}" 2>/dev/null || true
KILL_TIME=$(date +%s)

# Wait for recovery
RECOVERY_TIMEOUT=120
RECOVERED=false
for i in $(seq 1 $((RECOVERY_TIMEOUT / 5))); do
  STATUS=$(docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" ps "${SERVICE}" 2>/dev/null \
    | grep -oE '(healthy|running)' | head -1 || echo "")
  if [[ "${STATUS}" == "healthy" ]] || [[ "${STATUS}" == "running" ]]; then
    RECOVERY_TIME=$(($(date +%s) - KILL_TIME))
    RECOVERED=true
    break
  fi
  sleep 5
done

if [[ "${RECOVERED}" == "true" ]]; then
  pass "Service ${SERVICE} recovered in ${RECOVERY_TIME}s (restart policy working)"
else
  fail "Service ${SERVICE} did NOT recover within ${RECOVERY_TIMEOUT}s"
fi

log "Chaos drill complete for ${SERVICE}."
# TODO: Per-service validation (e.g., for DLP: verify NATS backlog accumulated and drained after recovery)
