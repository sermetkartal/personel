#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Vault Unseal
# TR: 3 Shamir payı ile Vault'u açar.
# EN: Unseals Vault with 3 Shamir shares.
# =============================================================================
set -euo pipefail

VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}"
VAULT_CACERT="${VAULT_CACERT:-/etc/personel/tls/tenant_ca.crt}"
export VAULT_ADDR VAULT_CACERT

log() { echo "[vault-unseal] $*"; }
die() { echo "[vault-unseal] ERROR: $*" >&2; exit 1; }

STATUS=$(docker exec personel-vault vault status -format=json \
  -address="${VAULT_ADDR}" -tls-skip-verify 2>/dev/null || echo '{"sealed":true,"initialized":false}')

INITIALIZED=$(echo "${STATUS}" | python3 -c "import json,sys; print(json.load(sys.stdin).get('initialized',False))")
SEALED=$(echo "${STATUS}" | python3 -c "import json,sys; print(json.load(sys.stdin).get('sealed',True))")

[[ "${INITIALIZED}" == "True" ]] || die "Vault is not initialized. Run: compose/vault/bootstrap.sh init"

if [[ "${SEALED}" == "False" ]]; then
  log "Vault is already unsealed."
  exit 0
fi

log "=== Vault Unseal — enter 3 of 5 Shamir shares ==="
log "TR: Kasayı açmak için 3 payı girin (toplam 5 paydan)."
log "EN: Enter 3 shares from the 5 tamper-evident envelopes."
echo ""

for i in 1 2 3; do
  read -r -s -p "  Share ${i} of 3: " SHARE
  echo
  docker exec -e VAULT_ADDR="${VAULT_ADDR}" personel-vault \
    vault operator unseal "${SHARE}" -tls-skip-verify >/dev/null \
    || die "Share ${i} rejected"
  log "Share ${i} accepted."
done

# Verify unsealed
sleep 2
STATUS_AFTER=$(docker exec personel-vault vault status -format=json \
  -address="${VAULT_ADDR}" -tls-skip-verify 2>/dev/null || echo '{"sealed":true}')
SEALED_AFTER=$(echo "${STATUS_AFTER}" | python3 -c "import json,sys; print(json.load(sys.stdin).get('sealed',True))")

if [[ "${SEALED_AFTER}" == "False" ]]; then
  log "Vault successfully unsealed."
else
  die "Vault is still sealed after 3 shares. Verify the correct shares were used."
fi
