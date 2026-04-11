#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Canary-Aware Upgrade Script
# TR: Sağlıklı servis koşullu yükseltme. Başarısızlıkta otomatik geri alma.
# EN: Health-gated upgrade with automatic rollback on failure.
#
# Usage:
#   ./upgrade.sh --version 0.2.0
#   ./upgrade.sh --version 0.2.0 --service api  (single service upgrade)
#   ./upgrade.sh --rollback                      (rollback to previous version)
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_DIR="${SCRIPT_DIR}/compose"

set -a; source "${COMPOSE_DIR}/.env"; set +a

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'; BOLD='\033[1m'
log()  { echo -e "${GREEN}[upgrade]${NC} $*"; }
warn() { echo -e "${YELLOW}[upgrade WARN]${NC} $*" >&2; }
die()  { echo -e "${RED}[upgrade ERROR]${NC} $*" >&2; exit 1; }

NEW_VERSION=""
ROLLBACK=false
SERVICE_FILTER=""
HEALTH_WAIT=120

for arg in "$@"; do
  case "${arg}" in
    --version=*)   NEW_VERSION="${arg#--version=}" ;;
    --service=*)   SERVICE_FILTER="${arg#--service=}" ;;
    --rollback)    ROLLBACK=true ;;
  esac
done

PREV_VERSION="${PERSONEL_VERSION:-0.1.0}"
STATE_FILE="${COMPOSE_DIR}/.upgrade-state"

# ---------------------------------------------------------------------------
wait_healthy() {
  local service="$1"
  local timeout="${2:-${HEALTH_WAIT}}"
  log "Waiting for ${service} to be healthy (timeout: ${timeout}s)..."
  local elapsed=0
  while [[ "${elapsed}" -lt "${timeout}" ]]; do
    if docker compose ps "${service}" 2>/dev/null | grep -q "healthy"; then
      log "  ${service} is healthy"
      return 0
    fi
    sleep 5; ((elapsed+=5))
  done
  return 1
}

# ---------------------------------------------------------------------------
if [[ "${ROLLBACK}" == "true" ]]; then
  [[ -f "${STATE_FILE}" ]] || die "No upgrade state file found. Cannot rollback."
  ROLLBACK_VERSION=$(cat "${STATE_FILE}")
  log "Rolling back to version: ${ROLLBACK_VERSION}"
  sed -i "s/^PERSONEL_VERSION=.*/PERSONEL_VERSION=${ROLLBACK_VERSION}/" "${COMPOSE_DIR}/.env"
  set -a; source "${COMPOSE_DIR}/.env"; set +a
  cd "${COMPOSE_DIR}"
  docker compose up -d --no-build
  log "Rollback to ${ROLLBACK_VERSION} complete."
  rm -f "${STATE_FILE}"
  exit 0
fi

[[ -n "${NEW_VERSION}" ]] || die "Usage: $0 --version X.Y.Z"

echo ""
echo -e "${BOLD}=== Personel Platform Upgrade: ${PREV_VERSION} → ${NEW_VERSION} ===${NC}"
echo ""

# Save current version for rollback
echo "${PREV_VERSION}" > "${STATE_FILE}"

# Update version in .env
sed -i "s/^PERSONEL_VERSION=.*/PERSONEL_VERSION=${NEW_VERSION}/" "${COMPOSE_DIR}/.env"
set -a; source "${COMPOSE_DIR}/.env"; set +a

cd "${COMPOSE_DIR}"

# Pull new images
log "Pulling new images (version ${NEW_VERSION})..."
if [[ -n "${SERVICE_FILTER}" ]]; then
  docker compose pull "${SERVICE_FILTER}"
else
  docker compose pull
fi

# ---------------------------------------------------------------------------
# Ordered rolling upgrade (preserving data service stability)
# ---------------------------------------------------------------------------
SERVICES=(gateway api enricher dlp console portal livekit envoy)
if [[ -n "${SERVICE_FILTER}" ]]; then
  SERVICES=("${SERVICE_FILTER}")
fi

for service in "${SERVICES[@]}"; do
  log "Upgrading service: ${service}..."
  docker compose up -d --no-build --no-deps "${service}"

  if ! wait_healthy "${service}" "${HEALTH_WAIT}"; then
    warn "Service ${service} failed health check after upgrade!"
    log "Initiating automatic rollback to ${PREV_VERSION}..."
    sed -i "s/^PERSONEL_VERSION=.*/PERSONEL_VERSION=${PREV_VERSION}/" "${COMPOSE_DIR}/.env"
    set -a; source "${COMPOSE_DIR}/.env"; set +a
    docker compose up -d --no-build --no-deps "${service}"
    if wait_healthy "${service}" 60; then
      log "Rollback to ${PREV_VERSION} successful for ${service}"
    else
      die "Rollback also failed for ${service}! Manual intervention required."
    fi
    die "Upgrade aborted at service ${service}. Rolled back to ${PREV_VERSION}."
  fi
done

# Run smoke tests
log "Running smoke tests..."
"${SCRIPT_DIR}/tests/smoke.sh" || {
  warn "Smoke tests failed after upgrade!"
  log "Initiating rollback..."
  ./upgrade.sh --rollback
  die "Upgrade failed smoke tests. Rolled back."
}

# Success — clean up rollback state
rm -f "${STATE_FILE}"

log "Upgrade to ${NEW_VERSION} complete."
echo ""
echo -e "${GREEN}${BOLD}=== Upgrade Successful ===${NC}"
echo "  Previous: ${PREV_VERSION}"
echo "  Current:  ${NEW_VERSION}"
