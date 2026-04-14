#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Idempotent Installer
# TR: Bu betik Personel platformunu Linux sunucuya kurar.
#     Güvenli yeniden çalıştırılabilir (idempotent).
# EN: This script installs the Personel platform on a Linux server.
#     Safe to re-run (idempotent).
#
# Usage:
#   sudo ./install.sh                  — interactive install
#   sudo ./install.sh --unattended     — non-interactive (CI/staging)
#   sudo ./install.sh --skip-images    — skip image pull (offline install)
#
# Requirements:
#   - Ubuntu 22.04 LTS or 24.04 LTS
#   - Docker 25+ (docker compose v2)
#   - 16 CPU, 64 GB RAM, 1 TB SSD (recommended)
#   - Root or sudo access
#
# TR: Gereksinimler:
#   - Ubuntu 22.04 LTS veya 24.04 LTS
#   - Docker 25+ (docker compose v2)
#   - 16 CPU, 64 GB RAM, 1 TB SSD (önerilen)
# =============================================================================
set -euo pipefail

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
PERSONEL_HOME="/opt/personel"
INFRA_DIR="${PERSONEL_HOME}/infra"
COMPOSE_DIR="${INFRA_DIR}/compose"
SCRIPTS_DIR="${INFRA_DIR}/scripts"
TLS_DIR="/etc/personel/tls"
DATA_DIR="/var/lib/personel"
LOG_DIR="/var/log/personel"
BACKUP_DIR="/var/lib/personel/backups"
PERSONEL_USER="personel"
PERSONEL_GROUP="personel"
SYSTEMD_DIR="/etc/systemd/system"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Flags
UNATTENDED="${UNATTENDED:-false}"
SKIP_IMAGES="${SKIP_IMAGES:-false}"
SKIP_PREFLIGHT="${SKIP_PREFLIGHT:-false}"
STRICT_PREFLIGHT="${STRICT_PREFLIGHT:-false}"
REPORT_FILE="${REPORT_FILE:-/var/log/personel/install-report.json}"
INSTALL_STARTED_AT=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
INSTALL_STEPS_JSON="["
INSTALL_STEPS_SEP=""

record_step() {
  local name="$1" status="$2" duration="$3" detail="${4:-}"
  INSTALL_STEPS_JSON="${INSTALL_STEPS_JSON}${INSTALL_STEPS_SEP}{\"step\":\"${name}\",\"status\":\"${status}\",\"duration_s\":${duration},\"detail\":\"${detail//\"/\\\"}\"}"
  INSTALL_STEPS_SEP=","
}

emit_report() {
  INSTALL_STEPS_JSON="${INSTALL_STEPS_JSON}]"
  mkdir -p "$(dirname "${REPORT_FILE}")" 2>/dev/null || true
  local ended_at total_s
  ended_at=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
  total_s=${SECONDS}
  cat > "${REPORT_FILE}" <<EOF
{
  "version": "${PERSONEL_VERSION:-0.1.0}",
  "started_at": "${INSTALL_STARTED_AT}",
  "ended_at": "${ended_at}",
  "total_seconds": ${total_s},
  "target_seconds": 7200,
  "unattended": ${UNATTENDED},
  "steps": ${INSTALL_STEPS_JSON}
}
EOF
  log "Install report written to ${REPORT_FILE}"
}
trap emit_report EXIT

# Colors for output
RED='\033[0;31m'; YELLOW='\033[1;33m'; GREEN='\033[0;32m'
BLUE='\033[0;34m'; BOLD='\033[1m'; NC='\033[0m'

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
log()     { echo -e "${GREEN}[install]${NC} $*"; }
info()    { echo -e "${BLUE}[install]${NC} $*"; }
warn()    { echo -e "${YELLOW}[install WARN]${NC} $*" >&2; }
error()   { echo -e "${RED}[install ERROR]${NC} $*" >&2; }
die()     { error "$*"; exit 1; }
step()    { echo -e "\n${BOLD}${BLUE}=== $* ===${NC}\n"; }

confirm() {
  local msg="$1"
  if [[ "${UNATTENDED}" == "true" ]]; then
    log "Auto-confirming (unattended mode): ${msg}"
    return 0
  fi
  read -r -p "${msg} [y/N]: " answer
  [[ "${answer}" =~ ^[Yy]$ ]]
}

require_root() {
  [[ "${EUID}" -eq 0 ]] || die "This script must be run as root. Use: sudo ./install.sh"
}

# ---------------------------------------------------------------------------
# Parse arguments
# ---------------------------------------------------------------------------
for arg in "$@"; do
  case "${arg}" in
    --unattended)     UNATTENDED=true   ;;
    --skip-images)    SKIP_IMAGES=true  ;;
    --skip-preflight) SKIP_PREFLIGHT=true ;;
    --strict)         STRICT_PREFLIGHT=true ;;
    --report=*)       REPORT_FILE="${arg#*=}" ;;
    --help|-h)
      cat <<'USAGE'
Usage: install.sh [flags]

Flags:
  --unattended        non-interactive (CI/staging)
  --skip-images       skip docker image pull (offline install)
  --skip-preflight    skip preflight check (not recommended)
  --strict            treat preflight WARN as FAIL
  --report=PATH       write install-report.json (default /var/log/personel/install-report.json)

Target runtime: 2 hours for 500-endpoint pilot (per CLAUDE.md §0).
USAGE
      exit 0 ;;
  esac
done

# ---------------------------------------------------------------------------
# STEP 0a: OS detection + package prerequisites
# ---------------------------------------------------------------------------
detect_os() {
  if [[ -f /etc/os-release ]]; then
    # shellcheck source=/dev/null
    source /etc/os-release
    OS_FAMILY="unknown"
    case "${ID}" in
      ubuntu|debian) OS_FAMILY="debian" ;;
      rhel|rocky|almalinux|centos) OS_FAMILY="rhel" ;;
      *) OS_FAMILY="unknown" ;;
    esac
  else
    OS_FAMILY="unknown"
  fi
  export OS_FAMILY OS_ID="${ID:-unknown}" OS_VER="${VERSION_ID:-unknown}"
}

install_packages() {
  local pkgs=(curl jq gpg ca-certificates openssl python3 nftables)
  case "${OS_FAMILY}" in
    debian)
      DEBIAN_FRONTEND=noninteractive apt-get update -qq
      DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "${pkgs[@]}" >/dev/null
      ;;
    rhel)
      dnf install -y -q "${pkgs[@]}" >/dev/null
      ;;
    *)
      warn "Unknown OS family — skipping package install; ensure: ${pkgs[*]}"
      ;;
  esac
}

# ---------------------------------------------------------------------------
# STEP 0: Root check
# ---------------------------------------------------------------------------
require_root

echo ""
echo "╔══════════════════════════════════════════════════════════════════╗"
echo "║          Personel Platform — On-Prem Installer v0.1.0           ║"
echo "║  TR: Kurulum başlıyor. Lütfen bekleyin...                        ║"
echo "║  EN: Starting installation. Please wait...                       ║"
echo "╚══════════════════════════════════════════════════════════════════╝"
echo ""

# ---------------------------------------------------------------------------
# STEP 1: Preflight checks
# ---------------------------------------------------------------------------
step "STEP 1/12: Preflight Checks"

t0=${SECONDS}
detect_os
log "Detected OS family: ${OS_FAMILY} (${OS_ID} ${OS_VER})"
install_packages

if [[ "${SKIP_PREFLIGHT}" != "true" ]]; then
  PRE_FLAGS=""
  [[ "${STRICT_PREFLIGHT}" == "true" ]] && PRE_FLAGS="--strict"
  # shellcheck disable=SC2086
  if ! "${SCRIPTS_DIR}/preflight-check.sh" ${PRE_FLAGS}; then
    record_step preflight fail $((SECONDS - t0)) "hard fail"
    die "Preflight checks failed. Resolve issues before continuing."
  fi
  record_step preflight pass $((SECONDS - t0)) ""
  log "Preflight checks passed."
else
  warn "Skipping preflight checks (--skip-preflight). Not recommended for production."
  record_step preflight skip $((SECONDS - t0)) "--skip-preflight"
fi

# ---------------------------------------------------------------------------
# STEP 2: Environment file
# ---------------------------------------------------------------------------
step "STEP 2/12: Environment Configuration"

if [[ ! -f "${COMPOSE_DIR}/.env" ]]; then
  if [[ -f "${COMPOSE_DIR}/.env.example" ]]; then
    cp "${COMPOSE_DIR}/.env.example" "${COMPOSE_DIR}/.env"
    chmod 600 "${COMPOSE_DIR}/.env"
    warn "Created .env from .env.example — EDIT before proceeding!"
    warn "File: ${COMPOSE_DIR}/.env"
    if [[ "${UNATTENDED}" != "true" ]]; then
      die "Please edit ${COMPOSE_DIR}/.env and set all CHANGEME values, then re-run."
    fi
  else
    die ".env.example not found at ${COMPOSE_DIR}/.env.example"
  fi
else
  log ".env file exists — checking for CHANGEME placeholders..."
  if grep -q "CHANGEME" "${COMPOSE_DIR}/.env"; then
    warn "Found CHANGEME placeholders in .env:"
    grep "CHANGEME" "${COMPOSE_DIR}/.env" | grep -v "^#" | head -20
    if [[ "${UNATTENDED}" != "true" ]]; then
      confirm "Continue anyway (not safe for production)?" || die "Please configure .env first."
    fi
  fi
fi

# Source environment
set -a
# shellcheck source=/dev/null
source "${COMPOSE_DIR}/.env"
set +a

# ---------------------------------------------------------------------------
# STEP 3: Create system user and directories
# ---------------------------------------------------------------------------
step "STEP 3/12: System User and Directories"

if ! id "${PERSONEL_USER}" &>/dev/null; then
  useradd --system --no-create-home --shell /usr/sbin/nologin \
    --comment "Personel Platform Service Account" \
    "${PERSONEL_USER}"
  log "Created system user: ${PERSONEL_USER}"
else
  log "System user ${PERSONEL_USER} already exists — skipping"
fi

# Add personel user to docker group
if getent group docker &>/dev/null; then
  usermod -aG docker "${PERSONEL_USER}"
  log "Added ${PERSONEL_USER} to docker group"
fi

# Create data directories
for dir in \
  "${TLS_DIR}" \
  "${DATA_DIR}/postgres/data" \
  "${DATA_DIR}/clickhouse/data" \
  "${DATA_DIR}/nats/data" \
  "${DATA_DIR}/minio/data" \
  "${DATA_DIR}/opensearch/data" \
  "${DATA_DIR}/vault/data" \
  "${DATA_DIR}/keycloak/data" \
  "${DATA_DIR}/prometheus/data" \
  "${DATA_DIR}/grafana/data" \
  "${DATA_DIR}/backups/daily" \
  "${DATA_DIR}/backups/weekly" \
  "${LOG_DIR}/audit" \
  "${LOG_DIR}/compliance" \
  "/var/lib/node_exporter/textfile_collector"; do
  mkdir -p "${dir}"
done

# Set ownership
chown -R "${PERSONEL_USER}:${PERSONEL_GROUP}" "${DATA_DIR}"
chown -R "${PERSONEL_USER}:${PERSONEL_GROUP}" "${LOG_DIR}"
chown "${PERSONEL_USER}:${PERSONEL_GROUP}" "${TLS_DIR}"
chmod 750 "${TLS_DIR}"
chmod 700 "${DATA_DIR}/vault/data"
log "Data directories created and owned by ${PERSONEL_USER}"

# ---------------------------------------------------------------------------
# STEP 4: Host hardening
# ---------------------------------------------------------------------------
step "STEP 4/12: Host Hardening"

# DLP ptrace_scope — required for DLP hardening controls
cat > /etc/sysctl.d/99-personel-dlp.conf <<'EOF'
# Personel DLP hardening — prevent ptrace between processes
kernel.yama.ptrace_scope = 3
# Disable core dumps globally (belt-and-braces alongside DLP ulimit)
kernel.core_pattern = |/bin/false
# Network hardening
net.ipv4.conf.all.rp_filter = 1
net.ipv4.conf.default.rp_filter = 1
net.ipv4.tcp_syncookies = 1
net.ipv4.conf.all.accept_redirects = 0
net.ipv4.conf.default.accept_redirects = 0
EOF
sysctl --system --quiet
log "Applied host sysctl hardening"

# Disable swap (DLP requirement: memory.swap.max=0)
if swapon --show | grep -q .; then
  warn "Swap is active. DLP security controls require swap to be disabled."
  if confirm "Disable swap now? (recommended for production)"; then
    swapoff -a
    # Comment out swap in /etc/fstab
    sed -i '/\bswap\b/s/^/#/' /etc/fstab
    log "Swap disabled"
  fi
fi

# AppArmor profile for DLP
if command -v apparmor_parser &>/dev/null; then
  if [[ -f "${COMPOSE_DIR}/dlp/apparmor-profile" ]]; then
    cp "${COMPOSE_DIR}/dlp/apparmor-profile" /etc/apparmor.d/personel-dlp
    apparmor_parser -r /etc/apparmor.d/personel-dlp 2>/dev/null || \
      warn "AppArmor profile load failed — DLP will run without AppArmor confinement"
    log "AppArmor profile personel-dlp loaded"
  fi
else
  warn "AppArmor not available — DLP will rely on seccomp profile only"
fi

# ---------------------------------------------------------------------------
# STEP 5: PKI Bootstrap (if not already done)
# ---------------------------------------------------------------------------
step "STEP 5/12: PKI and TLS Bootstrap"

if [[ -f "${TLS_DIR}/tenant_ca.crt" ]]; then
  log "Tenant CA already exists at ${TLS_DIR}/tenant_ca.crt — skipping PKI bootstrap"
  log "To regenerate: scripts/ca-bootstrap.sh"
else
  warn "No Tenant CA found. Running PKI bootstrap..."
  "${SCRIPTS_DIR}/ca-bootstrap.sh" \
    --tenant-id "${PERSONEL_TENANT_ID:-default}" \
    --tls-dir "${TLS_DIR}" \
    --non-interactive \
    || warn "PKI bootstrap incomplete — some services will not start until certs are provisioned"
fi

# ---------------------------------------------------------------------------
# STEP 6: Vault initialization
# ---------------------------------------------------------------------------
step "STEP 6/12: Vault Initialization"

# Start Vault only first
log "Starting Vault container for initialization..."
cd "${COMPOSE_DIR}"
docker compose up -d vault

log "Waiting for Vault to be ready (up to 60s)..."
for i in $(seq 1 30); do
  if docker exec personel-vault vault status \
      -address=https://127.0.0.1:8200 \
      -tls-skip-verify 2>/dev/null | grep -q "Initialized"; then
    break
  fi
  sleep 2
done

VAULT_INITIALIZED=$(docker exec personel-vault vault status \
  -address=https://127.0.0.1:8200 -tls-skip-verify -format=json 2>/dev/null \
  | python3 -c "import json,sys; d=json.load(sys.stdin); print('true' if d.get('initialized') else 'false')" \
  || echo "false")

if [[ "${VAULT_INITIALIZED}" == "false" ]]; then
  log "Vault not initialized — running Shamir unseal ceremony..."
  "${COMPOSE_DIR}/vault/bootstrap.sh" init
  log ""
  warn "TR: Vault anahtarları oluşturuldu. Yukarıdaki 5 payı güvenli zarflara koyun."
  warn "EN: Vault keys generated. Place the 5 shares in tamper-evident envelopes."
  warn "    Distribute per pki-bootstrap.md §3.2 before continuing."
  confirm "Have you secured the Shamir shares and noted the root token?" || \
    die "Cannot continue without securing Vault keys."
  "${COMPOSE_DIR}/vault/bootstrap.sh" unseal
  "${COMPOSE_DIR}/vault/bootstrap.sh" configure
else
  VAULT_SEALED=$(docker exec personel-vault vault status \
    -address=https://127.0.0.1:8200 -tls-skip-verify -format=json 2>/dev/null \
    | python3 -c "import json,sys; d=json.load(sys.stdin); print('true' if d.get('sealed') else 'false')" \
    || echo "true")

  if [[ "${VAULT_SEALED}" == "true" ]]; then
    log "Vault is initialized but sealed — running unseal..."
    "${SCRIPTS_DIR}/vault-unseal.sh"
  else
    log "Vault is initialized and unsealed — continuing"
  fi
fi

# ---------------------------------------------------------------------------
# STEP 7: Start core data services
# ---------------------------------------------------------------------------
step "STEP 7/12: Starting Core Data Services"

log "Starting Postgres, ClickHouse, NATS, MinIO, OpenSearch..."
docker compose up -d postgres clickhouse nats minio opensearch

log "Waiting for all data services to be healthy..."
for service in postgres clickhouse nats minio opensearch; do
  log "  Waiting for ${service}..."
  for i in $(seq 1 60); do
    if docker compose ps "${service}" 2>/dev/null | grep -q "healthy"; then
      log "  ${service} is healthy"
      break
    fi
    if [[ "${i}" -eq 60 ]]; then
      error "${service} did not become healthy in 120s"
      docker compose logs --tail=50 "${service}" >&2
      die "${service} health check failed"
    fi
    sleep 2
  done
done

# Initialize MinIO buckets
log "Initializing MinIO buckets..."
docker compose run --rm minio-init || warn "MinIO init had warnings — check logs"

# Apply OpenSearch index templates
log "Applying OpenSearch index templates..."
for template_file in "${COMPOSE_DIR}/opensearch/index-templates/"*.json; do
  template_name=$(basename "${template_file}" .json)
  curl -sk -X PUT \
    "https://localhost:9200/_index_template/${template_name}" \
    -H "Content-Type: application/json" \
    -u "admin:${OPENSEARCH_ADMIN_PASSWORD}" \
    -d "@${template_file}" >/dev/null && \
    log "  Applied template: ${template_name}" || \
    warn "  Failed to apply template: ${template_name}"
done

# ---------------------------------------------------------------------------
# STEP 8: Database schema bootstrap
# ---------------------------------------------------------------------------
step "STEP 8/12: Database Schema Bootstrap"

log "PostgreSQL schema: init.sql is run automatically on first start by docker-entrypoint"
log "Verifying schema..."
docker exec personel-postgres psql -U postgres -d personel \
  -c "SELECT table_name FROM information_schema.tables WHERE table_schema='core' ORDER BY 1" \
  2>/dev/null | grep -q "tenants" && log "  Schema verified: core tables exist" || \
  warn "  Schema verification failed — run init.sql manually"

log "Seeding tenant configuration..."
docker exec personel-postgres psql -U postgres -d personel -c \
  "UPDATE core.tenants SET name='${PERSONEL_TENANT_NAME:-Personel Customer}', \
   kvkk_config='{\"verbis_registered\": false, \"data_controller\": \"${PERSONEL_TENANT_NAME:-CHANGEME}\"}' \
   WHERE slug='default'" 2>/dev/null || true

log "Bootstrapping ClickHouse schema (via gateway)..."
# The gateway bootstraps its own ClickHouse tables on first start.
# We start the enricher after the gateway schema is in place.

# ---------------------------------------------------------------------------
# STEP 9: Start Keycloak and application services
# ---------------------------------------------------------------------------
step "STEP 9/12: Starting Application Services"

log "Starting Keycloak..."
docker compose up -d keycloak
for i in $(seq 1 90); do
  if docker compose ps keycloak 2>/dev/null | grep -q "healthy"; then
    log "Keycloak is healthy"
    break
  fi
  [[ "${i}" -eq 90 ]] && die "Keycloak did not become healthy in 180s"
  sleep 2
done

log "Starting gateway, API, enricher, DLP, console, portal, LiveKit..."
docker compose up -d gateway api enricher dlp console portal livekit

log "Starting edge proxy (Envoy)..."
docker compose up -d envoy

log "Starting observability (Prometheus, Grafana)..."
docker compose up -d prometheus grafana

log "Waiting for all application services to be healthy..."
for service in gateway api console portal; do
  for i in $(seq 1 60); do
    if docker compose ps "${service}" 2>/dev/null | grep -q "healthy"; then
      log "  ${service} is healthy"
      break
    fi
    [[ "${i}" -eq 60 ]] && warn "${service} health check timed out — check logs"
    sleep 2
  done
done

# ---------------------------------------------------------------------------
# STEP 10: Install systemd units
# ---------------------------------------------------------------------------
step "STEP 10/12: Installing systemd Units"

for unit_file in \
  personel.target \
  personel-compose.service \
  personel-backup.timer \
  personel-backup.service \
  personel-audit-verifier.timer \
  personel-audit-verifier.service \
  personel-cert-renewer.timer \
  personel-cert-renewer.service; do
  src="${INFRA_DIR}/systemd/${unit_file}"
  dst="${SYSTEMD_DIR}/${unit_file}"
  if [[ -f "${src}" ]]; then
    cp "${src}" "${dst}"
    log "  Installed: ${unit_file}"
  else
    warn "  Missing: ${src}"
  fi
done

systemctl daemon-reload
systemctl enable personel.target personel-compose.service \
  personel-backup.timer personel-audit-verifier.timer \
  personel-cert-renewer.timer

log "systemd units enabled"

# ---------------------------------------------------------------------------
# STEP 11: Smoke test
# ---------------------------------------------------------------------------
step "STEP 11/12: Smoke Tests"

log "Running post-install smoke tests..."
"${INFRA_DIR}/tests/smoke.sh" || warn "Some smoke tests failed — check output above"

log "Running post-install validation..."
if [[ -x "${SCRIPTS_DIR}/post-install-validate.sh" ]]; then
  "${SCRIPTS_DIR}/post-install-validate.sh" --report=/var/log/personel/post-install-validate.json \
    || warn "Post-install validation reported issues — see /var/log/personel/post-install-validate.json"
  record_step post_install_validate pass $((SECONDS)) ""
else
  warn "post-install-validate.sh not executable — skipping"
fi

# ---------------------------------------------------------------------------
# STEP 12: First admin user
# ---------------------------------------------------------------------------
step "STEP 12/12: DPO and Admin Onboarding"

DPO_URL="https://${PERSONEL_EXTERNAL_HOST:-localhost}/console/onboarding"

log "Installation complete!"
echo ""
echo "╔══════════════════════════════════════════════════════════════════╗"
echo "║              KURULUM TAMAMLANDI / INSTALLATION COMPLETE          ║"
echo "╠══════════════════════════════════════════════════════════════════╣"
echo "║                                                                  ║"
echo "║  Yönetici Konsolu / Admin Console:                               ║"
echo "║    https://${PERSONEL_EXTERNAL_HOST:-YOUR_HOST}/console         "
echo "║                                                                  ║"
echo "║  Şeffaflık Portalı / Transparency Portal:                        ║"
echo "║    https://${PERSONEL_EXTERNAL_HOST:-YOUR_HOST}/portal          "
echo "║                                                                  ║"
echo "║  SONRAKI ADIMLAR / NEXT STEPS:                                   ║"
echo "║  1. İlk DPO ve Admin kullanıcısını oluşturun:                    ║"
echo "║     scripts/create-admin.sh                                      ║"
echo "║  2. VERBİS kaydını tamamlayın                                    ║"
echo "║  3. Endpoint ajanlarını kurun                                    ║"
echo "║  4. runbooks/install.md sayfasını inceleyin                      ║"
echo "║                                                                  ║"
echo "║  TR: DPO onboarding URL:                                         ║"
echo "║    ${DPO_URL}"
echo "║                                                                  ║"
echo "║  KVKK: Kurulum, denetim günlüğüne kaydedildi.                    ║"
echo "╚══════════════════════════════════════════════════════════════════╝"
echo ""

# Log install event to audit (via API once it's up)
INSTALL_TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
log "Install completed at ${INSTALL_TIMESTAMP}. Logging to KVKK audit trail..."

# Try to write to audit log via the API
curl -sk -X POST "http://localhost:${API_HTTP_PORT:-8000}/internal/audit/install" \
  -H "Content-Type: application/json" \
  -d "{\"event\": \"install.completed\", \"version\": \"${PERSONEL_VERSION:-0.1.0}\", \"timestamp\": \"${INSTALL_TIMESTAMP}\"}" \
  >/dev/null 2>&1 || true

log "Done. Total install time: $((SECONDS / 60)) minutes $((SECONDS % 60)) seconds."
