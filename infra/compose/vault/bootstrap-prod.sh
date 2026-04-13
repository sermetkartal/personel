#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Vault PRODUCTION Initialization Ceremony
# =============================================================================
#
# PURPOSE
#   Real Shamir 3-of-5 unseal key ceremony for production deployments. This
#   replaces the dev 1-of-1 single-share shortcut used during Faz 1 bring-up.
#
# WHEN TO RUN
#   EXACTLY ONCE, on a fresh Vault instance, with three to five physical
#   custodians present in the room. After this script completes, the printed
#   unseal shares MUST be physically distributed and never typed back into the
#   same machine in plaintext form again.
#
# SAFETY
#   - Refuses to run if Vault is already initialized.
#   - Does NOT auto-unseal. Production never auto-unseals on first boot — the
#     custodians manually run `vault operator unseal` from their machines, or
#     the operator runs auto-unseal-sealed.sh after the age-encrypted shares
#     file has been provisioned out-of-band.
#   - Stores the root token in /root/vault-init-prod.json (chmod 600). The
#     operator MUST revoke this token after the post-init configure step
#     and shred the file.
#
# USAGE
#   sudo VAULT_ADDR=https://127.0.0.1:8200 \
#        VAULT_CACERT=/etc/personel/tls/tenant_ca.crt \
#        ./bootstrap-prod.sh
#
# REFERENCE
#   docs/operations/vault-prod-migration.md
#   docs/security/runbooks/vault-setup.md §3 (Shamir ceremony)
#   docs/security/runbooks/pki-bootstrap.md §3.2 (custodian assignments)
# =============================================================================
set -euo pipefail

VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}"
VAULT_CACERT="${VAULT_CACERT:-/etc/personel/tls/tenant_ca.crt}"
ROOT_TOKEN_FILE="${VAULT_PROD_ROOT_TOKEN_FILE:-/root/vault-init-prod.json}"

export VAULT_ADDR VAULT_CACERT

log()  { echo "[vault-bootstrap-prod] $*"; }
warn() { echo "[vault-bootstrap-prod] WARN: $*" >&2; }
die()  { echo "[vault-bootstrap-prod] ERROR: $*" >&2; exit 1; }

# ---------------------------------------------------------------------------
# 0. Pre-flight — must be root, vault CLI present, sane paths
# ---------------------------------------------------------------------------
[[ "$(id -u)" -eq 0 ]] \
  || die "Must run as root (need to write ${ROOT_TOKEN_FILE} with mode 600)."

command -v vault >/dev/null 2>&1 \
  || die "vault CLI not found in PATH. Install or use docker exec wrapper."

[[ -f "${VAULT_CACERT}" ]] \
  || die "VAULT_CACERT=${VAULT_CACERT} not found. PKI must be bootstrapped first."

# ---------------------------------------------------------------------------
# 1. Refuse if already initialized
# ---------------------------------------------------------------------------
log "Checking Vault state at ${VAULT_ADDR}..."

# `vault status` returns exit 2 if sealed, 0 if unsealed, 1 on connection
# error. It writes Initialized: true/false to stdout regardless of seal state.
set +e
status_out=$(vault status -format=json 2>/dev/null)
status_rc=$?
set -e

if [[ ${status_rc} -eq 1 ]]; then
  die "Cannot reach Vault at ${VAULT_ADDR}. Is the container running?"
fi

initialized=$(echo "${status_out}" \
  | python3 -c "import json,sys; print(json.load(sys.stdin).get('initialized', False))" \
  2>/dev/null || echo "False")

if [[ "${initialized}" == "True" ]]; then
  die "Vault is ALREADY initialized. Refusing to re-run the ceremony.

If you are migrating from dev (1-of-1) to production (3-of-5), you must
follow docs/operations/vault-prod-migration.md — the migration uses
'vault operator rekey', NOT a re-init. Re-init would destroy all PKI state,
all AppRoles, and all transit keys."
fi

# ---------------------------------------------------------------------------
# 2. Confirmation prompt — this is destructive-by-omission
# ---------------------------------------------------------------------------
cat <<'BANNER'
==============================================================================
 PERSONEL VAULT — PRODUCTION INITIALIZATION CEREMONY
==============================================================================

You are about to initialize a NEW Vault instance with Shamir 3-of-5 unseal.

REQUIRED ATTENDEES (per pki-bootstrap.md §3.2):
  - Customer security officer (will hold shares 1 and 2)
  - Security-engineer team lead (share 3)
  - Customer DPO (share 4)
  - Customer HQ vault custodian (share 5 — fireproof safe)

The ceremony output will print the 5 unseal shares ONE TIME. They will not
be re-readable from this machine after the ceremony completes. You must
physically write each share into a tamper-evident envelope and distribute
them to the named custodians BEFORE this terminal session is closed.

The root token will be written to /root/vault-init-prod.json (mode 600).
After the post-init configure run completes you MUST:
  1. vault token revoke <root>
  2. shred -uz /root/vault-init-prod.json

Type 'I UNDERSTAND' to continue, or anything else to abort.
==============================================================================
BANNER

read -r -p "> " confirmation
[[ "${confirmation}" == "I UNDERSTAND" ]] \
  || die "Aborted. No state changed."

# ---------------------------------------------------------------------------
# 3. Run the actual init
# ---------------------------------------------------------------------------
log "Running 'vault operator init -key-shares=5 -key-threshold=3'..."

init_output=$(vault operator init \
  -key-shares=5 \
  -key-threshold=3 \
  -format=json)

# Persist the JSON to the operator's root-only file BEFORE printing anything,
# so a partial print + Ctrl-C still leaves the operator able to recover the
# shares from disk.
umask 077
echo "${init_output}" > "${ROOT_TOKEN_FILE}"
chmod 600 "${ROOT_TOKEN_FILE}"

log "Init JSON persisted to ${ROOT_TOKEN_FILE} (mode 600, root:root)."

# ---------------------------------------------------------------------------
# 4. Print the shares and root token to stdout — ceremony moment
# ---------------------------------------------------------------------------
echo "${init_output}" | python3 - <<'PY'
import json
import sys

data = json.load(sys.stdin)

print()
print("==============================================================================")
print("  UNSEAL SHARES — DISTRIBUTE TO CUSTODIANS NOW")
print("==============================================================================")
print()
for i, share in enumerate(data["unseal_keys_b64"], 1):
    print(f"  Share {i} of 5:  {share}")
print()
print("  Custodian assignments (per pki-bootstrap.md §3.2):")
print("    Share 1 -> Customer security officer (envelope A)")
print("    Share 2 -> Customer security officer (envelope B, separate location)")
print("    Share 3 -> Security-engineer team lead")
print("    Share 4 -> Customer DPO")
print("    Share 5 -> Customer HQ fireproof safe")
print()
print("  Threshold is 3. Any 3 of the 5 shares can unseal Vault.")
print()
print("==============================================================================")
print("  ROOT TOKEN — REVOKE AFTER POST-INIT CONFIGURE")
print("==============================================================================")
print()
print(f"  Root Token: {data['root_token']}")
print()
print("  This token has full sudo and is the ONLY way to perform the post-init")
print("  configure step (audit devices, AppRoles, PKI engines, policies).")
print()
print("  Immediately after running bootstrap.sh configure (which uses the same")
print("  /tmp/vault-root-token convention), the root token MUST be revoked:")
print("    vault token revoke <root-token>")
print("    shred -uz /root/vault-init-prod.json")
print()
PY

# ---------------------------------------------------------------------------
# 5. Final instructions
# ---------------------------------------------------------------------------
log ""
log "=========================================================="
log "CEREMONY COMPLETE — physically distribute the printed shares NOW."
log "=========================================================="
log ""
log "Vault is INITIALIZED but still SEALED. To unseal:"
log ""
log "  Option A (manual, three custodians present):"
log "    vault operator unseal   # custodian A enters their share"
log "    vault operator unseal   # custodian B enters their share"
log "    vault operator unseal   # custodian C enters their share"
log ""
log "  Option B (operationally automated via age-encrypted shares file):"
log "    1. Have three custodians age-encrypt their shares to /etc/personel/vault-shares.enc"
log "    2. Provision the master decryption key into systemd drop-in"
log "    3. systemctl start personel-vault-autounseal.service"
log ""
log "After unsealing, run the existing bootstrap.sh configure step:"
log "  /opt/personel/infra/compose/vault/bootstrap.sh configure"
log "  (It will pick up /tmp/vault-root-token if you copy it from"
log "   ${ROOT_TOKEN_FILE})"
log ""
log "FINAL CRITICAL STEP:"
log "  After configure completes, revoke the root token and shred the file:"
log "    vault token revoke \$(jq -r .root_token ${ROOT_TOKEN_FILE})"
log "    shred -uz ${ROOT_TOKEN_FILE}"
log ""
