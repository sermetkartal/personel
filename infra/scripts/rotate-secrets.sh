#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Secret Rotation Orchestrator
# =============================================================================
# Faz 5 Madde 55 — automated rotation of static secrets per
# infra/scripts/secret-inventory.yaml.
#
# Per secret:
#   1. Decide if rotation is due (cadence + last-rotated marker)
#   2. Generate a new random secret of the requested length+charset
#   3. Update the source of truth (postgres role, vault approle, minio, kc)
#   4. Write to /etc/personel/secrets/<name>.new (chmod 600 root:root)
#   5. Run reload_cmd (compose restart of dependent service)
#   6. Run verify_cmd (must exit 0 with NEW_SECRET in env)
#   7. On success: atomic mv <name>.new -> <name>; shred old; touch marker
#   8. On failure: leave old secret in place, log CRITICAL, mark service
#      degraded, continue to next secret (other rotations should not block)
#
# Every action is journaled to /var/log/personel/secret-rotation.log AND
# (via stderr→systemd) journald, and an audit hash-chain entry is requested
# from the API for SOC 2 CC6.1 attestation.
#
# Usage:
#   sudo ./rotate-secrets.sh                # rotate all due secrets
#   sudo ./rotate-secrets.sh --check-only   # report due secrets, do not rotate
#   sudo ./rotate-secrets.sh --force        # rotate everything regardless of cadence
#   sudo ./rotate-secrets.sh --secret <name>      # rotate one only
#   sudo ./rotate-secrets.sh --certs-only         # legacy no-op for systemd unit
#
# Environment:
#   SECRETS_DIR        default /etc/personel/secrets
#   STATE_DIR          default /var/lib/personel/secret-rotation
#   LOG_FILE           default /var/log/personel/secret-rotation.log
#   INVENTORY_FILE     default <script_dir>/secret-inventory.yaml
#   COMPOSE_DIR        default <script_dir>/../compose
#   VAULT_ADDR         default https://127.0.0.1:8200
#   VAULT_TOKEN        required for vault-approle-secret-id rotations
#   AUDIT_API          default http://127.0.0.1:8000  (for SOC 2 audit emit)
#   AUDIT_TOKEN        bearer token for the audit API; if unset, audit emit
#                      is skipped with a warning (operator must record manually)
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_DIR="${COMPOSE_DIR:-${SCRIPT_DIR}/../compose}"
INVENTORY_FILE="${INVENTORY_FILE:-${SCRIPT_DIR}/secret-inventory.yaml}"
SECRETS_DIR="${SECRETS_DIR:-/etc/personel/secrets}"
STATE_DIR="${STATE_DIR:-/var/lib/personel/secret-rotation}"
LOG_FILE="${LOG_FILE:-/var/log/personel/secret-rotation.log}"
VAULT_ADDR="${VAULT_ADDR:-https://127.0.0.1:8200}"
AUDIT_API="${AUDIT_API:-http://127.0.0.1:8000}"

CHECK_ONLY=false
FORCE=false
ONLY_SECRET=""
LEGACY_CERTS_ONLY=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --check-only) CHECK_ONLY=true; shift ;;
    --force)      FORCE=true; shift ;;
    --secret)     ONLY_SECRET="${2:-}"; shift 2 ;;
    --certs-only)
      # Legacy compatibility: the existing personel-cert-renewer.service
      # invokes this script with --certs-only. Cert rotation now lives in
      # rotate-certs.sh (Faz 5 Madde 54 — sister agent). We keep this flag
      # as a no-op so the systemd unit doesn't break during the gap.
      LEGACY_CERTS_ONLY=true
      shift
      ;;
    -h|--help)
      sed -n '2,40p' "$0"
      exit 0
      ;;
    *)
      echo "unknown flag: $1" >&2
      exit 1
      ;;
  esac
done

# -----------------------------------------------------------------------------
# Logging
# -----------------------------------------------------------------------------
mkdir -p "$(dirname "${LOG_FILE}")" 2>/dev/null || true
mkdir -p "${STATE_DIR}" 2>/dev/null || true
mkdir -p "${SECRETS_DIR}" 2>/dev/null || true
chmod 700 "${SECRETS_DIR}" 2>/dev/null || true

ts() { date -u +"%Y-%m-%dT%H:%M:%SZ"; }
log()  { local m; m="[$(ts)] [rotate-secrets] $*"; printf '%s\n' "${m}" | tee -a "${LOG_FILE}" >&2; }
warn() { local m; m="[$(ts)] [rotate-secrets] WARN: $*"; printf '%s\n' "${m}" | tee -a "${LOG_FILE}" >&2; }
crit() { local m; m="[$(ts)] [rotate-secrets] CRITICAL: $*"; printf '%s\n' "${m}" | tee -a "${LOG_FILE}" >&2; }

# -----------------------------------------------------------------------------
# Legacy short-circuit
# -----------------------------------------------------------------------------
if [[ "${LEGACY_CERTS_ONLY}" == "true" ]]; then
  log "--certs-only is a legacy no-op; cert rotation lives in rotate-certs.sh now"
  exit 0
fi

# -----------------------------------------------------------------------------
# Pre-flight
# -----------------------------------------------------------------------------
if [[ ! -f "${INVENTORY_FILE}" ]]; then
  crit "inventory file not found: ${INVENTORY_FILE}"
  exit 1
fi

# Load compose env (POSTGRES creds for admin operations, VAULT_TOKEN, etc.)
if [[ -f "${COMPOSE_DIR}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${COMPOSE_DIR}/.env"
  set +a
fi

if ! command -v jq >/dev/null 2>&1; then
  crit "jq not found in PATH"; exit 1
fi
if ! command -v openssl >/dev/null 2>&1; then
  crit "openssl not found in PATH"; exit 1
fi

log "=== Personel secret rotation ==="
log "inventory : ${INVENTORY_FILE}"
log "secrets   : ${SECRETS_DIR}"
log "state     : ${STATE_DIR}"
log "log       : ${LOG_FILE}"
log "check-only: ${CHECK_ONLY}"
log "force     : ${FORCE}"
[[ -n "${ONLY_SECRET}" ]] && log "only      : ${ONLY_SECRET}"

# -----------------------------------------------------------------------------
# YAML parser — emits one record per secret to stdout in the form:
#   name|type|role|length|rotate_every_days|reload_cmd_b64|verify_cmd_b64
# Reload + verify commands can span multiple lines so we base64 them.
# -----------------------------------------------------------------------------
parse_inventory() {
  awk '
    function emit() {
      if (have) {
        cmd = "printf %s \"" reload_buf "\" | base64 -w0"
        cmd | getline reload_b64; close(cmd)
        cmd = "printf %s \"" verify_buf "\" | base64 -w0"
        cmd | getline verify_b64; close(cmd)
        printf "%s|%s|%s|%s|%s|%s|%s\n", name, type, role, length, rotate, reload_b64, verify_b64
      }
    }
    BEGIN { in_secrets=0; have=0; mode="" }
    /^secrets:/ { in_secrets=1; next }
    in_secrets==0 { next }
    /^[a-zA-Z]/ { in_secrets=0; next }
    /^  - name:/ {
      emit()
      have=1; mode=""
      name=$0; sub(/^  - name:[[:space:]]*/, "", name)
      type=""; role=""; length="0"; rotate="0"; reload_buf=""; verify_buf=""
      next
    }
    /^    type:/             { type=$0;   sub(/^    type:[[:space:]]*/, "", type);   mode=""; next }
    /^    role:/             { role=$0;   sub(/^    role:[[:space:]]*/, "", role);   mode=""; next }
    /^    length:/           { length=$0; sub(/^    length:[[:space:]]*/, "", length); mode=""; next }
    /^    rotate_every_days:/ { rotate=$0; sub(/^    rotate_every_days:[[:space:]]*/, "", rotate); mode=""; next }
    /^    reload_cmd:/ {
      v=$0; sub(/^    reload_cmd:[[:space:]]*/, "", v)
      if (v == "|") { mode="reload"; reload_buf="" }
      else { reload_buf=v; mode="" }
      next
    }
    /^    verify_cmd:/ {
      v=$0; sub(/^    verify_cmd:[[:space:]]*/, "", v)
      if (v == "|") { mode="verify"; verify_buf="" }
      else { verify_buf=v; mode="" }
      next
    }
    /^      / {
      line=$0; sub(/^      /, "", line)
      if (mode == "reload") { reload_buf = reload_buf line "\n" }
      else if (mode == "verify") { verify_buf = verify_buf line "\n" }
      next
    }
    /^[^ ]/ { mode="" }
    END { emit() }
  ' "${INVENTORY_FILE}"
}

# -----------------------------------------------------------------------------
# Helpers
# -----------------------------------------------------------------------------
gen_secret() {
  local len="$1"
  # Charset: alnum (URL-safe). Use openssl for strong entropy.
  LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c "${len}"
}

last_rotated_epoch() {
  local name="$1"
  local marker="${STATE_DIR}/${name}.last_rotated"
  if [[ -f "${marker}" ]]; then
    cat "${marker}"
  else
    echo "0"
  fi
}

is_due() {
  local name="$1" cadence_days="$2"
  [[ "${FORCE}" == "true" ]] && return 0
  local last now diff
  last=$(last_rotated_epoch "${name}")
  now=$(date +%s)
  diff=$(( (now - last) / 86400 ))
  if [[ "${last}" -eq 0 ]]; then
    return 0  # never rotated → due
  fi
  [[ "${diff}" -ge "${cadence_days}" ]]
}

mark_rotated() {
  local name="$1"
  date +%s > "${STATE_DIR}/${name}.last_rotated"
  chmod 644 "${STATE_DIR}/${name}.last_rotated"
}

emit_audit() {
  local name="$1" type="$2" outcome="$3"
  if [[ -z "${AUDIT_TOKEN:-}" ]]; then
    warn "AUDIT_TOKEN not set — SOC 2 CC6.1 audit emit skipped for ${name} (manual entry required)"
    return 0
  fi
  local payload
  payload=$(jq -nc \
    --arg name "${name}" \
    --arg type "${type}" \
    --arg outcome "${outcome}" \
    --arg ts "$(ts)" \
    '{action:"secret.rotated",resource:$name,resource_type:$type,outcome:$outcome,occurred_at:$ts}')
  if ! curl -fsS -X POST "${AUDIT_API}/v1/system/audit/emit" \
       -H "Authorization: Bearer ${AUDIT_TOKEN}" \
       -H "Content-Type: application/json" \
       -d "${payload}" >/dev/null 2>&1; then
    warn "audit emit failed for ${name} (will retry on next run via journald scrape)"
  fi
}

# -----------------------------------------------------------------------------
# Per-type updaters — each writes the new secret value to stdout.
# Returns 0 on success.
# -----------------------------------------------------------------------------
update_postgres_role() {
  local role="$1" new_secret="$2"
  # Use the postgres superuser from the loaded compose env.
  local pgpass="${POSTGRES_PASSWORD:-${POSTGRES_SUPERUSER_PASSWORD:-}}"
  if [[ -z "${pgpass}" ]]; then
    crit "POSTGRES_PASSWORD not set in compose .env"
    return 1
  fi
  # ALTER ROLE requires escaping single quotes in the password.
  local escaped="${new_secret//\'/\'\'}"
  if ! docker exec -e PGPASSWORD="${pgpass}" personel-postgres \
       psql -h 127.0.0.1 -U postgres -d personel \
       -c "ALTER ROLE \"${role}\" WITH PASSWORD '${escaped}';" >/dev/null 2>&1; then
    return 1
  fi
  return 0
}

update_vault_approle_secret_id() {
  local role="$1"
  if [[ -z "${VAULT_TOKEN:-}" ]]; then
    crit "VAULT_TOKEN not set — cannot rotate vault approle"
    return 1
  fi
  export VAULT_ADDR VAULT_TOKEN
  if ! command -v vault >/dev/null 2>&1; then
    crit "vault CLI not found"
    return 1
  fi
  local resp new_id
  if ! resp=$(vault write -format=json -f "auth/approle/role/${role}/secret-id" 2>&1); then
    crit "vault approle issuance failed: ${resp}"
    return 1
  fi
  new_id=$(jq -r '.data.secret_id' <<<"${resp}")
  if [[ -z "${new_id}" || "${new_id}" == "null" ]]; then
    crit "vault response missing secret_id"
    return 1
  fi
  printf '%s' "${new_id}"
}

update_minio_root() {
  local new_secret="$1"
  # MinIO root key rotation requires updating the MINIO_ROOT_PASSWORD env var
  # and restarting the container. Compose .env is the source of truth here;
  # we update it via sed and let reload_cmd (compose restart) pick it up.
  local env_file="${COMPOSE_DIR}/.env"
  if [[ ! -w "${env_file}" ]]; then
    crit "compose .env not writable: ${env_file}"
    return 1
  fi
  # Backup, then in-place edit
  cp "${env_file}" "${env_file}.bak.$(date +%s)"
  if grep -q '^MINIO_ROOT_PASSWORD=' "${env_file}"; then
    sed -i "s|^MINIO_ROOT_PASSWORD=.*|MINIO_ROOT_PASSWORD=${new_secret}|" "${env_file}"
  else
    printf 'MINIO_ROOT_PASSWORD=%s\n' "${new_secret}" >> "${env_file}"
  fi
  return 0
}

update_keycloak_admin() {
  local new_secret="$1"
  # Keycloak admin password rotation goes through kcadm.sh inside the container.
  local kc_old="${KC_BOOTSTRAP_ADMIN_PASSWORD:-${KEYCLOAK_ADMIN_PASSWORD:-admin123}}"
  if ! docker exec personel-keycloak /opt/keycloak/bin/kcadm.sh config credentials \
       --server http://127.0.0.1:8080 --realm master \
       --user admin --password "${kc_old}" >/dev/null 2>&1; then
    crit "kcadm login with old admin password failed"
    return 1
  fi
  if ! docker exec personel-keycloak /opt/keycloak/bin/kcadm.sh set-password \
       -r master --username admin --new-password "${new_secret}" >/dev/null 2>&1; then
    crit "kcadm set-password failed"
    return 1
  fi
  # Update env file so future restarts use it
  local env_file="${COMPOSE_DIR}/.env"
  if [[ -w "${env_file}" ]]; then
    cp "${env_file}" "${env_file}.bak.$(date +%s)"
    if grep -q '^KC_BOOTSTRAP_ADMIN_PASSWORD=' "${env_file}"; then
      sed -i "s|^KC_BOOTSTRAP_ADMIN_PASSWORD=.*|KC_BOOTSTRAP_ADMIN_PASSWORD=${new_secret}|" "${env_file}"
    else
      printf 'KC_BOOTSTRAP_ADMIN_PASSWORD=%s\n' "${new_secret}" >> "${env_file}"
    fi
  fi
  return 0
}

# -----------------------------------------------------------------------------
# Atomic write helper
# -----------------------------------------------------------------------------
write_new_secret_file() {
  local name="$1" value="$2"
  local target="${SECRETS_DIR}/${name}.new"
  umask 077
  printf '%s' "${value}" > "${target}"
  chmod 600 "${target}"
}

shred_file() {
  local f="$1"
  if command -v shred >/dev/null 2>&1; then
    shred -u "${f}" 2>/dev/null || rm -f "${f}"
  else
    rm -f "${f}"
  fi
}

# -----------------------------------------------------------------------------
# rotate_one: orchestrate a single secret rotation
# -----------------------------------------------------------------------------
rotate_one() {
  local name="$1" type="$2" role="$3" length="$4" reload_cmd="$5" verify_cmd="$6"

  log "  rotating ${name} (type=${type} role=${role:-n/a})"

  # 1. Generate new secret value
  local new_secret=""
  case "${type}" in
    postgres-role|minio-root|keycloak-admin)
      new_secret=$(gen_secret "${length}")
      ;;
    vault-approle-secret-id)
      # Vault returns its own secret_id; we don't generate
      :
      ;;
    *)
      crit "unknown secret type: ${type}"
      return 1
      ;;
  esac

  # 2. Update source of truth
  case "${type}" in
    postgres-role)
      if ! update_postgres_role "${role}" "${new_secret}"; then
        crit "  postgres ALTER ROLE failed for ${role}"
        return 1
      fi
      ;;
    vault-approle-secret-id)
      if ! new_secret=$(update_vault_approle_secret_id "${role}"); then
        crit "  vault approle rotation failed for ${role}"
        return 1
      fi
      ;;
    minio-root)
      if ! update_minio_root "${new_secret}"; then
        crit "  minio root update failed"
        return 1
      fi
      ;;
    keycloak-admin)
      if ! update_keycloak_admin "${new_secret}"; then
        crit "  keycloak admin update failed"
        return 1
      fi
      ;;
  esac

  # 3. Stage the new secret file
  write_new_secret_file "${name}" "${new_secret}"

  # 4. Reload dependents
  if [[ -n "${reload_cmd}" ]]; then
    log "  reloading: ${reload_cmd}"
    if ! ( cd "${COMPOSE_DIR}" && bash -c "${reload_cmd}" ) >>"${LOG_FILE}" 2>&1; then
      crit "  reload_cmd failed for ${name}; leaving old secret in place"
      shred_file "${SECRETS_DIR}/${name}.new"
      emit_audit "${name}" "${type}" "reload_failed"
      return 1
    fi
    # Give the service a moment to come up
    sleep 3
  fi

  # 5. Verify
  log "  verifying"
  if ! NEW_SECRET="${new_secret}" bash -c "${verify_cmd}" >>"${LOG_FILE}" 2>&1; then
    crit "  VERIFY FAILED for ${name} — old secret REMAINS in place"
    shred_file "${SECRETS_DIR}/${name}.new"
    emit_audit "${name}" "${type}" "verify_failed"
    return 1
  fi

  # 6. Atomic swap
  local target="${SECRETS_DIR}/${name}"
  if [[ -f "${target}" ]]; then
    cp "${target}" "${target}.old"
    chmod 600 "${target}.old"
  fi
  mv -f "${SECRETS_DIR}/${name}.new" "${target}"
  chmod 600 "${target}"
  if [[ -f "${target}.old" ]]; then
    shred_file "${target}.old"
  fi

  mark_rotated "${name}"
  emit_audit "${name}" "${type}" "success"
  log "  OK ${name}"
  return 0
}

# -----------------------------------------------------------------------------
# Main loop
# -----------------------------------------------------------------------------
total=0
due=0
rotated=0
skipped=0
failed=0
failed_names=()

while IFS='|' read -r name type role length cadence reload_b64 verify_b64; do
  [[ -z "${name}" ]] && continue
  total=$((total+1))

  if [[ -n "${ONLY_SECRET}" && "${name}" != "${ONLY_SECRET}" ]]; then
    continue
  fi

  if ! is_due "${name}" "${cadence}"; then
    log "  skip ${name} — not due (cadence ${cadence}d)"
    skipped=$((skipped+1))
    continue
  fi
  due=$((due+1))

  if [[ "${CHECK_ONLY}" == "true" ]]; then
    log "  DUE ${name} (type=${type} cadence=${cadence}d)"
    continue
  fi

  reload_cmd=$(printf '%s' "${reload_b64}" | base64 -d 2>/dev/null || true)
  verify_cmd=$(printf '%s' "${verify_b64}" | base64 -d 2>/dev/null || true)

  set +e
  rotate_one "${name}" "${type}" "${role}" "${length}" "${reload_cmd}" "${verify_cmd}"
  rc=$?
  set -e

  if [[ "${rc}" -eq 0 ]]; then
    rotated=$((rotated+1))
  else
    failed=$((failed+1))
    failed_names+=("${name}")
  fi
done < <(parse_inventory)

log ""
log "=== Summary ==="
log "total in inventory: ${total}"
log "due now           : ${due}"
log "rotated           : ${rotated}"
log "skipped (not due) : ${skipped}"
log "failed            : ${failed}"
if [[ "${failed}" -gt 0 ]]; then
  log "failed secrets    : ${failed_names[*]}"
  exit 2
fi
exit 0
