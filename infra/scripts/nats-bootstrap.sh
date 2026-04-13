#!/usr/bin/env bash
# =============================================================================
# Personel Platform — NATS Production Operator/Account/User Bootstrap
#
# Generates an operator NKey + JWT, a PersonelMain application account, the
# SYS system account, and three scoped service users:
#
#   gateway-publisher  — publish-only on events.raw.>, events.sensitive.>,
#                        agent.health.>, pki.events.>
#   enricher-consumer  — subscribe on events.>, publish on live_view.>
#   api-controlplane   — publish on policy.v1, live_view.>;
#                        subscribe on agent.health.>
#
# Outputs (chmod 600):
#   /etc/personel/nats/operator.jwt
#   /etc/personel/nats/resolver/<account_pub_key>.jwt
#   /etc/personel/nats-creds/gateway.creds
#   /etc/personel/nats-creds/enricher.creds
#   /etc/personel/nats-creds/api.creds
#   /etc/personel/secrets/nats-encryption.key   (32 bytes hex)
#
# Idempotent: refuses to run if /etc/personel/nats-creds/gateway.creds exists.
# Pass --force to rotate. Rotation invalidates all previously issued user JWTs
# and requires every consuming service to be restarted with the new creds.
#
# This script does NOT touch the running stack. The operator must:
#   1. Run this script.
#   2. Switch the compose stack to use docker-compose.prod-override.yaml.
#   3. Restart NATS, then enricher, then gateway, then api.
#   4. Verify subjects via:
#        nats --creds /etc/personel/nats-creds/gateway.creds \
#             --tlscert /etc/personel/tls/nats.crt \
#             --tlskey /etc/personel/tls/nats.key \
#             pub events.raw.smoke '{"hello":"world"}'
#
# Prerequisites:
#   nsc >= 2.10  (https://github.com/nats-io/nsc)
#   openssl
# =============================================================================
set -euo pipefail

SCRIPT_NAME="nats-bootstrap"
NSC_HOME="${NSC_HOME:-/var/lib/personel/nsc}"
NATS_CONF_DIR="/etc/personel/nats"
NATS_RESOLVER_DIR="${NATS_CONF_DIR}/resolver"
NATS_CREDS_DIR="/etc/personel/nats-creds"
NATS_SECRETS_DIR="/etc/personel/secrets"

OPERATOR_NAME="PersonelOperator"
ACCOUNT_NAME="PersonelMain"
SYS_ACCOUNT_NAME="SYS"

# User -> account mapping
USER_GATEWAY="gateway-publisher"
USER_ENRICHER="enricher-consumer"
USER_API="api-controlplane"

# ---------------------------------------------------------------------------
# Logging helpers
# ---------------------------------------------------------------------------
log()  { printf '[%s] %s\n' "${SCRIPT_NAME}" "$*"; }
warn() { printf '[%s] WARN: %s\n' "${SCRIPT_NAME}" "$*" >&2; }
die()  { printf '[%s] ERROR: %s\n' "${SCRIPT_NAME}" "$*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# Flag parsing
# ---------------------------------------------------------------------------
FORCE=false
while [[ $# -gt 0 ]]; do
  case "$1" in
    --force) FORCE=true; shift ;;
    --help|-h)
      sed -n '2,45p' "${BASH_SOURCE[0]}" | sed 's/^# \?//'
      exit 0
      ;;
    *) die "unknown flag: $1" ;;
  esac
done

# ---------------------------------------------------------------------------
# Prerequisite check
# ---------------------------------------------------------------------------
command -v nsc >/dev/null 2>&1 || die "nsc not installed; see https://github.com/nats-io/nsc"
command -v openssl >/dev/null 2>&1 || die "openssl required for encryption key generation"

# ---------------------------------------------------------------------------
# Idempotency guard
# ---------------------------------------------------------------------------
if [[ -f "${NATS_CREDS_DIR}/gateway.creds" ]] && [[ "${FORCE}" == "false" ]]; then
  warn "NATS credentials already exist at ${NATS_CREDS_DIR}/"
  warn "Pass --force to rotate. Rotation invalidates all previously issued JWTs."
  exit 0
fi

if [[ "${FORCE}" == "true" ]]; then
  warn "FORCE mode: existing credentials WILL be overwritten and old JWTs revoked."
fi

# ---------------------------------------------------------------------------
# Directory layout
# ---------------------------------------------------------------------------
log "Preparing directory layout..."
install -d -m 0755 "${NATS_CONF_DIR}"
install -d -m 0700 "${NATS_RESOLVER_DIR}"
install -d -m 0700 "${NATS_CREDS_DIR}"
install -d -m 0700 "${NATS_SECRETS_DIR}"
install -d -m 0700 "${NSC_HOME}"

export NSC_HOME

# ---------------------------------------------------------------------------
# Operator
# ---------------------------------------------------------------------------
log "Creating operator '${OPERATOR_NAME}'..."
if ! nsc list operators 2>/dev/null | grep -q "${OPERATOR_NAME}"; then
  nsc add operator --name "${OPERATOR_NAME}" --sys
  # Mark the operator as the signing root for account JWTs
  nsc edit operator --require-signing-keys --account-jwt-server-url "nats://nats:4222"
else
  log "  operator already present in nsc store — reusing"
fi
nsc env --operator "${OPERATOR_NAME}" >/dev/null

# ---------------------------------------------------------------------------
# System account
# ---------------------------------------------------------------------------
log "Creating system account '${SYS_ACCOUNT_NAME}'..."
if ! nsc list accounts 2>/dev/null | grep -q " ${SYS_ACCOUNT_NAME} "; then
  nsc add account --name "${SYS_ACCOUNT_NAME}"
fi

# ---------------------------------------------------------------------------
# PersonelMain account with JetStream quotas
# ---------------------------------------------------------------------------
log "Creating account '${ACCOUNT_NAME}'..."
if ! nsc list accounts 2>/dev/null | grep -q " ${ACCOUNT_NAME} "; then
  nsc add account \
    --name "${ACCOUNT_NAME}" \
    --js-mem-storage -1 \
    --js-disk-storage -1 \
    --js-streams -1 \
    --js-consumer -1
fi
nsc env --operator "${OPERATOR_NAME}" --account "${ACCOUNT_NAME}" >/dev/null

# ---------------------------------------------------------------------------
# Helper: (re)create a user with explicit allow-pub / allow-sub
# ---------------------------------------------------------------------------
create_user() {
  local user_name="$1"; shift
  local -a pub_subjects=()
  local -a sub_subjects=()
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --pub) pub_subjects+=("$2"); shift 2 ;;
      --sub) sub_subjects+=("$2"); shift 2 ;;
      *) die "create_user: bad arg: $1" ;;
    esac
  done

  log "Creating user '${user_name}'..."
  if nsc list users 2>/dev/null | grep -q " ${user_name} "; then
    if [[ "${FORCE}" == "true" ]]; then
      log "  exists — deleting and recreating"
      nsc delete user --name "${user_name}" --account "${ACCOUNT_NAME}" >/dev/null 2>&1 || true
    else
      log "  user already exists — skipping create"
      return 0
    fi
  fi

  local args=(--name "${user_name}" --account "${ACCOUNT_NAME}")
  for s in "${pub_subjects[@]}"; do
    args+=(--allow-pub "${s}")
  done
  for s in "${sub_subjects[@]}"; do
    args+=(--allow-sub "${s}")
  done
  # Every user always needs reply-inbox access for request/reply
  args+=(--allow-sub "_INBOX.>")

  nsc add user "${args[@]}"
}

# ---------------------------------------------------------------------------
# gateway-publisher: ingest path only
# ---------------------------------------------------------------------------
create_user "${USER_GATEWAY}" \
  --pub "events.raw.>" \
  --pub "events.sensitive.>" \
  --pub "agent.health.>" \
  --pub "pki.events.>"

# ---------------------------------------------------------------------------
# enricher-consumer: drains events, emits live-view fanout
# ---------------------------------------------------------------------------
create_user "${USER_ENRICHER}" \
  --sub "events.>" \
  --sub "agent.health.>" \
  --pub "live_view.>"

# ---------------------------------------------------------------------------
# api-controlplane: control plane + heartbeat read
# ---------------------------------------------------------------------------
create_user "${USER_API}" \
  --pub "policy.v1" \
  --pub "policy.v1.>" \
  --pub "live_view.>" \
  --sub "agent.health.>" \
  --sub "live_view.>"

# ---------------------------------------------------------------------------
# Export creds files
# ---------------------------------------------------------------------------
export_creds() {
  local user="$1" outfile="$2"
  log "Exporting creds for ${user} -> ${outfile}"
  nsc generate creds \
    --operator "${OPERATOR_NAME}" \
    --account  "${ACCOUNT_NAME}" \
    --name     "${user}" \
    > "${outfile}"
  chmod 600 "${outfile}"
}

export_creds "${USER_GATEWAY}"  "${NATS_CREDS_DIR}/gateway.creds"
export_creds "${USER_ENRICHER}" "${NATS_CREDS_DIR}/enricher.creds"
export_creds "${USER_API}"      "${NATS_CREDS_DIR}/api.creds"

# ---------------------------------------------------------------------------
# Operator JWT for the server
# ---------------------------------------------------------------------------
log "Writing operator JWT..."
OPERATOR_JWT_PATH="${NATS_CONF_DIR}/operator.jwt"
nsc describe operator "${OPERATOR_NAME}" --raw > "${OPERATOR_JWT_PATH}"
chmod 644 "${OPERATOR_JWT_PATH}"

# ---------------------------------------------------------------------------
# Push account JWTs into the resolver dir
# ---------------------------------------------------------------------------
log "Writing account JWTs to resolver dir..."
push_account_jwt() {
  local acct="$1"
  local pub_key
  pub_key="$(nsc describe account "${acct}" --field 'sub' 2>/dev/null \
    | tr -d '"' || true)"
  [[ -n "${pub_key}" ]] || die "could not extract public key for account ${acct}"
  nsc describe account "${acct}" --raw > "${NATS_RESOLVER_DIR}/${pub_key}.jwt"
  chmod 600 "${NATS_RESOLVER_DIR}/${pub_key}.jwt"
}
push_account_jwt "${SYS_ACCOUNT_NAME}"
push_account_jwt "${ACCOUNT_NAME}"

# ---------------------------------------------------------------------------
# JetStream at-rest encryption key
# ---------------------------------------------------------------------------
ENCRYPTION_KEY_PATH="${NATS_SECRETS_DIR}/nats-encryption.key"
if [[ ! -f "${ENCRYPTION_KEY_PATH}" ]] || [[ "${FORCE}" == "true" ]]; then
  log "Generating JetStream at-rest encryption key (32 bytes hex)..."
  openssl rand -hex 32 > "${ENCRYPTION_KEY_PATH}"
  chmod 600 "${ENCRYPTION_KEY_PATH}"
else
  log "Encryption key already present — preserving (avoid re-encrypt)"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
log ""
log "Bootstrap complete. Files written:"
log "  ${OPERATOR_JWT_PATH}"
log "  ${NATS_RESOLVER_DIR}/<account_pub_key>.jwt  (SYS + ${ACCOUNT_NAME})"
log "  ${NATS_CREDS_DIR}/{gateway,enricher,api}.creds"
log "  ${ENCRYPTION_KEY_PATH}"
log ""
log "NEXT: Switch the stack to docker-compose.prod-override.yaml and restart"
log "      services in this order:  nats -> enricher -> gateway -> api"
log "      See docs/operations/nats-prod-auth-migration.md"
