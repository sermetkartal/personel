#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Vault Bootstrap Script
# Shamir unseal ceremony wrapper for Phase 1 on-prem deployments.
# Per vault-setup.md §3
#
# Usage:
#   ./bootstrap.sh init        — Initialize Vault, print key shares (CEREMONY)
#   ./bootstrap.sh unseal      — Enter 3 Shamir shares to unseal
#   ./bootstrap.sh configure   — Post-init: enable auth, engines, policies, AppRoles
#   ./bootstrap.sh status      — Print current seal status
#
# TR: Bu betik Vault'un ilk kurulumunda bir kez çalıştırılır.
#     Anahtar paylaşımları fiziksel olarak güvenli zarflarda saklanmalıdır.
# EN: This script is run once during initial Vault setup.
#     Key shares must be stored in physically secure tamper-evident envelopes.
# =============================================================================
set -euo pipefail

VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}"
VAULT_CACERT="${VAULT_CACERT:-/etc/personel/tls/tenant_ca.crt}"
POLICY_DIR="${POLICY_DIR:-/etc/personel/vault/policies}"
TENANT_ID="${PERSONEL_TENANT_ID:-}"

export VAULT_ADDR VAULT_CACERT

log()  { echo "[vault-bootstrap] $*"; }
die()  { echo "[vault-bootstrap] ERROR: $*" >&2; exit 1; }
warn() { echo "[vault-bootstrap] WARN: $*" >&2; }

# ---------------------------------------------------------------------------
cmd_status() {
  vault status || true
}

# ---------------------------------------------------------------------------
cmd_init() {
  log "=========================================================="
  log "VAULT INITIALIZATION — SHAMIR 3-OF-5"
  log "This ceremony must be performed with authorized witnesses."
  log "=========================================================="

  if vault status 2>/dev/null | grep -q "Initialized.*true"; then
    die "Vault is already initialized. Use 'unseal' to unseal."
  fi

  local init_output
  init_output=$(vault operator init \
    -key-shares=5 \
    -key-threshold=3 \
    -format=json)

  echo "${init_output}" | python3 -c "
import json, sys
data = json.load(sys.stdin)
print()
print('=== UNSEAL KEYS (distribute to custodians) ===')
for i, k in enumerate(data['unseal_keys_b64'], 1):
    print(f'  Share {i}: {k}')
print()
print('=== ROOT TOKEN (SEAL IMMEDIATELY AFTER SETUP) ===')
print(f'  Root Token: {data[\"root_token\"]}')
print()
print('CRITICAL: Store each share in a separate tamper-evident envelope.')
print('Custodian assignments per pki-bootstrap.md §3.2:')
print('  share-1,2: Customer security officer')
print('  share-3:   Security-engineer team lead')
print('  share-4:   Customer DPO')
print('  share-5:   Customer HQ fireproof safe')
"

  log ""
  log "Root token saved temporarily to: /tmp/vault-root-token (chmod 600)"
  log "IT MUST BE REVOKED after configure step. Do NOT store long-term."
  echo "${init_output}" | python3 -c "
import json, sys
data = json.load(sys.stdin)
print(data['root_token'])
" > /tmp/vault-root-token
  chmod 600 /tmp/vault-root-token

  log "Initialization complete. Run 'unseal' then 'configure'."
}

# ---------------------------------------------------------------------------
cmd_unseal() {
  log "=== VAULT UNSEAL — enter 3 Shamir shares ==="
  if vault status 2>/dev/null | grep -q "Sealed.*false"; then
    log "Vault is already unsealed."
    return 0
  fi

  for i in 1 2 3; do
    local share
    read -r -s -p "  Share ${i} of 3: " share
    echo
    vault operator unseal "${share}"
    log "Share ${i} accepted."
  done

  if vault status 2>/dev/null | grep -q "Sealed.*false"; then
    log "Vault successfully unsealed."
  else
    die "Vault is still sealed after 3 shares. Verify the shares are correct."
  fi
}

# ---------------------------------------------------------------------------
cmd_configure() {
  [[ -f /tmp/vault-root-token ]] || die "Root token file not found at /tmp/vault-root-token. Run 'init' first."
  export VAULT_TOKEN
  VAULT_TOKEN=$(cat /tmp/vault-root-token)

  log "=== STEP 1: Enable audit devices ==="
  vault audit enable file \
    file_path=/vault/data/audit.log \
    log_raw=false \
    hmac_accessor=true \
    || log "File audit already enabled"

  vault audit enable syslog \
    tag="vault-audit" \
    facility="LOCAL6" \
    || log "Syslog audit already enabled"

  log "=== STEP 2: Enable auth methods ==="
  vault auth enable approle || log "AppRole already enabled"

  log "=== STEP 3: Enable secrets engines ==="
  vault secrets enable -path=transit transit || log "Transit already enabled"
  vault secrets enable -path=kv -version=2 kv || log "KV v2 already enabled"

  # PKI engines (tenant CA placeholder — pki-bootstrap.sh fills in real certs)
  if [[ -n "${TENANT_ID}" ]]; then
    vault secrets enable -path="pki/tenant/${TENANT_ID}" pki \
      || log "Tenant PKI already enabled"
    vault secrets tune -max-lease-ttl=26280h "pki/tenant/${TENANT_ID}" \
      || true

    vault secrets enable -path="pki/tenant/${TENANT_ID}/agents" pki \
      || log "Agent PKI already enabled"
    vault secrets tune -max-lease-ttl=17520h "pki/tenant/${TENANT_ID}/agents" \
      || true

    vault secrets enable -path="pki/tenant/${TENANT_ID}/servers" pki \
      || log "Server PKI already enabled"
    vault secrets tune -max-lease-ttl=17520h "pki/tenant/${TENANT_ID}/servers" \
      || true

    log "=== STEP 4: Create Tenant Master Key (TMK) ==="
    vault write -f "transit/keys/tenant/${TENANT_ID}/tmk" \
      type=aes256-gcm96 \
      derived=true \
      exportable=false \
      allow_plaintext_backup=false \
      || log "TMK already exists"

    vault write "transit/keys/tenant/${TENANT_ID}/tmk/config" \
      min_decryption_version=1 \
      min_encryption_version=0 \
      deletion_allowed=false \
      auto_rotate_period=8760h \
      || log "TMK config already set"
  else
    warn "PERSONEL_TENANT_ID not set — skipping tenant-specific PKI and TMK setup"
  fi

  log "=== STEP 5: Initialize PKI deny-list in KV ==="
  vault kv put kv/pki/deny-list serials="[]" || log "PKI deny-list already set"

  log "=== STEP 6: Write Vault policies ==="
  for policy_file in "${POLICY_DIR}"/*.hcl; do
    local policy_name
    policy_name=$(basename "${policy_file}" .hcl)
    vault policy write "${policy_name}" "${policy_file}"
    log "  Written policy: ${policy_name}"
  done

  log "=== STEP 7: Create AppRole roles ==="
  # agent-enrollment: single-use, 15 min
  vault write auth/approle/role/agent-enrollment \
    token_policies=agent-enrollment \
    secret_id_num_uses=1 \
    secret_id_ttl=15m \
    token_ttl=15m \
    token_max_ttl=15m \
    || log "agent-enrollment role already exists"

  # dlp-service: renewable, 1h token
  vault write auth/approle/role/dlp-service \
    token_policies=dlp-service \
    secret_id_num_uses=0 \
    secret_id_ttl=24h \
    token_ttl=1h \
    token_max_ttl=24h \
    || log "dlp-service role already exists"

  # gateway-service: renewable, 1h token
  vault write auth/approle/role/gateway-service \
    token_policies=gateway-service \
    secret_id_num_uses=0 \
    secret_id_ttl=24h \
    token_ttl=1h \
    token_max_ttl=24h \
    || log "gateway-service role already exists"

  # admin-api: renewable, 1h token
  vault write auth/approle/role/admin-api \
    token_policies=admin-api,agent-enrollment \
    secret_id_num_uses=0 \
    secret_id_ttl=24h \
    token_ttl=1h \
    token_max_ttl=24h \
    || log "admin-api role already exists"

  # backup-operator: daily rotation
  vault write auth/approle/role/backup-operator \
    token_policies=backup-operator \
    secret_id_num_uses=0 \
    secret_id_ttl=24h \
    token_ttl=1h \
    token_max_ttl=2h \
    || log "backup-operator role already exists"

  log "=== STEP 8: Print Role IDs (add to .env) ==="
  for role in agent-enrollment dlp-service gateway-service admin-api backup-operator; do
    local role_id
    role_id=$(vault read -field=role_id "auth/approle/role/${role}/role-id")
    log "  ${role}: ${role_id}"
  done

  log "=== STEP 9: Create break-glass token ==="
  vault policy write break-glass - <<'EOF' || log "break-glass policy already written"
path "*" {
  capabilities = ["create", "read", "update", "delete", "list", "sudo"]
}
EOF
  local bg_token
  bg_token=$(vault token create \
    -policy=break-glass \
    -ttl=8760h \
    -orphan \
    -display-name=break-glass \
    -field=token)
  log "  Break-glass token generated. SEAL IN TAMPER-EVIDENT ENVELOPE:"
  log "  ${bg_token}"
  log "  This token is NOT stored on disk. Copy it now."

  log "=== STEP 10: Revoke root token ==="
  vault token revoke "${VAULT_TOKEN}"
  shred -uz /tmp/vault-root-token 2>/dev/null || rm -f /tmp/vault-root-token
  unset VAULT_TOKEN
  log "Root token revoked and removed."

  log "=========================================================="
  log "Vault configuration complete."
  log "Next: run scripts/ca-bootstrap.sh to initialize the PKI."
  log "=========================================================="
}

# ---------------------------------------------------------------------------
main() {
  local cmd="${1:-status}"
  case "${cmd}" in
    init)      cmd_init      ;;
    unseal)    cmd_unseal    ;;
    configure) cmd_configure ;;
    status)    cmd_status    ;;
    *)
      echo "Usage: $0 {init|unseal|configure|status}"
      exit 1
      ;;
  esac
}

main "$@"
