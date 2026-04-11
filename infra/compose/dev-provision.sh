#!/usr/bin/env bash
# =============================================================================
# dev-provision.sh — one-shot runtime provisioning for the dev stack.
#
# Runs after `docker compose ... up -d` brings the containers up. Idempotent:
# every step tolerates "already exists" responses so you can re-run it.
#
# What it does:
#   1. Enables Vault transit engine + creates control-plane-signing ed25519
#      key + provisions an AppRole for the API
#   2. Creates the audit-worm MinIO bucket with Object Lock (Compliance
#      mode, 5-year retention) plus the four app buckets
#   3. In Keycloak: creates the console OIDC client with audience mapper
#      for personel-admin-api and a hardcoded tenant_id claim, plus
#      the realm roles and a dpo-test user
#   4. Inserts the dev seed tenant row into public.tenants (not core.tenants
#      which is the dead init.sql definition)
#
# Usage:
#   cd infra/compose
#   docker compose -f docker-compose.yaml -f docker-compose.dev.yaml up -d
#   ./dev-provision.sh
#
# After success, a working DPO token can be obtained with:
#   curl -X POST http://localhost:8080/realms/personel/protocol/openid-connect/token \
#     -d client_id=console \
#     -d client_secret=dev-console-client-secret \
#     -d username=dpo-test \
#     -d password=dpo-test-pass \
#     -d grant_type=password
# =============================================================================
set -euo pipefail

cd "$(dirname "$0")"

green() { printf '\033[32m%s\033[0m\n' "$*"; }
yellow() { printf '\033[33m%s\033[0m\n' "$*"; }
red()   { printf '\033[31m%s\033[0m\n' "$*"; }

green "==> 1/4 Vault transit + AppRole"
docker exec -e VAULT_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN=dev-root-token personel-vault sh -c '
  vault secrets enable transit 2>/dev/null || true
  vault write -f transit/keys/control-plane-signing type=ed25519 2>/dev/null || true
  vault auth enable approle 2>/dev/null || true
  vault policy write api-dev - <<EOF
path "transit/sign/control-plane-signing"   { capabilities = ["update"] }
path "transit/verify/control-plane-signing" { capabilities = ["update"] }
path "transit/keys/control-plane-signing"   { capabilities = ["read"] }
EOF
  vault write auth/approle/role/api-dev token_policies=api-dev token_ttl=1h token_max_ttl=4h >/dev/null
  ROLE_ID=$(vault read -field=role_id auth/approle/role/api-dev/role-id)
  SECRET_ID=$(vault write -field=secret_id -f auth/approle/role/api-dev/secret-id)
  echo "ROLE_ID=$ROLE_ID"
  echo "SECRET_ID=$SECRET_ID"
' > /tmp/personel-vault-approle.env
cat /tmp/personel-vault-approle.env
yellow "(Copy ROLE_ID / SECRET_ID into api/api.dev.yaml vault.app_role_id / app_role_secret_id if they changed.)"

green "==> 2/4 MinIO buckets (audit-worm with Object Lock)"
docker run --rm --network personel_data --entrypoint sh minio/mc:latest -c "
  mc alias set local http://minio:9000 minioadmin minioadmin-dev-only >/dev/null
  mc mb --ignore-existing local/personel-screenshots local/personel-dsr local/personel-destruction >/dev/null
  if ! mc ls local/audit-worm >/dev/null 2>&1; then
    mc mb --with-lock local/audit-worm
    mc retention set --default compliance 5y local/audit-worm
  else
    echo 'audit-worm already exists (lock state preserved)'
  fi
  mc ls local/
"

green "==> 3/4 Keycloak realm, client, roles, user"
TOKEN=$(curl -sS -X POST http://localhost:8080/realms/master/protocol/openid-connect/token \
  -d "client_id=admin-cli" -d "username=admin" -d "password=admin-dev-only" -d "grant_type=password" \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['access_token'])")

# realm (201 or 409 Conflict both fine)
curl -sS -o /dev/null -w "realm: %{http_code}\n" -X POST http://localhost:8080/admin/realms \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"realm":"personel","enabled":true}'

# console client
CLIENT_STATUS=$(curl -sS -o /dev/null -w "%{http_code}" \
  "http://localhost:8080/admin/realms/personel/clients?clientId=console" \
  -H "Authorization: Bearer $TOKEN")
CLIENT_LIST=$(curl -sS "http://localhost:8080/admin/realms/personel/clients?clientId=console" \
  -H "Authorization: Bearer $TOKEN")
if [[ "$CLIENT_LIST" == "[]" ]]; then
  curl -sS -o /dev/null -w "client create: %{http_code}\n" -X POST \
    "http://localhost:8080/admin/realms/personel/clients" \
    -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
    -d '{
      "clientId": "console",
      "enabled": true,
      "publicClient": true,
      "standardFlowEnabled": true,
      "directAccessGrantsEnabled": true,
      "protocol": "openid-connect",
      "redirectUris": ["http://localhost:13000/*"],
      "webOrigins": ["http://localhost:13000", "+"],
      "attributes": {"pkce.code.challenge.method": "S256"}
    }'
else
  # Idempotent update: force publicClient + PKCE on every run.
  EXISTING_ID=$(echo "$CLIENT_LIST" | python3 -c "import sys,json;print(json.load(sys.stdin)[0]['id'])")
  curl -sS -o /dev/null -X PUT \
    "http://localhost:8080/admin/realms/personel/clients/$EXISTING_ID" \
    -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
    -d '{
      "clientId": "console",
      "enabled": true,
      "publicClient": true,
      "standardFlowEnabled": true,
      "directAccessGrantsEnabled": true,
      "protocol": "openid-connect",
      "redirectUris": ["http://localhost:13000/*"],
      "webOrigins": ["http://localhost:13000", "+"],
      "attributes": {"pkce.code.challenge.method": "S256"}
    }'
  echo "console client updated (public + PKCE)"
fi

CLIENT_ID=$(curl -sS "http://localhost:8080/admin/realms/personel/clients?clientId=console" \
  -H "Authorization: Bearer $TOKEN" \
  | python3 -c "import sys,json;d=json.load(sys.stdin);print(d[0]['id']) if d else ''")

# Audience + tenant_id mappers (create silently; ignore 409)
for MAPPER in \
  '{"name":"personel-admin-api-audience","protocol":"openid-connect","protocolMapper":"oidc-audience-mapper","config":{"included.custom.audience":"personel-admin-api","id.token.claim":"false","access.token.claim":"true"}}' \
  '{"name":"tenant-id-hardcoded","protocol":"openid-connect","protocolMapper":"oidc-hardcoded-claim-mapper","config":{"claim.name":"tenant_id","claim.value":"00000000-0000-0000-0000-000000000001","jsonType.label":"String","id.token.claim":"true","access.token.claim":"true","userinfo.token.claim":"true"}}'; do
  curl -sS -o /dev/null -X POST \
    "http://localhost:8080/admin/realms/personel/clients/$CLIENT_ID/protocol-mappers/models" \
    -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
    -d "$MAPPER" || true
done
echo "mappers: ensured"

# Realm roles
for ROLE in dpo admin auditor hr investigator manager employee; do
  curl -sS -o /dev/null -X POST "http://localhost:8080/admin/realms/personel/roles" \
    -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
    -d "{\"name\":\"$ROLE\"}" || true
done
echo "roles: ensured"

# dpo-test user
USER_LIST=$(curl -sS "http://localhost:8080/admin/realms/personel/users?username=dpo-test" \
  -H "Authorization: Bearer $TOKEN")
if [[ "$USER_LIST" == "[]" ]]; then
  curl -sS -o /dev/null -w "user create: %{http_code}\n" -X POST \
    "http://localhost:8080/admin/realms/personel/users" \
    -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
    -d '{
      "username":"dpo-test","email":"dpo-test@personel.local","emailVerified":true,"enabled":true,
      "firstName":"DPO","lastName":"Test",
      "credentials":[{"type":"password","value":"dpo-test-pass","temporary":false}]
    }'
else
  echo "dpo-test user already exists"
fi

USER_ID=$(curl -sS "http://localhost:8080/admin/realms/personel/users?username=dpo-test" \
  -H "Authorization: Bearer $TOKEN" \
  | python3 -c "import sys,json;print(json.load(sys.stdin)[0]['id'])")

# assign dpo role
DPO_ROLE=$(curl -sS "http://localhost:8080/admin/realms/personel/roles/dpo" -H "Authorization: Bearer $TOKEN")
curl -sS -o /dev/null -X POST \
  "http://localhost:8080/admin/realms/personel/users/$USER_ID/role-mappings/realm" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d "[$DPO_ROLE]"
echo "dpo role assigned to dpo-test"

green "==> 4/4 Postgres seed tenant (public.tenants)"
docker exec personel-postgres psql -U postgres -d personel -c "
INSERT INTO public.tenants(id, name, slug)
VALUES ('00000000-0000-0000-0000-000000000001', 'Dev Tenant', 'dev')
ON CONFLICT (id) DO NOTHING;
SELECT COUNT(*) AS tenant_rows FROM public.tenants;
"

green "==> Dev stack provisioned."
echo
yellow "Get a DPO token:"
cat <<'EOF'
  TOKEN=$(curl -sS -X POST "http://localhost:8080/realms/personel/protocol/openid-connect/token" \
    -d "client_id=console" \
    -d "client_secret=dev-console-client-secret" \
    -d "username=dpo-test" \
    -d "password=dpo-test-pass" \
    -d "grant_type=password" \
    | python3 -c "import sys,json;print(json.load(sys.stdin)['access_token'])")

  curl -sS "http://localhost:8001/v1/system/evidence-coverage?period=2026-04" \
    -H "Authorization: Bearer $TOKEN" | python3 -m json.tool
EOF
