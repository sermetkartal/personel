#!/usr/bin/env bash
# dlp-enable.sh — ADR 0013 opt-in ceremony for the Personel DLP service.
#
# TR: Bu script yalnızca müşteri DPO'su imzalı onay formunu hazırladıktan
#     sonra çalıştırılmalıdır. Çalıştıran operator'ın `vault-admin` rolü
#     gerekir. Başarısızlık durumunda tüm işlemler otomatik geri alınır.
#
# EN: This script must only be run AFTER the customer DPO has prepared a
#     signed opt-in form. The invoking operator needs the `vault-admin`
#     role. Any failure triggers automatic rollback (ADR 0013 A3).
#
# Flow (ADR 0013):
#   1. Verify signed form present and readable
#   2. Verify prerequisites (Vault unsealed, DLP AppRole created, state=disabled)
#   3. Issue one-time AppRole Secret ID to dlp-service
#   4. Start DLP container via `docker compose --profile dlp up -d`
#   5. Wait for DLP container health = healthy
#   6. Bootstrap PE-DEKs for already-enrolled endpoints (ADR 0013 A2)
#   7. Record dlp.enabled audit event with form hash
#   8. Surface transparency portal banner via admin API
#   9. Validate new state via GET /v1/system/dlp-state
#
# On failure at any step, rollback in reverse order.
#
# Usage:
#   sudo ./infra/scripts/dlp-enable.sh \
#     --form /var/lib/personel/dlp/opt-in-signed.pdf \
#     --dpo-email dpo@customer.com.tr \
#     --actor-id "dpo-001"
#
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/../.." &>/dev/null && pwd)"
COMPOSE_DIR="$REPO_ROOT/infra/compose"
COMPOSE_FILE="$COMPOSE_DIR/docker-compose.yaml"
LOG_PREFIX="[dlp-enable]"

# ---- Parse args --------------------------------------------------------------

FORM_PATH=""
DPO_EMAIL=""
ACTOR_ID=""
AYDINLATMA_VERSION="1.0"
SKIP_FORM_CHECK="0"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --form)                   FORM_PATH="$2";          shift 2 ;;
    --dpo-email)              DPO_EMAIL="$2";          shift 2 ;;
    --actor-id)               ACTOR_ID="$2";           shift 2 ;;
    --aydinlatma-version)     AYDINLATMA_VERSION="$2"; shift 2 ;;
    --skip-form-check)        SKIP_FORM_CHECK="1";     shift 1 ;;  # test-only, DO NOT use in prod
    -h|--help)
      sed -n '1,40p' "$0"
      exit 0 ;;
    *)
      echo "$LOG_PREFIX unknown arg: $1" >&2
      exit 2 ;;
  esac
done

if [[ -z "$FORM_PATH" || -z "$DPO_EMAIL" || -z "$ACTOR_ID" ]]; then
  echo "$LOG_PREFIX ERROR: --form, --dpo-email, and --actor-id are required" >&2
  exit 2
fi

# ---- Helpers -----------------------------------------------------------------

log()  { echo "$LOG_PREFIX $(date --iso-8601=seconds) $*"; }
err()  { echo "$LOG_PREFIX $(date --iso-8601=seconds) ERROR: $*" >&2; }
need() { command -v "$1" &>/dev/null || { err "required command missing: $1"; exit 3; }; }

need docker
need sha256sum
need curl
need jq
need vault

VAULT_ADDR="${VAULT_ADDR:-https://vault.personel.internal:8200}"
API_URL="${API_URL:-https://api.personel.internal}"
export VAULT_ADDR

# ---- Rollback state tracking -------------------------------------------------

ROLLBACK_SECRET_ID=""
ROLLBACK_CONTAINER_STARTED="0"
ROLLBACK_AUDIT_WRITTEN="0"

on_failure() {
  local rc=$?
  err "ceremony failed with exit $rc — initiating rollback (ADR 0013 A3)"

  if [[ "$ROLLBACK_CONTAINER_STARTED" = "1" ]]; then
    log "rollback: stopping DLP container"
    docker compose -f "$COMPOSE_FILE" --profile dlp down dlp || true
  fi

  if [[ -n "$ROLLBACK_SECRET_ID" ]]; then
    log "rollback: destroying Vault Secret ID"
    vault write -f "auth/approle/role/dlp-service/secret-id/destroy" \
      secret_id="$ROLLBACK_SECRET_ID" 2>/dev/null || true
  fi

  if [[ "$ROLLBACK_AUDIT_WRITTEN" = "1" ]]; then
    log "rollback: invoking POST /v1/system/dlp-transition action=enable-failed"
    curl -sS -X POST "$API_URL/v1/system/dlp-transition" \
      -H "Authorization: Bearer ${API_DLPADMIN_TOKEN:-}" \
      -H "Content-Type: application/json" \
      -d "$(jq -nc --arg actor "$ACTOR_ID" --arg reason "ceremony_rollback_exit_$rc" \
            '{action:"enable-failed", actor_id:$actor, reason:$reason}')" \
      >/dev/null 2>&1 || true
  fi

  err "rollback complete. DLP remains DISABLED."
  exit "$rc"
}
trap on_failure ERR

# ---- Step 1: verify signed form ---------------------------------------------

if [[ "$SKIP_FORM_CHECK" != "1" ]]; then
  log "step 1/9: verifying signed form at $FORM_PATH"
  [[ -r "$FORM_PATH" ]] || { err "form file not readable: $FORM_PATH"; exit 4; }

  # Require file size > 100 bytes (anti-placeholder)
  size="$(stat -c '%s' "$FORM_PATH" 2>/dev/null || stat -f '%z' "$FORM_PATH")"
  (( size > 100 )) || { err "form file too small ($size bytes) — placeholder?"; exit 4; }

  FORM_HASH="$(sha256sum "$FORM_PATH" | awk '{print $1}')"
  log "  form sha256: $FORM_HASH"
else
  log "step 1/9: SKIPPING form check (--skip-form-check, test mode)"
  FORM_HASH="0000000000000000000000000000000000000000000000000000000000000000"
fi

# ---- Step 2: verify prerequisites -------------------------------------------

log "step 2/9: verifying prerequisites"

# Vault unsealed?
vault status &>/dev/null || { err "Vault not unsealed — run vault-unseal.sh first"; exit 5; }
log "  vault: unsealed ✓"

# DLP AppRole exists?
vault read "auth/approle/role/dlp-service" &>/dev/null \
  || { err "dlp-service AppRole missing — run install.sh first"; exit 5; }
log "  vault dlp-service AppRole: exists ✓"

# Current DLP state = disabled?
CURRENT_STATE="$(curl -sS "$API_URL/v1/system/dlp-state" \
  -H "Authorization: Bearer ${API_BEARER:-}" | jq -r '.state // "unknown"')"
if [[ "$CURRENT_STATE" != "disabled" && "$CURRENT_STATE" != "error" ]]; then
  err "current state is '$CURRENT_STATE' — expected 'disabled'. Aborting."
  exit 5
fi
log "  current DLP state: $CURRENT_STATE ✓"

# ---- Step 3: issue Vault Secret ID ------------------------------------------

log "step 3/9: issuing one-time Vault Secret ID for dlp-service"

SECRET_ID_JSON="$(vault write -format=json -f "auth/approle/role/dlp-service/secret-id" \
  metadata='{"ceremony_actor":"'$ACTOR_ID'","form_hash":"'$FORM_HASH'"}')"
SECRET_ID="$(echo "$SECRET_ID_JSON" | jq -r '.data.secret_id')"
SECRET_ID_ACCESSOR="$(echo "$SECRET_ID_JSON" | jq -r '.data.secret_id_accessor')"

[[ -n "$SECRET_ID" && "$SECRET_ID" != "null" ]] \
  || { err "failed to extract Secret ID"; exit 6; }

ROLLBACK_SECRET_ID="$SECRET_ID_ACCESSOR"
log "  secret_id_accessor: $SECRET_ID_ACCESSOR (stored for rollback)"

# Write to temporary file with restrictive permissions for the DLP container
SECRET_ID_FILE="/run/personel/dlp-secret-id"
sudo mkdir -p "$(dirname "$SECRET_ID_FILE")"
echo "$SECRET_ID" | sudo tee "$SECRET_ID_FILE" >/dev/null
sudo chmod 0600 "$SECRET_ID_FILE"
sudo chown 65532:65532 "$SECRET_ID_FILE"

# ---- Step 4: start DLP container --------------------------------------------

log "step 4/9: starting DLP container via compose profile"
DLP_VAULT_SECRET_ID_FILE="$SECRET_ID_FILE" docker compose \
  -f "$COMPOSE_FILE" --profile dlp up -d dlp
ROLLBACK_CONTAINER_STARTED="1"

# ---- Step 5: wait for container healthy -------------------------------------

log "step 5/9: waiting up to 90s for DLP container to become healthy"
deadline=$(( $(date +%s) + 90 ))
while true; do
  health="$(docker inspect --format='{{.State.Health.Status}}' personel-dlp 2>/dev/null || echo "starting")"
  [[ "$health" = "healthy" ]] && { log "  dlp healthy ✓"; break; }
  (( $(date +%s) > deadline )) && { err "dlp health check timed out (last status: $health)"; exit 7; }
  sleep 3
done

# ---- Step 6: PE-DEK bootstrap for enrolled endpoints (ADR 0013 A2) ----------

log "step 6/9: bootstrapping PE-DEKs for already-enrolled endpoints"
BOOTSTRAP_RESP="$(curl -sS --fail-with-body -X POST \
  "$API_URL/v1/system/dlp-bootstrap-keys" \
  -H "Authorization: Bearer ${API_DLPADMIN_TOKEN:-}" \
  -H "Content-Type: application/json")"

TOTAL="$(echo "$BOOTSTRAP_RESP" | jq -r '.total_endpoints')"
BOOTSTRAPPED="$(echo "$BOOTSTRAP_RESP" | jq -r '.bootstrapped')"
FAILED="$(echo "$BOOTSTRAP_RESP" | jq -r '.failed')"

log "  bootstrap: total=$TOTAL bootstrapped=$BOOTSTRAPPED failed=$FAILED"
(( FAILED > 0 )) && { err "bootstrap had $FAILED failures — see API logs"; exit 8; }

# ---- Step 7: atomic state transition + audit + banner (single API call) ----

# The API owns state consistency: writing the dlp.enabled audit entry,
# updating the dlp_state table, and surfacing the transparency portal banner
# all happen in one handler call. See apps/api/internal/dlpstate/service.go
# Transition() for the implementation.

log "step 7/9: invoking POST /v1/system/dlp-transition action=enable-complete"
curl -sS --fail-with-body -X POST \
  "$API_URL/v1/system/dlp-transition" \
  -H "Authorization: Bearer ${API_DLPADMIN_TOKEN:-}" \
  -H "Content-Type: application/json" \
  -d "$(jq -nc \
        --arg action "enable-complete" \
        --arg actor "$ACTOR_ID" \
        --arg dpo_email "$DPO_EMAIL" \
        --arg form_hash "$FORM_HASH" \
        --arg bootstrapped "$BOOTSTRAPPED" \
        '{
          action:$action,
          actor_id:$actor,
          dpo_email:$dpo_email,
          form_hash:$form_hash,
          endpoints_bootstrapped:($bootstrapped|tonumber)
         }')" >/dev/null
ROLLBACK_AUDIT_WRITTEN="1"

# ---- Step 8: banner is set atomically by step 7 (no-op placeholder) --------

log "step 8/9: transparency portal banner updated by transition handler"

# ---- Step 9: validate state ------------------------------------------------

log "step 9/9: validating new DLP state"
FINAL_STATE="$(curl -sS "$API_URL/v1/system/dlp-state" \
  -H "Authorization: Bearer ${API_BEARER:-}" | jq -r '.state')"

[[ "$FINAL_STATE" = "enabled" ]] \
  || { err "post-ceremony state is '$FINAL_STATE', expected 'enabled'"; exit 9; }

trap - ERR
log "✓ DLP enable ceremony complete. State: enabled."
log "  Employees will see the activation banner on next portal visit."
log "  To disable: sudo ./infra/scripts/dlp-disable.sh --actor-id $ACTOR_ID"
exit 0
