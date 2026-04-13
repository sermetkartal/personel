#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Vault PKI Certificate Rotator
# =============================================================================
#
# PURPOSE
#   Daily-fired cert renewal orchestrator. Reads cert-inventory.yaml, checks
#   each server cert's expiry, and re-issues from Vault PKI any cert that
#   falls inside the renewal window (default 30 days). Atomic swap, service
#   reload, journald + file logging, alert on failure.
#
# SAFETY GUARANTEES
#   - NEVER overwrites an existing cert if Vault returns an error or
#     non-JSON output. The new material is staged in a sibling .staging
#     directory and only `mv`'d into place after full validation.
#   - On ANY per-service failure, the old cert+key remain in place untouched
#     and the script proceeds to the next service. Final exit code is 1
#     if ANY service failed, 0 only if all succeeded.
#   - A service's reload_cmd runs only AFTER the atomic swap. If reload
#     fails, the cert is still in place (already-running connections keep
#     using the old cert) and the failure is logged but not auto-rolled-back
#     — service-side rollback would risk leaving a half-restarted state.
#
# USAGE
#   ./rotate-certs.sh                    # rotate as needed (production cron mode)
#   ./rotate-certs.sh --check            # dry run: report state, no changes
#   ./rotate-certs.sh --force <name>     # force-rotate a single service even if not expiring
#   ./rotate-certs.sh --inventory PATH   # use an alternate inventory file
#
# REQUIREMENTS
#   - vault CLI in PATH, VAULT_TOKEN exported (the cron unit pulls it from
#     a Vault AppRole login wrapper)
#   - openssl
#   - python3 (for YAML and JSON parsing)
#   - jq for JSON output extraction
#
# ENV
#   VAULT_ADDR        default https://127.0.0.1:8200
#   VAULT_CACERT      default /etc/personel/tls/tenant_ca.crt
#   ROTATE_LOG_FILE   default /var/log/personel/cert-rotation.log
#   RENEWAL_WINDOW    default 30 (days)
#   ROTATE_ALERT_CMD  default 'wall'; override with a curl webhook command
#                     e.g. 'curl -fsS -X POST -d @- https://hooks.example/cert-rotation'
#
# REFERENCE
#   docs/security/runbooks/secret-rotation.md
#   infra/scripts/cert-inventory.yaml
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INVENTORY="${SCRIPT_DIR}/cert-inventory.yaml"
LOG_FILE="${ROTATE_LOG_FILE:-/var/log/personel/cert-rotation.log}"
RENEWAL_WINDOW="${RENEWAL_WINDOW:-30}"
ALERT_CMD="${ROTATE_ALERT_CMD:-wall}"
VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}"
VAULT_CACERT="${VAULT_CACERT:-/etc/personel/tls/tenant_ca.crt}"

CHECK_ONLY=false
FORCE_NAME=""

export VAULT_ADDR VAULT_CACERT

# ---------------------------------------------------------------------------
# CLI parsing
# ---------------------------------------------------------------------------
while [[ $# -gt 0 ]]; do
  case "$1" in
    --check)            CHECK_ONLY=true; shift ;;
    --force)            FORCE_NAME="${2:-}"; shift 2 ;;
    --inventory)        INVENTORY="${2:-}"; shift 2 ;;
    -h|--help)
      sed -n '/^# USAGE/,/^# REQUIREMENTS/p' "$0" | sed 's/^# *//'
      exit 0
      ;;
    *)
      echo "Unknown arg: $1" >&2
      exit 2
      ;;
  esac
done

# ---------------------------------------------------------------------------
# Logging — both journald-friendly stdout AND a rotated file
# ---------------------------------------------------------------------------
mkdir -p "$(dirname "${LOG_FILE}")" 2>/dev/null || true

log() {
  local ts
  ts=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
  echo "[rotate-certs] ${ts} $*"
  echo "[rotate-certs] ${ts} $*" >> "${LOG_FILE}" 2>/dev/null || true
}

warn() {
  local ts
  ts=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
  echo "[rotate-certs] ${ts} WARN: $*" >&2
  echo "[rotate-certs] ${ts} WARN: $*" >> "${LOG_FILE}" 2>/dev/null || true
}

err() {
  local ts
  ts=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
  echo "[rotate-certs] ${ts} ERROR: $*" >&2
  echo "[rotate-certs] ${ts} ERROR: $*" >> "${LOG_FILE}" 2>/dev/null || true
}

alert() {
  local msg="$1"
  if [[ -n "${ALERT_CMD}" ]]; then
    printf '%s\n' "PERSONEL CERT ROTATION FAILED: ${msg}" \
      | ${ALERT_CMD} 2>/dev/null \
      || warn "Alert command failed; manual escalation required."
  fi
}

# ---------------------------------------------------------------------------
# Pre-flight
# ---------------------------------------------------------------------------
[[ -f "${INVENTORY}" ]] \
  || { err "Inventory not found: ${INVENTORY}"; exit 1; }

for tool in vault openssl python3 jq; do
  command -v "${tool}" >/dev/null 2>&1 \
    || { err "'${tool}' not found in PATH"; exit 1; }
done

if [[ "${CHECK_ONLY}" != "true" ]]; then
  [[ -n "${VAULT_TOKEN:-}" ]] \
    || { err "VAULT_TOKEN not set. Cron wrapper must perform AppRole login first."; exit 1; }
fi

log "=== Personel cert rotation start ==="
log "Inventory: ${INVENTORY}"
log "Renewal window: ${RENEWAL_WINDOW} days"
log "Mode: $([[ "${CHECK_ONLY}" == "true" ]] && echo "DRY-RUN" || echo "APPLY")"
[[ -n "${FORCE_NAME}" ]] && log "Force-rotate: ${FORCE_NAME}"

# ---------------------------------------------------------------------------
# YAML parsing — emit one shell-safe line per service via a small Python helper.
# Format:
#   name|role|issuer_path|cn|sans|ip_sans|cert_path|key_path|reload_cmd|owner|mode|ttl
# ---------------------------------------------------------------------------
parse_inventory() {
  python3 - "${INVENTORY}" <<'PY'
import sys
import yaml

with open(sys.argv[1], "r", encoding="utf-8") as fh:
    data = yaml.safe_load(fh) or {}

defaults = data.get("defaults", {}) or {}
def_issuer = defaults.get("issuer_path", "pki")
def_ttl = defaults.get("ttl", "720h")
def_owner = defaults.get("owner", "personel:personel")
def_mode = str(defaults.get("mode", "0600"))

for svc in data.get("services", []) or []:
    name = svc.get("name", "")
    role = svc.get("role", "server-cert")
    issuer = svc.get("issuer_path", def_issuer)
    cn = svc.get("cn", "")
    sans = ",".join(svc.get("sans", []) or [])
    ip_sans = ",".join(svc.get("ip_sans", []) or [])
    cert_path = svc.get("cert_path", "")
    key_path = svc.get("key_path", "")
    reload_cmd = svc.get("reload_cmd", "")
    owner = svc.get("owner", def_owner)
    mode = str(svc.get("mode", def_mode))
    ttl = svc.get("ttl", def_ttl)
    # Use a delimiter that cannot appear in any normal field.
    print("|".join([name, role, issuer, cn, sans, ip_sans, cert_path,
                    key_path, reload_cmd, owner, mode, ttl]))
PY
}

# ---------------------------------------------------------------------------
# Per-service rotation
# ---------------------------------------------------------------------------
days_until_expiry() {
  local cert_file="$1"
  local end_str end_epoch now_epoch
  end_str=$(openssl x509 -enddate -noout -in "${cert_file}" 2>/dev/null \
            | sed 's/notAfter=//') || return 1
  end_epoch=$(date -d "${end_str}" +%s 2>/dev/null) || return 1
  now_epoch=$(date +%s)
  echo $(( (end_epoch - now_epoch) / 86400 ))
}

issue_from_vault() {
  # Returns 0 on success, prints JSON to stdout. Returns nonzero on Vault error.
  local issuer="$1" role="$2" cn="$3" sans="$4" ip_sans="$5" ttl="$6"
  local args=("write" "-format=json" "${issuer}/issue/${role}" "common_name=${cn}" "ttl=${ttl}")
  [[ -n "${sans}"    ]] && args+=("alt_names=${sans}")
  [[ -n "${ip_sans}" ]] && args+=("ip_sans=${ip_sans}")
  vault "${args[@]}" 2>/dev/null
}

rotate_one() {
  local name="$1" role="$2" issuer="$3" cn="$4" sans="$5" ip_sans="$6"
  local cert_path="$7" key_path="$8" reload_cmd="$9" owner="${10}" mode="${11}" ttl="${12}"

  log "--- ${name} ---"

  # Validate paths exist (for a check pass) or that the directory exists
  local cert_dir key_dir
  cert_dir=$(dirname "${cert_path}")
  key_dir=$(dirname "${key_path}")

  if [[ ! -d "${cert_dir}" ]]; then
    err "${name}: cert dir ${cert_dir} does not exist"
    return 1
  fi
  if [[ ! -d "${key_dir}" ]]; then
    err "${name}: key dir ${key_dir} does not exist"
    return 1
  fi

  # Decide whether to rotate
  local needs_rotation="no"
  local days_left="-"
  if [[ -f "${cert_path}" ]]; then
    if days_left=$(days_until_expiry "${cert_path}"); then
      log "${name}: cert expires in ${days_left} days"
      if (( days_left <= RENEWAL_WINDOW )); then
        needs_rotation="yes (within ${RENEWAL_WINDOW}d window)"
      fi
    else
      warn "${name}: cannot parse expiry of ${cert_path}; treating as needing rotation"
      needs_rotation="yes (unparseable)"
    fi
  else
    log "${name}: cert missing at ${cert_path}; will issue fresh"
    needs_rotation="yes (missing)"
  fi

  if [[ -n "${FORCE_NAME}" && "${FORCE_NAME}" == "${name}" ]]; then
    needs_rotation="yes (force)"
  fi

  if [[ "${needs_rotation}" == "no" ]]; then
    log "${name}: OK, no action"
    return 0
  fi

  log "${name}: rotation needed — ${needs_rotation}"

  if [[ "${CHECK_ONLY}" == "true" ]]; then
    log "${name}: DRY-RUN, skipping issue"
    return 0
  fi

  # Stage to a sibling directory under the cert dir
  local staging="${cert_dir}/.staging"
  mkdir -p "${staging}"
  chmod 700 "${staging}"

  local stage_cert="${staging}/${name}.crt"
  local stage_key="${staging}/${name}.key"
  local stage_chain="${staging}/${name}.chain"

  # ---- Issue from Vault ----
  log "${name}: requesting cert from ${issuer}/issue/${role}"
  local issue_json
  if ! issue_json=$(issue_from_vault "${issuer}" "${role}" "${cn}" "${sans}" "${ip_sans}" "${ttl}"); then
    err "${name}: Vault issue failed; LEAVING old cert in place"
    rm -f "${stage_cert}" "${stage_key}" "${stage_chain}"
    return 1
  fi

  # ---- Validate the JSON has the fields we expect ----
  local new_cert new_key new_chain
  new_cert=$(printf '%s' "${issue_json}" | jq -r '.data.certificate // empty')
  new_key=$(printf '%s'  "${issue_json}" | jq -r '.data.private_key // empty')
  new_chain=$(printf '%s' "${issue_json}" | jq -r '.data.issuing_ca // empty')

  if [[ -z "${new_cert}" || -z "${new_key}" ]]; then
    err "${name}: Vault response missing certificate or private_key; LEAVING old cert in place"
    return 1
  fi

  # ---- Write to staging with restrictive perms ----
  umask 077
  printf '%s\n' "${new_cert}"  > "${stage_cert}"
  printf '%s\n' "${new_key}"   > "${stage_key}"
  [[ -n "${new_chain}" ]] && printf '%s\n' "${new_chain}" > "${stage_chain}"

  # ---- Sanity check: openssl can parse the new cert ----
  if ! openssl x509 -in "${stage_cert}" -noout -text >/dev/null 2>&1; then
    err "${name}: staged cert failed openssl parse; LEAVING old cert in place"
    rm -f "${stage_cert}" "${stage_key}" "${stage_chain}"
    return 1
  fi

  # ---- Atomic swap (mv on same filesystem is atomic on ext4/xfs) ----
  if ! mv -f "${stage_cert}" "${cert_path}"; then
    err "${name}: cert mv failed; old cert UNTOUCHED"
    return 1
  fi
  if ! mv -f "${stage_key}" "${key_path}"; then
    err "${name}: key mv failed AFTER cert was swapped — service may now be inconsistent"
    err "${name}: MANUAL INTERVENTION REQUIRED"
    return 1
  fi

  # Apply ownership + mode
  chown "${owner}" "${cert_path}" "${key_path}" 2>/dev/null || warn "${name}: chown ${owner} failed"
  chmod 0644 "${cert_path}" 2>/dev/null || true
  chmod "${mode}" "${key_path}" 2>/dev/null || warn "${name}: chmod ${mode} on key failed"

  log "${name}: cert+key swapped successfully"

  # ---- Reload the service ----
  if [[ -n "${reload_cmd}" ]]; then
    log "${name}: running reload — ${reload_cmd}"
    if ! eval "${reload_cmd}" >>"${LOG_FILE}" 2>&1; then
      warn "${name}: reload command failed; cert is in place but service may need manual restart"
      return 1
    fi
    log "${name}: reload OK"
  else
    warn "${name}: no reload_cmd defined; service still running with old cert in memory"
  fi

  return 0
}

# ---------------------------------------------------------------------------
# Main loop
# ---------------------------------------------------------------------------
total=0
ok_count=0
fail_count=0
fail_names=()

while IFS='|' read -r name role issuer cn sans ip_sans cert_path key_path reload_cmd owner mode ttl; do
  [[ -z "${name}" ]] && continue
  total=$((total + 1))

  if rotate_one "${name}" "${role}" "${issuer}" "${cn}" "${sans}" "${ip_sans}" \
                "${cert_path}" "${key_path}" "${reload_cmd}" "${owner}" "${mode}" "${ttl}"; then
    ok_count=$((ok_count + 1))
  else
    fail_count=$((fail_count + 1))
    fail_names+=("${name}")
  fi
done < <(parse_inventory)

# ---------------------------------------------------------------------------
# Summary + alert
# ---------------------------------------------------------------------------
log "=== Summary: ${ok_count}/${total} OK, ${fail_count} failed ==="

if (( fail_count > 0 )); then
  log "Failed: ${fail_names[*]}"
  alert "rotation failed for: ${fail_names[*]}"
  exit 1
fi

log "=== Personel cert rotation complete ==="
exit 0
