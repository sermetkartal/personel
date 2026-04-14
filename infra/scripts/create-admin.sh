#!/usr/bin/env bash
# =============================================================================
# Personel Platform — First Admin and DPO User Creation
# TR: İlk DPO ve yönetici kullanıcılarını Keycloak'ta oluşturur.
# EN: Creates first DPO and admin users in Keycloak.
# =============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_DIR="${SCRIPT_DIR}/../compose"
set -a; source "${COMPOSE_DIR}/.env"; set +a

KEYCLOAK_URL="http://localhost:${KEYCLOAK_PORT:-8080}"
REALM="${KEYCLOAK_REALM:-personel}"

log() { echo "[create-admin] $*"; }
die() { echo "[create-admin] ERROR: $*" >&2; exit 1; }

log "=== Creating First Admin and DPO Users ==="
echo ""

# Get Keycloak admin token
TOKEN=$(curl -sf -X POST \
  "${KEYCLOAK_URL}/realms/master/protocol/openid-connect/token" \
  -d "grant_type=password" \
  -d "client_id=admin-cli" \
  -d "username=${KEYCLOAK_ADMIN_USER:-admin}" \
  -d "password=${KEYCLOAK_ADMIN_PASSWORD}" \
  | python3 -c "import json,sys; print(json.load(sys.stdin)['access_token'])")

[[ -n "${TOKEN}" ]] || die "Failed to get Keycloak admin token"
log "Keycloak admin token obtained"

# Create user function
create_user() {
  local username="$1" email="$2" firstname="$3" lastname="$4" role="$5" password="$6"

  log "Creating user: ${username} (${role})"

  # Create user
  local user_id
  user_id=$(curl -sf -X POST \
    "${KEYCLOAK_URL}/admin/realms/${REALM}/users" \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{
      \"username\": \"${username}\",
      \"email\": \"${email}\",
      \"firstName\": \"${firstname}\",
      \"lastName\": \"${lastname}\",
      \"enabled\": true,
      \"emailVerified\": true,
      \"credentials\": [{\"type\": \"password\", \"value\": \"${password}\", \"temporary\": true}]
    }" \
    -o /dev/null -w "%header{Location}" 2>/dev/null | \
    grep -oP '[^/]+$' || echo "")

  if [[ -z "${user_id}" ]]; then
    # User may already exist
    user_id=$(curl -sf \
      "${KEYCLOAK_URL}/admin/realms/${REALM}/users?username=${username}" \
      -H "Authorization: Bearer ${TOKEN}" \
      | python3 -c "import json,sys; users=json.load(sys.stdin); print(users[0]['id'] if users else '')" \
      2>/dev/null || echo "")
  fi

  [[ -n "${user_id}" ]] || { log "  WARN: Could not get user ID for ${username}"; return; }

  # Get role ID
  local role_id
  role_id=$(curl -sf \
    "${KEYCLOAK_URL}/admin/realms/${REALM}/roles/${role}" \
    -H "Authorization: Bearer ${TOKEN}" \
    | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])" 2>/dev/null || echo "")

  if [[ -n "${role_id}" ]]; then
    curl -sf -X POST \
      "${KEYCLOAK_URL}/admin/realms/${REALM}/users/${user_id}/role-mappings/realm" \
      -H "Authorization: Bearer ${TOKEN}" \
      -H "Content-Type: application/json" \
      -d "[{\"id\": \"${role_id}\", \"name\": \"${role}\"}]" \
      >/dev/null
    log "  Role '${role}' assigned to ${username}"
  fi

  log "  User ${username} created (ID: ${user_id})"
  log "  TEMPORARY password set — user must change on first login"
}

# Prompt for user details
read -r -p "Admin email: " ADMIN_EMAIL
read -r -p "Admin first name: " ADMIN_FIRST
read -r -p "Admin last name: " ADMIN_LAST
read -r -s -p "Admin temporary password: " ADMIN_PASS; echo

read -r -p "DPO email: " DPO_EMAIL
read -r -p "DPO first name: " DPO_FIRST
read -r -p "DPO last name: " DPO_LAST
read -r -s -p "DPO temporary password: " DPO_PASS; echo

create_user "admin1" "${ADMIN_EMAIL}" "${ADMIN_FIRST}" "${ADMIN_LAST}" "admin" "${ADMIN_PASS}"
create_user "dpo1" "${DPO_EMAIL}" "${DPO_FIRST}" "${DPO_LAST}" "dpo" "${DPO_PASS}"

echo ""
log "Users created successfully."
echo ""
echo "TR: Aşağıdaki URL'ler ile giriş yapabilirsiniz:"
echo "EN: You can log in at the following URLs:"
echo ""
echo "  Yönetici Konsolu / Admin Console:"
echo "    https://${PERSONEL_EXTERNAL_HOST:-localhost}/console"
echo "  Keycloak Admin:"
echo "    https://${PERSONEL_EXTERNAL_HOST:-localhost}/auth/admin/"
echo ""
echo "TR: İlk girişte geçici şifrenizi değiştirmeniz istenecektir."
echo "EN: You will be prompted to change the temporary password on first login."
