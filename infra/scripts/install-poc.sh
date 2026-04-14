#!/usr/bin/env bash
# =============================================================================
# Personel Platform — POC Installer Wrapper
# TR: 30 günlük POC ortamı için optimize edilmiş kurulum scripti.
#     Standart install.sh'i çağırır ama POC'a özgü ayarları override eder:
#     demo creds, showcase seed data, trial lisans üretimi, daha az container.
# EN: POC-optimized installer wrapper. Invokes the standard install.sh with
#     POC-specific overrides: demo credentials, showcase seed data, trial
#     license generation, lighter container footprint.
#
# Usage:
#   sudo ./install-poc.sh --endpoints=50           # 50 endpoint POC
#   sudo ./install-poc.sh --endpoints=100 --uba    # with UBA module
#   sudo ./install-poc.sh --airgapped              # offline install
#   sudo ./install-poc.sh --dry-run                # show what would run
#
# POC deviations from production install.sh:
#   - admin/admin123 demo Keycloak credentials (MUST be changed first login)
#   - service_started dependencies (faster startup, less robust)
#   - No Vault auto-unseal — operator sees the unseal keys on screen once
#   - 7-day log retention (vs 30 day prod)
#   - Trial license auto-generated (30 days, configured endpoint cap)
#   - Showcase seed data populated for demo screens
#   - Pre-flight skips "server class hardware" check (POC can run on dev laptops)
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# Defaults
POC_ENDPOINTS=50
POC_ENABLE_UBA=0
POC_ENABLE_OCR=0
POC_AIRGAPPED=0
POC_DRY_RUN=0
POC_LICENSE_DAYS=30

# Parse args
while [[ $# -gt 0 ]]; do
    case "$1" in
        --endpoints=*)
            POC_ENDPOINTS="${1#*=}"
            shift
            ;;
        --uba)
            POC_ENABLE_UBA=1
            shift
            ;;
        --ocr)
            POC_ENABLE_OCR=1
            shift
            ;;
        --airgapped)
            POC_AIRGAPPED=1
            shift
            ;;
        --dry-run)
            POC_DRY_RUN=1
            shift
            ;;
        --help|-h)
            sed -n '2,26p' "$0"
            exit 0
            ;;
        *)
            echo "Unknown option: $1" >&2
            exit 1
            ;;
    esac
done

log() { echo -e "\033[1;34m[POC-INSTALL]\033[0m $*"; }
warn() { echo -e "\033[1;33m[POC-WARN]\033[0m $*" >&2; }
err() { echo -e "\033[1;31m[POC-ERR]\033[0m $*" >&2; }

# Safety: only run as root
if [[ "$EUID" -ne 0 ]]; then
    err "POC install must be run as root (use sudo)."
    exit 1
fi

log "Personel POC Installer"
log "  Endpoints: ${POC_ENDPOINTS}"
log "  UBA module: $([[ $POC_ENABLE_UBA -eq 1 ]] && echo yes || echo no)"
log "  OCR module: $([[ $POC_ENABLE_OCR -eq 1 ]] && echo yes || echo no)"
log "  Airgapped: $([[ $POC_AIRGAPPED -eq 1 ]] && echo yes || echo no)"
log "  Dry run: $([[ $POC_DRY_RUN -eq 1 ]] && echo yes || echo no)"

# ---------------------------------------------------------------------------
# Pre-flight (POC-lenient version)
# ---------------------------------------------------------------------------
log "Pre-flight checks..."

# Check CPU cores (POC: 4 min, prod: 8)
CPU_CORES=$(nproc)
if [[ $CPU_CORES -lt 4 ]]; then
    err "POC requires at least 4 CPU cores (found $CPU_CORES)"
    exit 1
fi

# Check RAM (POC: 8GB min, prod: 16GB)
MEM_GB=$(awk '/MemTotal/ {print int($2/1024/1024)}' /proc/meminfo)
if [[ $MEM_GB -lt 8 ]]; then
    err "POC requires at least 8 GB RAM (found ${MEM_GB} GB)"
    exit 1
fi

# Check disk (100GB min)
DISK_GB=$(df -BG --output=avail /var/lib 2>/dev/null | tail -1 | tr -d 'G' | tr -d ' ')
if [[ ${DISK_GB:-0} -lt 100 ]]; then
    warn "POC recommends 100+ GB free disk space (found ${DISK_GB} GB)"
fi

# Check Docker
if ! command -v docker &>/dev/null; then
    err "Docker not installed. Install first: curl -fsSL https://get.docker.com | sh"
    exit 1
fi

if ! docker compose version &>/dev/null; then
    err "Docker Compose v2 plugin not installed"
    exit 1
fi

log "Pre-flight OK"

if [[ $POC_DRY_RUN -eq 1 ]]; then
    log "Dry run requested — would invoke:"
    log "  ${REPO_ROOT}/infra/install.sh --profile poc --endpoints ${POC_ENDPOINTS}"
    exit 0
fi

# ---------------------------------------------------------------------------
# Generate POC .env
# ---------------------------------------------------------------------------
log "Generating POC .env..."
ENV_FILE="${REPO_ROOT}/infra/compose/.env"
if [[ -f "$ENV_FILE" ]]; then
    cp "$ENV_FILE" "${ENV_FILE}.backup.$(date +%s)"
fi

# POC uses weak default passwords intentionally — documented and forced-change
cat > "$ENV_FILE" <<EOF
# Personel POC Environment — generated $(date -Iseconds)
# WARNING: POC default credentials. Change all CHANGEME on first login.
DEPLOYMENT_MODE=poc
DEPLOYMENT_ENDPOINTS=${POC_ENDPOINTS}
LICENSE_TIER=poc-trial

# Keycloak POC admin (MUST change on first login)
KEYCLOAK_ADMIN=admin
KEYCLOAK_ADMIN_PASSWORD=admin123

# Postgres app accounts
POSTGRES_PASSWORD=pocpass123
APP_ADMIN_API_PASSWORD=pocapipass
PERSONEL_ENRICHER_PASSWORD=pocenricherpass
PERSONEL_GW_PASSWORD=pocgwpass

# ClickHouse
CLICKHOUSE_ADMIN_PASSWORD=pocchpass
CLICKHOUSE_APP_PASSWORD=pocchapppass

# MinIO
MINIO_ROOT_USER=pocminio
MINIO_ROOT_PASSWORD=pocminiopass

# NATS (POC: no auth, token-based in prod)
NATS_AUTH_MODE=none

# Feature modules (POC opt-in)
POC_ENABLE_UBA=${POC_ENABLE_UBA}
POC_ENABLE_OCR=${POC_ENABLE_OCR}

# Log retention (POC: 7 days, prod: 30 days)
LOG_RETENTION_DAYS=7
EOF

chmod 0600 "$ENV_FILE"
log "POC .env written to ${ENV_FILE}"

# ---------------------------------------------------------------------------
# Invoke standard installer with POC profile
# ---------------------------------------------------------------------------
log "Invoking standard install.sh with POC profile..."
bash "${REPO_ROOT}/infra/install.sh" --profile poc || {
    err "install.sh failed — check /var/log/personel/install.log"
    exit 1
}

# ---------------------------------------------------------------------------
# Generate trial license
# ---------------------------------------------------------------------------
log "Generating ${POC_LICENSE_DAYS}-day trial license..."
if [[ -x "${REPO_ROOT}/infra/scripts/gen-trial-license.sh" ]]; then
    "${REPO_ROOT}/infra/scripts/gen-trial-license.sh" \
        --customer-id "poc-$(hostname)" \
        --max-endpoints "$POC_ENDPOINTS" \
        --days "$POC_LICENSE_DAYS" \
        --output /etc/personel/license.json
    log "Trial license written to /etc/personel/license.json"
else
    warn "gen-trial-license.sh missing — API will run without license check (dev mode)"
fi

# ---------------------------------------------------------------------------
# Load showcase seed data
# ---------------------------------------------------------------------------
log "Loading showcase seed data for POC demo..."
if [[ -x "${REPO_ROOT}/infra/scripts/dev-seed-showcase.sh" ]]; then
    "${REPO_ROOT}/infra/scripts/dev-seed-showcase.sh" || warn "seed-showcase failed (non-fatal)"
else
    warn "dev-seed-showcase.sh missing — Console will start empty"
fi

# ---------------------------------------------------------------------------
# Final summary
# ---------------------------------------------------------------------------
HOSTNAME_FQDN=$(hostname -f 2>/dev/null || hostname)
log ""
log "======================================================================"
log " Personel POC Installation Complete"
log "======================================================================"
log " Admin Console : https://${HOSTNAME_FQDN}/console/tr/dashboard"
log " Employee Portal: https://${HOSTNAME_FQDN}/portal/tr/verilerim"
log " Grafana       : https://${HOSTNAME_FQDN}/grafana"
log ""
log " Keycloak admin  : admin / admin123  (CHANGE ON FIRST LOGIN)"
log " Endpoints cap   : ${POC_ENDPOINTS}"
log " License expires : $(date -d "+${POC_LICENSE_DAYS} days" -Iseconds 2>/dev/null || date -v +${POC_LICENSE_DAYS}d -Iseconds 2>/dev/null || echo "${POC_LICENSE_DAYS} days from now")"
log ""
log " Next steps:"
log "   1. Log in, change admin password"
log "   2. Console → Endpoints → 'Token Oluştur' → enroll first Windows host"
log "   3. Review: docs/sales/poc-guide.md"
log ""
log " Cleanup when done: sudo ./infra/scripts/poc-teardown.sh"
log "======================================================================"
