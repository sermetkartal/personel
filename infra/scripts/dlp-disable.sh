#!/usr/bin/env bash
# dlp-disable.sh — ADR 0013 opt-out for the Personel DLP service.
#
# TR: DLP servisini devre dışı bırakır. Mevcut şifreli klavye ciphertext
#     blob'ları 14 günlük TTL'ye bırakılır (ADR 0013 A4) — forensic
#     continuity için hemen silinmez.
#
# EN: Disables the DLP service. Existing encrypted keystroke ciphertext
#     blobs are left to age out naturally via their 14-day TTL (ADR 0013
#     A4) — they are NOT destroyed immediately, to preserve forensic
#     continuity for incidents that began before disable.
#
# Flow:
#   1. Verify current state = enabled
#   2. Stop DLP container
#   3. Revoke Vault Secret ID
#   4. Record dlp.disabled audit event
#   5. Update transparency portal banner
#   6. Validate state = disabled
#
# Usage:
#   sudo ./infra/scripts/dlp-disable.sh --actor-id "dpo-001" --reason "routine_rotation"
#
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/../.." &>/dev/null && pwd)"
COMPOSE_FILE="$REPO_ROOT/infra/compose/docker-compose.yaml"
LOG_PREFIX="[dlp-disable]"

ACTOR_ID=""
REASON=""
FORCE="0"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --actor-id) ACTOR_ID="$2"; shift 2 ;;
    --reason)   REASON="$2";   shift 2 ;;
    --force)    FORCE="1";     shift 1 ;;
    -h|--help)  sed -n '1,30p' "$0"; exit 0 ;;
    *) echo "$LOG_PREFIX unknown arg: $1" >&2; exit 2 ;;
  esac
done

[[ -n "$ACTOR_ID" && -n "$REASON" ]] \
  || { echo "$LOG_PREFIX ERROR: --actor-id and --reason are required" >&2; exit 2; }

# iso_date: portable ISO-8601 timestamp — BSD date (macOS) uses -u +format;
# GNU date uses --iso-8601=seconds. Both output the same format.
iso_date() { date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date --iso-8601=seconds; }
log()  { echo "$LOG_PREFIX $(iso_date) $*"; }
err()  { echo "$LOG_PREFIX $(iso_date) ERROR: $*" >&2; }
need() { command -v "$1" &>/dev/null || { err "required: $1"; exit 3; }; }

need docker
need curl
need jq
need vault

VAULT_ADDR="${VAULT_ADDR:-https://vault.personel.internal:8200}"
API_URL="${API_URL:-https://api.personel.internal}"
export VAULT_ADDR

# ---- Step 1: verify current state = enabled --------------------------------

log "step 1/6: verifying current DLP state"
CURRENT_STATE="$(curl -sS "$API_URL/v1/system/dlp-state" \
  -H "Authorization: Bearer ${API_BEARER:-}" | jq -r '.state // "unknown"')"

if [[ "$CURRENT_STATE" != "enabled" && "$FORCE" != "1" ]]; then
  err "current state is '$CURRENT_STATE' — expected 'enabled'. Use --force to override."
  exit 4
fi
log "  current state: $CURRENT_STATE"

# ---- Step 2: stop DLP container --------------------------------------------

log "step 2/6: stopping DLP container"
docker compose -f "$COMPOSE_FILE" --profile dlp stop dlp || true
docker compose -f "$COMPOSE_FILE" --profile dlp rm -f dlp || true

# ---- Step 3: revoke Vault Secret ID ----------------------------------------

log "step 3/6: revoking Vault Secret ID for dlp-service AppRole"
# List all current secret IDs and destroy them
ACCESSORS="$(vault list -format=json "auth/approle/role/dlp-service/secret-id" 2>/dev/null \
              | jq -r '.[]? // empty')"
if [[ -n "$ACCESSORS" ]]; then
  while IFS= read -r accessor; do
    [[ -z "$accessor" ]] && continue
    log "  destroying secret_id_accessor: $accessor"
    vault write -f "auth/approle/role/dlp-service/secret-id-accessor/destroy" \
      secret_id_accessor="$accessor" >/dev/null || true
  done <<<"$ACCESSORS"
else
  log "  no active Secret IDs found (already revoked?)"
fi

# Clean up the on-disk secret file
sudo rm -f /run/personel/dlp-secret-id || true

# ---- Step 4: atomic state transition + audit + banner (single API call) ----

log "step 4/6: invoking POST /v1/system/dlp-transition action=disable-complete"
curl -sS --fail-with-body -X POST \
  "$API_URL/v1/system/dlp-transition" \
  -H "Authorization: Bearer ${API_DLPADMIN_TOKEN:-}" \
  -H "Content-Type: application/json" \
  -d "$(jq -nc --arg actor "$ACTOR_ID" --arg reason "$REASON" \
        '{action:"disable-complete", actor_id:$actor, reason:$reason}')" >/dev/null

# ---- Step 5: banner is set atomically by step 4 (no-op placeholder) --------

log "step 5/6: transparency portal banner updated by transition handler"

# ---- Step 6: validate state -----------------------------------------------

log "step 6/6: validating new DLP state"
FINAL_STATE="$(curl -sS "$API_URL/v1/system/dlp-state" \
  -H "Authorization: Bearer ${API_BEARER:-}" | jq -r '.state')"

[[ "$FINAL_STATE" = "disabled" ]] \
  || { err "post-opt-out state is '$FINAL_STATE', expected 'disabled'"; exit 5; }

log "✓ DLP disable complete. State: disabled."
log ""
log "NOTE on ciphertext blobs (ADR 0013 A4):"
log "  Existing encrypted keystroke content remains in MinIO until its"
log "  14-day TTL expires. This is intentional — forensic continuity for"
log "  incidents that began before disable. To force immediate destruction,"
log "  use: sudo ./infra/scripts/dlp-emergency-purge.sh (DPO + CISO only)."
exit 0
