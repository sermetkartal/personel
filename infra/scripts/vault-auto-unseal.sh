#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Vault Auto-Unseal (Dev/Staging)
# =============================================================================
#
# PURPOSE
#   Reads Shamir unseal keys from environment variables and calls
#   `vault operator unseal` non-interactively. Designed for:
#     - Dev/staging: run manually or via docker-compose healthcheck
#     - Systemd: ExecStartPre in personel-vault-unseal.service
#
# CRITICAL SECURITY WARNING
# ─────────────────────────
#   Storing Shamir unseal keys in environment variables or files on the
#   same host as Vault defeats the security model of Shamir Secret Sharing.
#   An attacker with root access to the host can read /proc/<pid>/environ
#   or the env file and reconstruct the unseal keys.
#
#   APPROVED USE CASES ONLY:
#     • Dev / staging environments with no real sensitive data
#     • CI pipelines with ephemeral Vault instances
#     • Short-lived demo stacks
#
#   PRODUCTION UPGRADE PATH — HSM-backed auto-unseal:
#     Replace Shamir unseal entirely with Vault's Transit Auto-Unseal
#     pointed at a cloud KMS (AWS KMS, Azure Key Vault, GCP CKMS) or a
#     hardware HSM (PKCS#11 seal). With Transit Auto-Unseal, Vault
#     encrypts its root key with the KMS CMK — no unseal keys exist at
#     all. Configure in vault.hcl:
#
#       seal "awskms" {
#         region     = "eu-central-1"
#         kms_key_id = "alias/personel-vault-unseal"
#       }
#
#     Reference: https://developer.hashicorp.com/vault/docs/configuration/seal
#
# USAGE
#   # Required env vars (3-of-5 threshold, default):
#   export VAULT_UNSEAL_KEY_1="<key>"
#   export VAULT_UNSEAL_KEY_2="<key>"
#   export VAULT_UNSEAL_KEY_3="<key>"
#   export VAULT_ADDR="https://127.0.0.1:8200"
#
#   ./vault-auto-unseal.sh
#
#   # Optionally load from a sealed env file (chmod 400, root-owned):
#   VAULT_UNSEAL_ENV_FILE=/etc/personel/vault-unseal.env ./vault-auto-unseal.sh
#
# SYSTEMD INTEGRATION
#   Copy infra/systemd/personel-vault-unseal.service to /etc/systemd/system/
#   and enable it. The service references an EnvironmentFile=/etc/personel/
#   vault-unseal.env that is root-owned and mode 400.
#
# EXIT CODES
#   0 — Vault successfully unsealed (or was already unsealed)
#   1 — Fatal error (not initialized, wrong keys, etc.)
# =============================================================================
set -euo pipefail

VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}"
VAULT_CACERT="${VAULT_CACERT:-/etc/personel/tls/tenant_ca.crt}"
VAULT_CONTAINER="${VAULT_CONTAINER:-personel-vault}"
# Number of shares to submit (must match Vault's unseal threshold)
VAULT_UNSEAL_THRESHOLD="${VAULT_UNSEAL_THRESHOLD:-3}"

export VAULT_ADDR VAULT_CACERT

log()  { echo "[vault-auto-unseal] $*"; }
warn() { echo "[vault-auto-unseal] WARN: $*" >&2; }
die()  { echo "[vault-auto-unseal] ERROR: $*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# 0. Print the security warning unless suppressed
# ---------------------------------------------------------------------------
if [[ "${VAULT_AUTO_UNSEAL_SUPPRESS_WARNING:-0}" != "1" ]]; then
  warn "=========================================================="
  warn "SECURITY: Auto-unseal is active. Unseal keys are in env."
  warn "This is ONLY safe for dev/staging. Production must use"
  warn "Vault Transit Auto-Unseal with an HSM or cloud KMS."
  warn "Set VAULT_AUTO_UNSEAL_SUPPRESS_WARNING=1 to hide this."
  warn "=========================================================="
fi

# ---------------------------------------------------------------------------
# 1. Optionally load from an env file (root-owned, chmod 400)
# ---------------------------------------------------------------------------
if [[ -n "${VAULT_UNSEAL_ENV_FILE:-}" ]]; then
  if [[ ! -f "${VAULT_UNSEAL_ENV_FILE}" ]]; then
    die "VAULT_UNSEAL_ENV_FILE=${VAULT_UNSEAL_ENV_FILE} not found"
  fi
  # shellcheck source=/dev/null
  source "${VAULT_UNSEAL_ENV_FILE}"
  log "Loaded unseal env from ${VAULT_UNSEAL_ENV_FILE}"
fi

# ---------------------------------------------------------------------------
# 2. Validate required env vars exist
# ---------------------------------------------------------------------------
missing=()
for i in $(seq 1 "${VAULT_UNSEAL_THRESHOLD}"); do
  var="VAULT_UNSEAL_KEY_${i}"
  if [[ -z "${!var:-}" ]]; then
    missing+=("${var}")
  fi
done
if [[ ${#missing[@]} -gt 0 ]]; then
  die "Missing required env vars: ${missing[*]}"
fi

# ---------------------------------------------------------------------------
# 3. Check Vault status
# ---------------------------------------------------------------------------
# Try direct vault CLI first; fall back to docker exec if running in container
vault_cmd() {
  if command -v vault &>/dev/null && vault status &>/dev/null 2>&1; then
    vault "$@"
  elif docker ps --format '{{.Names}}' 2>/dev/null | grep -q "^${VAULT_CONTAINER}$"; then
    docker exec \
      -e VAULT_ADDR="${VAULT_ADDR}" \
      -e VAULT_CACERT="${VAULT_CACERT}" \
      "${VAULT_CONTAINER}" vault "$@"
  else
    die "vault CLI not found and container '${VAULT_CONTAINER}' is not running"
  fi
}

STATUS=$(vault_cmd status -format=json -tls-skip-verify 2>/dev/null \
  || echo '{"sealed":true,"initialized":false}')

INITIALIZED=$(echo "${STATUS}" | python3 -c \
  "import json,sys; print(json.load(sys.stdin).get('initialized',False))")
SEALED=$(echo "${STATUS}" | python3 -c \
  "import json,sys; print(json.load(sys.stdin).get('sealed',True))")

[[ "${INITIALIZED}" == "True" ]] \
  || die "Vault is not initialized. Run the bootstrap ceremony first."

if [[ "${SEALED}" == "False" ]]; then
  log "Vault is already unsealed — nothing to do."
  exit 0
fi

# ---------------------------------------------------------------------------
# 4. Submit unseal shares
# ---------------------------------------------------------------------------
log "Vault is sealed. Submitting ${VAULT_UNSEAL_THRESHOLD} unseal shares..."

for i in $(seq 1 "${VAULT_UNSEAL_THRESHOLD}"); do
  var="VAULT_UNSEAL_KEY_${i}"
  share="${!var}"
  vault_cmd operator unseal -tls-skip-verify "${share}" >/dev/null \
    || die "Share ${i} rejected. Check that VAULT_UNSEAL_KEY_${i} is correct."
  log "Share ${i}/${VAULT_UNSEAL_THRESHOLD} accepted."
done

# ---------------------------------------------------------------------------
# 5. Verify unsealed
# ---------------------------------------------------------------------------
sleep 2
STATUS_AFTER=$(vault_cmd status -format=json -tls-skip-verify 2>/dev/null \
  || echo '{"sealed":true}')
SEALED_AFTER=$(echo "${STATUS_AFTER}" | python3 -c \
  "import json,sys; print(json.load(sys.stdin).get('sealed',True))")

if [[ "${SEALED_AFTER}" == "False" ]]; then
  log "Vault successfully unsealed."
else
  die "Vault is still sealed after ${VAULT_UNSEAL_THRESHOLD} shares. Verify keys are correct."
fi
