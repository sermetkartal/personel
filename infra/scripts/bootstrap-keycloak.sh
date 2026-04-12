#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Keycloak Realm Provisioning
# TR: realm-personel.json'daki tüm CHANGEME_* değerlerini gerçek secret'larla
#     değiştirir. Çıktı: realm-personel-provisioned.json (gitignore'da).
#     Client secret'lar .env'ye append edilir.
# EN: Replaces all CHANGEME_* placeholders in realm-personel.json with real
#     secrets. Output: realm-personel-provisioned.json (gitignored).
#     Client secrets are appended to .env.
#
# Usage:
#   ./bootstrap-keycloak.sh                          — auto-generate all secrets
#   ./bootstrap-keycloak.sh --tenant-id <UUID>       — use specific tenant UUID
#   ./bootstrap-keycloak.sh --host console.acme.com  — override external host
#   ./bootstrap-keycloak.sh --dry-run                — print diff without writing
#
# Prerequisites:
#   • infra/compose/.env must exist (run bootstrap-env.sh first)
#   • python3 OR jq available for JSON processing
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_DIR="${SCRIPT_DIR}/../compose"
KEYCLOAK_DIR="${COMPOSE_DIR}/keycloak"
TEMPLATE_FILE="${KEYCLOAK_DIR}/realm-personel.json"
OUTPUT_FILE="${KEYCLOAK_DIR}/realm-personel-provisioned.json"
ENV_FILE="${COMPOSE_DIR}/.env"

# ---------------------------------------------------------------------------
# Flag parsing
# ---------------------------------------------------------------------------
DRY_RUN=false
TENANT_ID_ARG=""
HOST_ARG=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)      DRY_RUN=true; shift ;;
    --tenant-id)    TENANT_ID_ARG="${2:?'--tenant-id requires a value'}"; shift 2 ;;
    --host)         HOST_ARG="${2:?'--host requires a value'}"; shift 2 ;;
    --help|-h)
      sed -n '2,22p' "${BASH_SOURCE[0]}" | sed 's/^# \?//'
      exit 0
      ;;
    *)
      echo "[bootstrap-keycloak] ERROR: Unknown flag: $1" >&2
      exit 1
      ;;
  esac
done

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
log()  { echo "[bootstrap-keycloak] $*"; }
warn() { echo "[bootstrap-keycloak] WARN: $*" >&2; }
die()  { echo "[bootstrap-keycloak] ERROR: $*" >&2; exit 1; }

rand_alnum() {
  local len="${1:-32}"
  LC_ALL=C tr -dc 'A-Za-z0-9' < /dev/urandom | head -c "${len}"
}

rand_hex_16() {
  openssl rand -hex 16
}

rand_uuid() {
  local hex
  hex="$(openssl rand -hex 16)"
  printf '%s-%s-4%s-%x%s-%s\n' \
    "${hex:0:8}" \
    "${hex:8:4}" \
    "${hex:13:3}" \
    "$(( (0x${hex:16:1} & 0x3) | 0x8 ))" \
    "${hex:17:3}" \
    "${hex:20:12}"
}

# portable_replace <file> <search> <replace>
# Uses python3 (preferred) or perl for cross-platform literal string replace
portable_replace() {
  local file="$1" search="$2" replace="$3"
  if command -v python3 >/dev/null 2>&1; then
    python3 -c "
import sys, pathlib
p = pathlib.Path(sys.argv[1])
content = p.read_text()
p.write_text(content.replace(sys.argv[2], sys.argv[3]))
" "${file}" "${search}" "${replace}"
  else
    # perl fallback — \Q disables metacharacters
    perl -i -pe 's/\Q'"${search}"'\E/'"${replace}"'/g' "${file}"
  fi
}

# ---------------------------------------------------------------------------
# Prerequisite checks
# ---------------------------------------------------------------------------
[[ -f "${TEMPLATE_FILE}" ]] || die "Template not found: ${TEMPLATE_FILE}"
[[ -f "${ENV_FILE}" ]] || die ".env not found at ${ENV_FILE}. Run bootstrap-env.sh first."
command -v openssl >/dev/null 2>&1 || die "'openssl' is required."

# ---------------------------------------------------------------------------
# Load .env for context values (PERSONEL_TENANT_ID, PERSONEL_EXTERNAL_HOST)
# ---------------------------------------------------------------------------
set -a
# shellcheck source=/dev/null
source "${ENV_FILE}"
set +a

# ---------------------------------------------------------------------------
# Resolve tenant UUID and external host
# ---------------------------------------------------------------------------
TENANT_ID="${TENANT_ID_ARG:-${PERSONEL_TENANT_ID:-}}"
if [[ -z "${TENANT_ID}" ]] || [[ "${TENANT_ID}" == *"CHANGEME"* ]] || [[ "${TENANT_ID}" == *"changeme"* ]]; then
  TENANT_ID="$(rand_uuid)"
  warn "PERSONEL_TENANT_ID not set or is placeholder. Generated new UUID: ${TENANT_ID}"
fi

EXTERNAL_HOST="${HOST_ARG:-${PERSONEL_EXTERNAL_HOST:-}}"
if [[ -z "${EXTERNAL_HOST}" ]] || [[ "${EXTERNAL_HOST}" == *"CHANGEME"* ]]; then
  EXTERNAL_HOST="personel.example.com"
  warn "PERSONEL_EXTERNAL_HOST not set. Using placeholder: ${EXTERNAL_HOST}"
  warn "Edit ${OUTPUT_FILE} after provisioning if needed."
fi

# ---------------------------------------------------------------------------
# Generate per-client secrets
# ---------------------------------------------------------------------------
log "Generating client secrets..."

SECRET_CONSOLE="$(rand_hex_16)"
SECRET_PORTAL="$(rand_hex_16)"
SECRET_API="$(rand_hex_16)"
SECRET_GRAFANA="$(rand_hex_16)"

SMTP_HOST="${KEYCLOAK_SMTP_HOST:-CHANGEME_SMTP_HOST}"
SMTP_USER="${KEYCLOAK_SMTP_USER:-CHANGEME_SMTP_USER}"
SMTP_PASSWORD="${KEYCLOAK_SMTP_PASSWORD:-CHANGEME_SMTP_PASSWORD}"
SMTP_DOMAIN="${EXTERNAL_HOST}"

# ---------------------------------------------------------------------------
# Dry-run: print planned substitutions
# ---------------------------------------------------------------------------
if [[ "${DRY_RUN}" == "true" ]]; then
  log "--- DRY RUN: substitutions that would be applied ---"
  cat <<EOF
CHANGEME_TENANT_ID      -> ${TENANT_ID}
CHANGEME_HOST           -> ${EXTERNAL_HOST}
CHANGEME_CONSOLE_CLIENT_SECRET -> ${SECRET_CONSOLE}
CHANGEME_PORTAL_CLIENT_SECRET  -> ${SECRET_PORTAL}
CHANGEME_API_CLIENT_SECRET     -> ${SECRET_API}
CHANGEME_GRAFANA_CLIENT_SECRET -> ${SECRET_GRAFANA}
CHANGEME_SMTP_HOST      -> ${SMTP_HOST}
CHANGEME_SMTP_USER      -> ${SMTP_USER}
CHANGEME_SMTP_PASSWORD  -> (from .env)
CHANGEME_DOMAIN         -> ${SMTP_DOMAIN}
EOF
  log "Output would be written to: ${OUTPUT_FILE}"
  exit 0
fi

# ---------------------------------------------------------------------------
# Write provisioned JSON
# ---------------------------------------------------------------------------
log "Copying template to ${OUTPUT_FILE} ..."
cp "${TEMPLATE_FILE}" "${OUTPUT_FILE}"

log "Substituting placeholders..."

portable_replace "${OUTPUT_FILE}" "CHANGEME_TENANT_ID"             "${TENANT_ID}"
portable_replace "${OUTPUT_FILE}" "CHANGEME_HOST"                  "${EXTERNAL_HOST}"
portable_replace "${OUTPUT_FILE}" "CHANGEME_CONSOLE_CLIENT_SECRET" "${SECRET_CONSOLE}"
portable_replace "${OUTPUT_FILE}" "CHANGEME_PORTAL_CLIENT_SECRET"  "${SECRET_PORTAL}"
portable_replace "${OUTPUT_FILE}" "CHANGEME_API_CLIENT_SECRET"     "${SECRET_API}"
portable_replace "${OUTPUT_FILE}" "CHANGEME_GRAFANA_CLIENT_SECRET" "${SECRET_GRAFANA}"
portable_replace "${OUTPUT_FILE}" "CHANGEME_SMTP_HOST"             "${SMTP_HOST}"
portable_replace "${OUTPUT_FILE}" "CHANGEME_SMTP_USER"             "${SMTP_USER}"
portable_replace "${OUTPUT_FILE}" "CHANGEME_SMTP_PASSWORD"         "${SMTP_PASSWORD}"
portable_replace "${OUTPUT_FILE}" "CHANGEME_DOMAIN"                "${SMTP_DOMAIN}"

# Secure the provisioned realm file
chmod 600 "${OUTPUT_FILE}"

# ---------------------------------------------------------------------------
# Verify no CHANGEME_ tokens remain
# ---------------------------------------------------------------------------
if grep -q "CHANGEME_" "${OUTPUT_FILE}" 2>/dev/null; then
  warn "Remaining CHANGEME_ tokens found in ${OUTPUT_FILE}:"
  grep -n "CHANGEME_" "${OUTPUT_FILE}" >&2
  warn "Edit ${OUTPUT_FILE} manually before import."
fi

# ---------------------------------------------------------------------------
# Append Keycloak client secrets to .env
# ---------------------------------------------------------------------------
log "Appending client secrets to ${ENV_FILE} ..."

# Guard: don't double-append on re-runs
if grep -q "^KEYCLOAK_CLIENT_SECRET_CONSOLE=" "${ENV_FILE}" 2>/dev/null; then
  warn "KEYCLOAK_CLIENT_SECRET_CONSOLE already in .env — not appending. Use --force to re-generate."
else
  cat >> "${ENV_FILE}" <<EOF

# ---------------------------------------------------------------------------
# KEYCLOAK CLIENT SECRETS (generated by bootstrap-keycloak.sh $(date -u +"%Y-%m-%dT%H:%M:%SZ"))
# ---------------------------------------------------------------------------
KEYCLOAK_CLIENT_SECRET_CONSOLE=${SECRET_CONSOLE}
KEYCLOAK_CLIENT_SECRET_PORTAL=${SECRET_PORTAL}
KEYCLOAK_CLIENT_SECRET_API=${SECRET_API}
KEYCLOAK_CLIENT_SECRET_GRAFANA=${SECRET_GRAFANA}
EOF
  chmod 600 "${ENV_FILE}"
  log "Client secrets appended to ${ENV_FILE}."
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
log ""
log "Provisioned realm file: ${OUTPUT_FILE}"
log ""
log "NEXT STEPS:"
log "  1. Start Keycloak: docker compose up -d keycloak"
log "  2. Wait for Keycloak readiness:"
log "     docker compose exec keycloak /opt/keycloak/bin/kc.sh show-config 2>/dev/null || sleep 30"
log "  3. Import realm:"
log "     docker compose exec -T keycloak \\"
log "       /opt/keycloak/bin/kcadm.sh config credentials \\"
log "         --server http://localhost:8080 \\"
log "         --realm master \\"
log "         --user \"\${KEYCLOAK_ADMIN_USER}\" \\"
log "         --password \"\${KEYCLOAK_ADMIN_PASSWORD}\""
log "     docker compose exec -T keycloak \\"
log "       /opt/keycloak/bin/kcadm.sh create realms \\"
log "         -s enabled=true \\"
log "         -f /opt/keycloak/data/import/realm-personel-provisioned.json"
log "  4. Verify tenant_id claim in token:"
log "     (decode access token and check tenant_id = ${TENANT_ID})"
