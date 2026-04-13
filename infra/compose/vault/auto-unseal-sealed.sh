#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Vault Auto-Unseal from age-encrypted shares file
# =============================================================================
#
# PURPOSE
#   Operationally-practical Vault auto-unseal for the period between Faz 1
#   bring-up (no HSM yet) and Phase 2 hardware HSM migration. The 3-of-5
#   Shamir shares are stored together in a single age-encrypted file at
#   /etc/personel/vault-shares.enc. The decryption key (the "master") is
#   supplied via the VAULT_AUTOSEAL_KEY environment variable, which is
#   populated by a systemd drop-in that lives ONLY on disk in
#   /etc/systemd/system/personel-vault-autounseal.service.d/master.conf
#   (root:root, mode 600).
#
# THREAT MODEL
#   - Disk-only attacker (e.g. unencrypted backup tape): cannot decrypt the
#     shares file because the master key is not in the same backup.
#   - Host-root attacker: can read both the shares file AND the systemd
#     drop-in. This is the SAME threat model as the existing dev script
#     vault-auto-unseal.sh — we accept it because the alternative is a
#     custodian phone tree at 03:00 every reboot, which is operationally
#     unworkable.
#   - Phase 2 HSM migration: when the customer provisions an HSM, the
#     `seal "pkcs11"` stanza in config.prod.hcl replaces this script
#     ENTIRELY. The shares file and the master key are then destroyed.
#
# INTERFACE WITH SYSTEMD
#   The companion unit personel-vault-autounseal.service has:
#     EnvironmentFile=/etc/systemd/system/personel-vault-autounseal.service.d/master.conf
#   which contains a single line:
#     VAULT_AUTOSEAL_KEY=AGE-SECRET-KEY-1...
#
#   The unit fires:
#     - At boot, after docker.service and network-online.target
#     - Before personel-compose.service
#
# REQUIREMENTS
#   - age >= 1.0 installed on the host (apt install age)
#   - python3 (already a dependency of bootstrap.sh)
#   - /etc/personel/vault-shares.enc exists and is mode 600
#
# EXIT CODES
#   0 — Vault is unsealed (or was already unsealed)
#   1 — Fatal error (shares file missing, key wrong, vault unreachable, etc.)
#
# REFERENCE
#   docs/operations/vault-prod-migration.md §7 (auto-unseal middle ground)
# =============================================================================
set -euo pipefail

VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}"
VAULT_CACERT="${VAULT_CACERT:-/etc/personel/tls/tenant_ca.crt}"
SHARES_FILE="${VAULT_SHARES_FILE:-/etc/personel/vault-shares.enc}"
THRESHOLD="${VAULT_UNSEAL_THRESHOLD:-3}"

export VAULT_ADDR VAULT_CACERT

log()  { echo "[vault-autounseal-sealed] $*"; }
warn() { echo "[vault-autounseal-sealed] WARN: $*" >&2; }
die()  { echo "[vault-autounseal-sealed] ERROR: $*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# 0. Pre-flight
# ---------------------------------------------------------------------------
[[ -n "${VAULT_AUTOSEAL_KEY:-}" ]] \
  || die "VAULT_AUTOSEAL_KEY is not set. The systemd drop-in must export it."

[[ -f "${SHARES_FILE}" ]] \
  || die "Shares file not found at ${SHARES_FILE}. Provision it first."

# Reject world-readable shares files outright
shares_perm=$(stat -c '%a' "${SHARES_FILE}" 2>/dev/null || stat -f '%Lp' "${SHARES_FILE}")
case "${shares_perm}" in
  600|400) ;;
  *) die "${SHARES_FILE} has mode ${shares_perm}; expected 600 or 400." ;;
esac

command -v age >/dev/null 2>&1 \
  || die "'age' binary not found. Install with: apt-get install age"

command -v vault >/dev/null 2>&1 \
  || die "'vault' CLI not found in PATH."

# ---------------------------------------------------------------------------
# 1. Check Vault status — exit clean if already unsealed
# ---------------------------------------------------------------------------
status_out=$(vault status -format=json 2>/dev/null || echo '{"sealed":true,"initialized":false}')

initialized=$(echo "${status_out}" \
  | python3 -c "import json,sys; print(json.load(sys.stdin).get('initialized', False))")
sealed=$(echo "${status_out}" \
  | python3 -c "import json,sys; print(json.load(sys.stdin).get('sealed', True))")

[[ "${initialized}" == "True" ]] \
  || die "Vault is not initialized. Run bootstrap-prod.sh first."

if [[ "${sealed}" == "False" ]]; then
  log "Vault is already unsealed — nothing to do."
  exit 0
fi

# ---------------------------------------------------------------------------
# 2. Decrypt the shares file with age
#
# The shares file is expected to contain ONE base64 share per line.
# We pipe the master key into `age --decrypt -i -` via a temporary
# identity file (age does not accept identity material on stdin in
# older versions, so we use an in-memory tmpfs path).
# ---------------------------------------------------------------------------
log "Decrypting ${SHARES_FILE}..."

# Use /dev/shm (tmpfs) so the identity material never touches durable storage.
identity_file=$(mktemp -p /dev/shm vault-autounseal.XXXXXX)
trap 'shred -u "${identity_file}" 2>/dev/null || rm -f "${identity_file}"' EXIT

printf '%s\n' "${VAULT_AUTOSEAL_KEY}" > "${identity_file}"
chmod 600 "${identity_file}"

if ! plaintext=$(age --decrypt -i "${identity_file}" "${SHARES_FILE}" 2>/dev/null); then
  die "Failed to decrypt ${SHARES_FILE}. Check VAULT_AUTOSEAL_KEY."
fi

# ---------------------------------------------------------------------------
# 3. Parse shares (one per line, ignore blanks and comments)
# ---------------------------------------------------------------------------
mapfile -t shares < <(printf '%s\n' "${plaintext}" | grep -Ev '^\s*(#|$)')

if [[ ${#shares[@]} -lt ${THRESHOLD} ]]; then
  die "Shares file contains ${#shares[@]} share(s), need at least ${THRESHOLD}."
fi

log "Decrypted ${#shares[@]} share(s); submitting ${THRESHOLD} to unseal."

# ---------------------------------------------------------------------------
# 4. Submit shares
# ---------------------------------------------------------------------------
for i in $(seq 0 $((THRESHOLD - 1))); do
  share="${shares[${i}]}"
  if ! vault operator unseal "${share}" >/dev/null 2>&1; then
    die "Share $((i + 1)) of ${THRESHOLD} was rejected by Vault."
  fi
  log "Share $((i + 1))/${THRESHOLD} accepted."
done

# ---------------------------------------------------------------------------
# 5. Verify
# ---------------------------------------------------------------------------
sleep 1
status_after=$(vault status -format=json 2>/dev/null || echo '{"sealed":true}')
sealed_after=$(echo "${status_after}" \
  | python3 -c "import json,sys; print(json.load(sys.stdin).get('sealed', True))")

if [[ "${sealed_after}" == "False" ]]; then
  log "Vault successfully unsealed."
  exit 0
fi

die "Vault is still sealed after submitting ${THRESHOLD} shares."
