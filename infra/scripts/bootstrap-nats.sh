#!/usr/bin/env bash
# =============================================================================
# Personel Platform — NATS Operator + Account + User Bootstrap
# TR: nsc aracıyla NATS operator/account/user hiyerarşisi oluşturur.
#     JWT ve credential dosyaları üretilir; .env güncellenir.
# EN: Creates NATS operator/account/user hierarchy using nsc.
#     Produces JWTs and credential files; updates .env.
#
# Usage:
#   ./bootstrap-nats.sh               — full setup with defaults
#   ./bootstrap-nats.sh --dry-run     — show what would be created, exit
#   ./bootstrap-nats.sh --force       — re-run even if already provisioned
#
# Outputs (all gitignored):
#   ${NATS_CREDS_DIR}/                          — credential store root
#   ${NATS_CREDS_DIR}/<user>.creds              — per-service NKey+JWT creds
#   .env updated with NATS_OPERATOR_JWT and creds paths
#
# Prerequisites:
#   • nsc >= 2.9 installed (brew install nats-io/nats-tools/nsc  OR  go install)
#   • nats CLI installed (brew install nats-io/nats-tools/nats   OR  go install)
#   • infra/compose/.env exists (run bootstrap-env.sh first)
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_DIR="${SCRIPT_DIR}/../compose"
ENV_FILE="${COMPOSE_DIR}/.env"

# Default NSC data directory (can be overridden by NSC_HOME env var)
NSC_HOME="${NSC_HOME:-${HOME}/.local/share/nats/nsc}"
NATS_CREDS_DIR="${COMPOSE_DIR}/nats/creds"

OPERATOR_NAME="personel"
ACCOUNT_NAME="personel"
SYSTEM_ACCOUNT_NAME="SYS"

# Service users — one per consuming service
USERS=("gateway" "dlp" "api" "audit-enricher")

# ---------------------------------------------------------------------------
# Flag parsing
# ---------------------------------------------------------------------------
DRY_RUN=false
FORCE=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run)  DRY_RUN=true; shift ;;
    --force)    FORCE=true; shift ;;
    --help|-h)
      sed -n '2,24p' "${BASH_SOURCE[0]}" | sed 's/^# \?//'
      exit 0
      ;;
    *)
      echo "[bootstrap-nats] ERROR: Unknown flag: $1" >&2
      exit 1
      ;;
  esac
done

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
log()  { echo "[bootstrap-nats] $*"; }
warn() { echo "[bootstrap-nats] WARN: $*" >&2; }
die()  { echo "[bootstrap-nats] ERROR: $*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Prerequisite: nsc
# ---------------------------------------------------------------------------
if ! command -v nsc >/dev/null 2>&1; then
  cat >&2 <<EOF

[bootstrap-nats] ERROR: 'nsc' is not installed.

nsc is the NATS Security Credentials tool — required for operator/account/user JWT generation.

Install options:
  macOS:   brew install nats-io/nats-tools/nsc
  Linux:   curl -sf https://raw.githubusercontent.com/nats-io/nsc/main/install.sh | sh
  Go:      go install github.com/nats-io/nsc/v2@latest

After installing, re-run this script.
EOF
  exit 1
fi

NSC_VERSION=$(nsc --version 2>&1 | head -1)
log "nsc version: ${NSC_VERSION}"

# ---------------------------------------------------------------------------
# Prerequisite: .env
# ---------------------------------------------------------------------------
[[ -f "${ENV_FILE}" ]] || die ".env not found at ${ENV_FILE}. Run bootstrap-env.sh first."

# ---------------------------------------------------------------------------
# Dry-run
# ---------------------------------------------------------------------------
if [[ "${DRY_RUN}" == "true" ]]; then
  log "--- DRY RUN: objects that would be created ---"
  log "NSC store:        ${NSC_HOME}"
  log "Creds dir:        ${NATS_CREDS_DIR}"
  log "Operator:         ${OPERATOR_NAME}"
  log "System account:   ${SYSTEM_ACCOUNT_NAME}"
  log "Account:          ${ACCOUNT_NAME}"
  log "Users:            ${USERS[*]}"
  log ""
  log ".env keys that would be written:"
  log "  NATS_OPERATOR_JWT=<operator JWT>"
  log "  NATS_SYSTEM_ACCOUNT_JWT=<system account JWT>"
  log "  NATS_GATEWAY_CREDS=${NATS_CREDS_DIR}/gateway.creds"
  log "  NATS_DLP_CREDS=${NATS_CREDS_DIR}/dlp.creds"
  log "  NATS_API_CREDS=${NATS_CREDS_DIR}/api.creds"
  log "  NATS_AUDIT_CREDS=${NATS_CREDS_DIR}/audit-enricher.creds"
  exit 0
fi

# ---------------------------------------------------------------------------
# Idempotency guard
# ---------------------------------------------------------------------------
if [[ -f "${NATS_CREDS_DIR}/gateway.creds" ]] && [[ "${FORCE}" == "false" ]]; then
  warn "NATS credentials already provisioned at ${NATS_CREDS_DIR}/"
  warn "Use --force to re-generate (WARNING: this invalidates all existing creds)"
  exit 0
fi

# ---------------------------------------------------------------------------
# Configure nsc environment
# ---------------------------------------------------------------------------
export NSC_HOME
mkdir -p "${NSC_HOME}"
mkdir -p "${NATS_CREDS_DIR}"

log "NSC store: ${NSC_HOME}"

# ---------------------------------------------------------------------------
# Create operator (idempotent: nsc add operator is safe to call twice)
# ---------------------------------------------------------------------------
log "Creating operator '${OPERATOR_NAME}'..."
if ! nsc list operators 2>/dev/null | grep -q "${OPERATOR_NAME}"; then
  nsc add operator \
    --name "${OPERATOR_NAME}" \
    --sys
  log "  Operator created."
else
  log "  Operator already exists — skipping create."
fi

# Set the current operator context
nsc env --operator "${OPERATOR_NAME}" >/dev/null

# ---------------------------------------------------------------------------
# Create system account SYS (required by NATS server operator mode)
# ---------------------------------------------------------------------------
log "Creating system account '${SYSTEM_ACCOUNT_NAME}'..."
if ! nsc list accounts 2>/dev/null | grep -q "^| ${SYSTEM_ACCOUNT_NAME}"; then
  nsc add account --name "${SYSTEM_ACCOUNT_NAME}"
  log "  System account created."
else
  log "  System account already exists — skipping."
fi

# Push system account JWT to operator
nsc generate config --sys-account "${SYSTEM_ACCOUNT_NAME}" >/dev/null 2>&1 || true

# ---------------------------------------------------------------------------
# Create application account
# ---------------------------------------------------------------------------
log "Creating account '${ACCOUNT_NAME}'..."
if ! nsc list accounts 2>/dev/null | grep -q "^| ${ACCOUNT_NAME}"; then
  nsc add account \
    --name "${ACCOUNT_NAME}" \
    --js-mem-storage -1 \
    --js-disk-storage -1 \
    --js-streams -1 \
    --js-consumer -1
  log "  Account created with unlimited JetStream quotas."
else
  log "  Account already exists — skipping."
fi

nsc env --operator "${OPERATOR_NAME}" --account "${ACCOUNT_NAME}" >/dev/null

# ---------------------------------------------------------------------------
# Create per-service users
# ---------------------------------------------------------------------------
for user in "${USERS[@]}"; do
  log "Creating user '${user}'..."
  if ! nsc list users 2>/dev/null | grep -q "^| ${user}"; then
    nsc add user --name "${user}" --account "${ACCOUNT_NAME}"
    log "  User '${user}' created."
  else
    if [[ "${FORCE}" == "true" ]]; then
      log "  User '${user}' exists but --force given — rotating credentials."
      nsc revoke adduser --name "${user}" --account "${ACCOUNT_NAME}" 2>/dev/null || true
      nsc add user --name "${user}" --account "${ACCOUNT_NAME}"
    else
      log "  User '${user}' already exists — skipping."
    fi
  fi

  # Export credentials file
  local_cred="${NATS_CREDS_DIR}/${user}.creds"
  nsc generate creds \
    --operator "${OPERATOR_NAME}" \
    --account  "${ACCOUNT_NAME}" \
    --name     "${user}" \
    > "${local_cred}"
  chmod 600 "${local_cred}"
  log "  Creds: ${local_cred}"
done

# ---------------------------------------------------------------------------
# Extract operator JWT
# ---------------------------------------------------------------------------
log "Extracting operator JWT..."
OPERATOR_JWT="$(nsc describe operator "${OPERATOR_NAME}" --raw 2>/dev/null)"
if [[ -z "${OPERATOR_JWT}" ]]; then
  # Fallback: read from nsc store directly
  OPERATOR_JWT="$(find "${NSC_HOME}" -name "*.jwt" -path "*${OPERATOR_NAME}*" | head -1 | xargs cat 2>/dev/null || true)"
fi
[[ -n "${OPERATOR_JWT}" ]] || die "Could not extract operator JWT. Check nsc store at ${NSC_HOME}"

# ---------------------------------------------------------------------------
# Extract system account JWT
# ---------------------------------------------------------------------------
log "Extracting system account JWT..."
SYS_ACCOUNT_JWT="$(nsc describe account "${SYSTEM_ACCOUNT_NAME}" --raw 2>/dev/null || true)"

# ---------------------------------------------------------------------------
# Update .env
# ---------------------------------------------------------------------------
log "Updating ${ENV_FILE} with NATS configuration..."

# Helper: upsert a key=value line in .env
upsert_env() {
  local key="$1" value="$2"
  if grep -q "^${key}=" "${ENV_FILE}" 2>/dev/null; then
    # Replace existing line — portable across macOS (BSD sed) and Linux (GNU sed)
    if [[ "$(uname -s)" == "Darwin" ]]; then
      sed -i '' "s|^${key}=.*|${key}=${value}|" "${ENV_FILE}"
    else
      sed -i "s|^${key}=.*|${key}=${value}|" "${ENV_FILE}"
    fi
  else
    printf '%s=%s\n' "${key}" "${value}" >> "${ENV_FILE}"
  fi
}

upsert_env "NATS_OPERATOR_JWT"       "${OPERATOR_JWT}"
upsert_env "NATS_SYSTEM_ACCOUNT_JWT" "${SYS_ACCOUNT_JWT:-}"
upsert_env "NATS_GATEWAY_CREDS"      "${NATS_CREDS_DIR}/gateway.creds"
upsert_env "NATS_DLP_CREDS"          "${NATS_CREDS_DIR}/dlp.creds"
upsert_env "NATS_API_CREDS"          "${NATS_CREDS_DIR}/api.creds"
upsert_env "NATS_AUDIT_CREDS"        "${NATS_CREDS_DIR}/audit-enricher.creds"

chmod 600 "${ENV_FILE}"

# ---------------------------------------------------------------------------
# Optional: generate nats.conf resolver snippet
# ---------------------------------------------------------------------------
RESOLVER_CONF="${COMPOSE_DIR}/nats/resolver.conf"
log "Writing account resolver config to ${RESOLVER_CONF} ..."
{
  printf '# Auto-generated by bootstrap-nats.sh on %s\n' "$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
  printf '# Paste (or include) into nats.conf if using push-resolver.\n\n'
  nsc generate config \
    --operator "${OPERATOR_NAME}" \
    --sys-account "${SYSTEM_ACCOUNT_NAME}" \
    2>/dev/null || true
} > "${RESOLVER_CONF}" 2>/dev/null || warn "Could not generate resolver config snippet."

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
log ""
log "NATS bootstrap complete."
log ""
log "Operator JWT written to .env (NATS_OPERATOR_JWT)"
log "Creds directory:   ${NATS_CREDS_DIR}/"
for user in "${USERS[@]}"; do
  log "  ${user}.creds"
done
log ""
log "NEXT STEPS:"
log "  1. The NATS_OPERATOR_JWT in .env is now set."
log "  2. Mount ${NATS_CREDS_DIR}/ into service containers (read-only)."
log "  3. In docker-compose.yaml, bind-mount each *.creds file and set"
log "     NATS_CREDS env var in the consuming service."
log "  4. Restart NATS: docker compose restart nats"
log "  5. Verify: nats account info --server nats://localhost:4222 --creds ${NATS_CREDS_DIR}/gateway.creds"
warn "Store ${NATS_CREDS_DIR}/*.creds with the same security as .env (chmod 600, gitignored)."
