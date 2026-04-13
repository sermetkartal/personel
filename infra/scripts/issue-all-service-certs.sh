#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Bulk Service TLS Issuance
# =============================================================================
# Issues a Vault-PKI server cert for every entry in service-tls-inventory.yaml.
# Designed for the initial migration (Faz 5 Madde 53) that takes the stack
# from mixed self-signed + plaintext to all-TLS-from-Vault-PKI.
#
# Idempotent: by default skips services whose cert exists with > MIN_DAYS_LEFT
# remaining. Pass --force to re-issue regardless.
#
# Usage:
#   VAULT_TOKEN=hvs.xxxx ./issue-all-service-certs.sh             # idempotent
#   VAULT_TOKEN=hvs.xxxx ./issue-all-service-certs.sh --force     # re-issue all
#   VAULT_TOKEN=hvs.xxxx ./issue-all-service-certs.sh --service api  # one only
#   VAULT_TOKEN=hvs.xxxx ./issue-all-service-certs.sh --dry-run
#
# Environment:
#   VAULT_TOKEN     (required) — token with pki/issue/server-cert capability
#   VAULT_ADDR      (optional) — default https://127.0.0.1:8200
#   TLS_DIR         (optional) — default /etc/personel/tls
#   PKI_MOUNT       (optional) — default pki
#   PKI_ROLE        (optional) — default server-cert
#   MIN_DAYS_LEFT   (optional) — default 7   (skip if more days remain)
#   INVENTORY_FILE  (optional) — default <script_dir>/service-tls-inventory.yaml
#
# Exit codes:
#   0 — all OK (or all due services issued cleanly)
#   1 — usage / config error
#   2 — partial failure (some services failed; continued past them)
#   3 — total failure (none succeeded)
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INVENTORY_FILE="${INVENTORY_FILE:-${SCRIPT_DIR}/service-tls-inventory.yaml}"
TLS_DIR="${TLS_DIR:-/etc/personel/tls}"
VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}"
PKI_MOUNT="${PKI_MOUNT:-pki}"
PKI_ROLE="${PKI_ROLE:-server-cert}"
MIN_DAYS_LEFT="${MIN_DAYS_LEFT:-7}"
DEFAULT_TTL="720h"

FORCE=false
DRY_RUN=false
ONLY_SERVICE=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --force)    FORCE=true; shift ;;
    --dry-run)  DRY_RUN=true; shift ;;
    --service)  ONLY_SERVICE="${2:-}"; shift 2 ;;
    -h|--help)
      sed -n '2,30p' "$0"
      exit 0
      ;;
    *)
      echo "unknown flag: $1" >&2
      exit 1
      ;;
  esac
done

log()  { printf '[issue-certs] %s\n' "$*"; }
warn() { printf '[issue-certs] WARN: %s\n' "$*" >&2; }
err()  { printf '[issue-certs] ERROR: %s\n' "$*" >&2; }

# -----------------------------------------------------------------------------
# Pre-flight
# -----------------------------------------------------------------------------
if [[ -z "${VAULT_TOKEN:-}" ]]; then
  err "VAULT_TOKEN environment variable is required"
  exit 1
fi
export VAULT_ADDR VAULT_TOKEN

if ! command -v vault >/dev/null 2>&1; then
  err "vault CLI not found in PATH"
  exit 1
fi
if ! command -v jq >/dev/null 2>&1; then
  err "jq not found in PATH (required to parse Vault JSON output)"
  exit 1
fi
if ! command -v openssl >/dev/null 2>&1; then
  err "openssl not found in PATH (required for cert expiry check)"
  exit 1
fi
if [[ ! -f "${INVENTORY_FILE}" ]]; then
  err "inventory file not found: ${INVENTORY_FILE}"
  exit 1
fi

if [[ "${DRY_RUN}" == "false" ]]; then
  if [[ ! -d "${TLS_DIR}" ]]; then
    log "creating TLS_DIR ${TLS_DIR}"
    mkdir -p "${TLS_DIR}"
  fi
  if ! vault token lookup >/dev/null 2>&1; then
    err "VAULT_TOKEN is invalid or vault is unreachable at ${VAULT_ADDR}"
    exit 1
  fi
fi

# -----------------------------------------------------------------------------
# YAML parser (minimal, no external deps).
# Emits one record per service to stdout in the form:
#   name|cn|sans_csv|ip_sans_csv
# -----------------------------------------------------------------------------
parse_inventory() {
  awk '
    BEGIN { in_services=0; have_record=0 }
    /^services:/ { in_services=1; next }
    in_services && /^[a-zA-Z]/ { in_services=0 }
    !in_services { next }
    /^  - name:/ {
      if (have_record) {
        printf "%s|%s|%s|%s\n", name, cn, sans, ip_sans
      }
      have_record=1
      name=$0; sub(/^  - name:[[:space:]]*/, "", name)
      cn=""; sans=""; ip_sans=""
      next
    }
    /^    cn:/ {
      cn=$0; sub(/^    cn:[[:space:]]*/, "", cn); next
    }
    /^    sans:/ {
      sans=$0; sub(/^    sans:[[:space:]]*\[/, "", sans); sub(/\][[:space:]]*$/, "", sans); gsub(/[[:space:]]/, "", sans); next
    }
    /^    ip_sans:/ {
      ip_sans=$0; sub(/^    ip_sans:[[:space:]]*\[/, "", ip_sans); sub(/\][[:space:]]*$/, "", ip_sans); gsub(/[[:space:]]/, "", ip_sans); next
    }
    END {
      if (have_record) {
        printf "%s|%s|%s|%s\n", name, cn, sans, ip_sans
      }
    }
  ' "${INVENTORY_FILE}"
}

# -----------------------------------------------------------------------------
# Cert expiry helper. Echoes integer days remaining; -1 if cert missing/unparseable.
# -----------------------------------------------------------------------------
days_remaining() {
  local crt="$1"
  if [[ ! -f "${crt}" ]]; then
    echo "-1"; return
  fi
  local end_str end_epoch now_epoch
  if ! end_str=$(openssl x509 -enddate -noout -in "${crt}" 2>/dev/null); then
    echo "-1"; return
  fi
  end_str="${end_str#notAfter=}"
  if ! end_epoch=$(date -d "${end_str}" +%s 2>/dev/null); then
    echo "-1"; return
  fi
  now_epoch=$(date +%s)
  echo $(( (end_epoch - now_epoch) / 86400 ))
}

# -----------------------------------------------------------------------------
# Issue one cert. Returns 0 on success, 1 on failure.
# -----------------------------------------------------------------------------
issue_one() {
  local name="$1" cn="$2" sans="$3" ip_sans="$4"
  local crt="${TLS_DIR}/${name}.crt"
  local key="${TLS_DIR}/${name}.key"
  local chain="${TLS_DIR}/${name}-chain.pem"

  # idempotency check
  if [[ "${FORCE}" == "false" && -f "${crt}" ]]; then
    local left
    left=$(days_remaining "${crt}")
    if [[ "${left}" -gt "${MIN_DAYS_LEFT}" ]]; then
      log "  SKIP ${name} — cert valid for ${left}d (> ${MIN_DAYS_LEFT}d threshold)"
      return 2  # convention: 2 = skipped
    fi
  fi

  if [[ "${DRY_RUN}" == "true" ]]; then
    log "  DRY-RUN ${name}: would issue cn=${cn} sans=${sans} ip_sans=${ip_sans} ttl=${DEFAULT_TTL}"
    return 0
  fi

  log "  ISSUE ${name}: cn=${cn}"
  local resp
  if ! resp=$(vault write -format=json "${PKI_MOUNT}/issue/${PKI_ROLE}" \
        common_name="${cn}" \
        alt_names="${sans}" \
        ip_sans="${ip_sans}" \
        ttl="${DEFAULT_TTL}" 2>&1); then
    err "  vault issue failed for ${name}:"
    err "  ${resp}"
    return 1
  fi

  # extract from Vault response
  local cert_pem key_pem ca_pem
  cert_pem=$(jq -r '.data.certificate' <<<"${resp}")
  key_pem=$(jq -r '.data.private_key' <<<"${resp}")
  ca_pem=$(jq -r '.data.issuing_ca' <<<"${resp}")

  if [[ -z "${cert_pem}" || "${cert_pem}" == "null" ]]; then
    err "  vault response missing certificate for ${name}"
    return 1
  fi

  # write atomically (write to .new, then mv)
  umask 022
  printf '%s\n' "${cert_pem}" > "${crt}.new"
  printf '%s\n%s\n' "${cert_pem}" "${ca_pem}" > "${chain}.new"
  printf '%s\n' "${key_pem}"  > "${key}.new"

  chmod 644 "${crt}.new" "${chain}.new"
  # Note: 644 on the key is intentional — see Faz 1 Madde 5 gateway lesson.
  # Some non-root containers (gateway) need world-read to load the key on startup.
  # Compensating control: TLS_DIR itself should be 0755 root:root and the
  # parent host filesystem ACL'd via systemd ProtectSystem + DAC.
  chmod 644 "${key}.new"

  mv -f "${crt}.new"   "${crt}"
  mv -f "${chain}.new" "${chain}"
  mv -f "${key}.new"   "${key}"

  log "  OK ${name} → ${crt}"
  return 0
}

# -----------------------------------------------------------------------------
# Main loop
# -----------------------------------------------------------------------------
log "=== Personel TLS bulk issuance ==="
log "vault   : ${VAULT_ADDR}"
log "pki     : ${PKI_MOUNT}/issue/${PKI_ROLE}"
log "tls dir : ${TLS_DIR}"
log "force   : ${FORCE}"
log "dry-run : ${DRY_RUN}"
[[ -n "${ONLY_SERVICE}" ]] && log "only    : ${ONLY_SERVICE}"
log ""

issued=0
skipped=0
failed=0
failed_names=()

while IFS='|' read -r name cn sans ip_sans; do
  [[ -z "${name}" ]] && continue
  if [[ -n "${ONLY_SERVICE}" && "${name}" != "${ONLY_SERVICE}" ]]; then
    continue
  fi

  set +e
  issue_one "${name}" "${cn}" "${sans}" "${ip_sans}"
  rc=$?
  set -e

  case "${rc}" in
    0) issued=$((issued+1)) ;;
    2) skipped=$((skipped+1)) ;;
    *) failed=$((failed+1)); failed_names+=("${name}") ;;
  esac
done < <(parse_inventory)

log ""
log "=== Summary ==="
log "issued : ${issued}"
log "skipped: ${skipped}"
log "failed : ${failed}"
if [[ "${failed}" -gt 0 ]]; then
  log "failed services: ${failed_names[*]}"
fi

if [[ "${failed}" -gt 0 && "${issued}" -eq 0 ]]; then
  exit 3
fi
if [[ "${failed}" -gt 0 ]]; then
  exit 2
fi
exit 0
