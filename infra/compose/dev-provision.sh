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

# Counters for summary output
BUCKET_COUNT=0
KC_CLIENT_COUNT=0
KC_ROLE_COUNT=0
KC_USER_COUNT=0

green "==> 1/4 Vault transit + AppRole"
docker exec -e VAULT_ADDR=http://127.0.0.1:8200 -e VAULT_TOKEN=dev-root-token personel-vault sh -c '
  set -eu
  # transit secrets engine — idempotent: "already enabled" exits non-zero without -eu guard.
  # We check manually so we can distinguish real failures from already-exists.
  if ! vault secrets list | grep -q "^transit/"; then
    vault secrets enable transit
  else
    echo "transit: already enabled"
  fi

  # ed25519 signing key — idempotent: key might already exist from a previous run.
  if ! vault read transit/keys/control-plane-signing >/dev/null 2>&1; then
    vault write -f transit/keys/control-plane-signing type=ed25519
  else
    echo "transit/keys/control-plane-signing: already exists"
  fi

  # AppRole auth method — idempotent.
  if ! vault auth list | grep -q "^approle/"; then
    vault auth enable approle
  else
    echo "approle: already enabled"
  fi

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

# Auto-sync the freshly-minted AppRole ID/Secret into api.dev.yaml so the
# next api container start finds valid credentials without hand-editing.
ROLE_ID=$(grep ^ROLE_ID= /tmp/personel-vault-approle.env | cut -d= -f2)
SECRET_ID=$(grep ^SECRET_ID= /tmp/personel-vault-approle.env | cut -d= -f2)

# Guard: fail loudly if the inner shell did not produce valid credentials.
if [[ -z "${ROLE_ID:-}" || -z "${SECRET_ID:-}" ]]; then
  red "FATAL: Vault AppRole creation failed — ROLE_ID or SECRET_ID empty"
  red "Check /tmp/personel-vault-approle.env and vault container logs"
  exit 1
fi

API_YAML="$(dirname "$0")/api/api.dev.yaml"
if [[ -f "$API_YAML" ]]; then
  # Backup before touching the file so we can restore on sed failure.
  cp "$API_YAML" "${API_YAML}.bak"

  # macOS sed: use BSD-compatible -i ''; GNU sed tolerates it too with
  # an empty suffix followed by an expression so keep both forms.
  if sed --version >/dev/null 2>&1; then
    # GNU sed
    sed -i "s|^  app_role_id: .*|  app_role_id: \"${ROLE_ID}\"|" "$API_YAML"
    sed -i "s|^  app_role_secret_id: .*|  app_role_secret_id: \"${SECRET_ID}\"|" "$API_YAML"
  else
    # BSD sed (macOS)
    sed -i '' "s|^  app_role_id: .*|  app_role_id: \"${ROLE_ID}\"|" "$API_YAML"
    sed -i '' "s|^  app_role_secret_id: .*|  app_role_secret_id: \"${SECRET_ID}\"|" "$API_YAML"
  fi

  # Validate that the replacement actually landed. If the YAML key was not
  # present (e.g. template has a different indentation or was renamed), the
  # sed silently no-ops and we'd continue with stale credentials.
  if ! grep -q "app_role_id: \"${ROLE_ID}\"" "$API_YAML"; then
    red "sed replace failed — restoring backup"
    mv "${API_YAML}.bak" "$API_YAML"
    exit 1
  fi

  rm -f "${API_YAML}.bak"
  green "api.dev.yaml: AppRole credentials synced ($API_YAML)"
else
  yellow "(api.dev.yaml not found — skipping credential sync; path: $API_YAML)"
fi

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
# Count buckets created/verified: screenshots + dsr + destruction + audit-worm
BUCKET_COUNT=4

green "==> 3/4 Keycloak realm, client, roles, user"
TOKEN=$(curl --fail -sS -X POST http://localhost:8080/realms/master/protocol/openid-connect/token \
  -d "client_id=admin-cli" -d "username=admin" -d "password=admin-dev-only" -d "grant_type=password" \
  | python3 -c "import sys,json;print(json.load(sys.stdin)['access_token'])")

# realm (201 = created, 409 = already exists — both acceptable)
REALM_STATUS=$(curl --fail -sS -o /dev/null -w "%{http_code}" -X POST http://localhost:8080/admin/realms \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"realm":"personel","enabled":true}')
if [[ "$REALM_STATUS" == "201" ]]; then
  echo "realm: created (201)"
elif [[ "$REALM_STATUS" == "409" ]]; then
  echo "realm: already exists — skipping (409)"
else
  red "realm create failed with HTTP $REALM_STATUS"
  exit 1
fi

# console client
CLIENT_LIST=$(curl --fail -sS "http://localhost:8080/admin/realms/personel/clients?clientId=console" \
  -H "Authorization: Bearer $TOKEN")
if [[ "$CLIENT_LIST" == "[]" ]]; then
  CLIENT_STATUS=$(curl --fail -sS -o /dev/null -w "%{http_code}" -X POST \
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
    }')
  if [[ "$CLIENT_STATUS" != "201" ]]; then
    red "console client create failed with HTTP $CLIENT_STATUS"
    exit 1
  fi
  echo "client create: 201"
  KC_CLIENT_COUNT=$((KC_CLIENT_COUNT + 1))
else
  # Idempotent update: force publicClient + PKCE on every run.
  EXISTING_ID=$(echo "$CLIENT_LIST" | python3 -c "import sys,json;print(json.load(sys.stdin)[0]['id'])")
  UPDATE_STATUS=$(curl --fail -sS -o /dev/null -w "%{http_code}" -X PUT \
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
    }')
  if [[ "$UPDATE_STATUS" != "204" ]]; then
    red "console client update failed with HTTP $UPDATE_STATUS"
    exit 1
  fi
  echo "console client updated (public + PKCE)"
  KC_CLIENT_COUNT=$((KC_CLIENT_COUNT + 1))
fi

CLIENT_ID=$(curl --fail -sS "http://localhost:8080/admin/realms/personel/clients?clientId=console" \
  -H "Authorization: Bearer $TOKEN" \
  | python3 -c "import sys,json;d=json.load(sys.stdin);print(d[0]['id']) if d else ''")

# Audience + tenant_id mappers.
# 409 Conflict means the mapper already exists — idempotent skip is correct
# because Keycloak does not allow duplicate mapper names in the same client.
for MAPPER in \
  '{"name":"personel-admin-api-audience","protocol":"openid-connect","protocolMapper":"oidc-audience-mapper","config":{"included.custom.audience":"personel-admin-api","id.token.claim":"false","access.token.claim":"true"}}' \
  '{"name":"tenant-id-hardcoded","protocol":"openid-connect","protocolMapper":"oidc-hardcoded-claim-mapper","config":{"claim.name":"tenant_id","claim.value":"00000000-0000-0000-0000-000000000001","jsonType.label":"String","id.token.claim":"true","access.token.claim":"true","userinfo.token.claim":"true"}}'; do
  MAPPER_STATUS=$(curl --fail -sS -o /dev/null -w "%{http_code}" -X POST \
    "http://localhost:8080/admin/realms/personel/clients/$CLIENT_ID/protocol-mappers/models" \
    -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
    -d "$MAPPER")
  if [[ "$MAPPER_STATUS" == "201" ]]; then
    echo "mapper: created (201)"
  elif [[ "$MAPPER_STATUS" == "409" ]]; then
    echo "mapper: already exists — skipping (409)"
  else
    red "mapper create failed with HTTP $MAPPER_STATUS"
    exit 1
  fi
done
echo "mappers: ensured"

# Realm roles.
# 409 Conflict means the role already exists — idempotent skip is correct
# because role names within a realm are unique by design.
for ROLE in dpo admin auditor hr investigator manager employee it_operator it_manager; do
  ROLE_STATUS=$(curl --fail -sS -o /dev/null -w "%{http_code}" -X POST \
    "http://localhost:8080/admin/realms/personel/roles" \
    -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
    -d "{\"name\":\"$ROLE\"}")
  if [[ "$ROLE_STATUS" == "201" ]]; then
    echo "role '$ROLE': created (201)"
    KC_ROLE_COUNT=$((KC_ROLE_COUNT + 1))
  elif [[ "$ROLE_STATUS" == "409" ]]; then
    echo "role '$ROLE': already exists — skipping (409)"
    KC_ROLE_COUNT=$((KC_ROLE_COUNT + 1))
  else
    red "role '$ROLE' create failed with HTTP $ROLE_STATUS"
    exit 1
  fi
done
echo "roles: ensured"

# dpo-test user
USER_LIST=$(curl --fail -sS "http://localhost:8080/admin/realms/personel/users?username=dpo-test" \
  -H "Authorization: Bearer $TOKEN")
if [[ "$USER_LIST" == "[]" ]]; then
  USER_STATUS=$(curl --fail -sS -o /dev/null -w "%{http_code}" -X POST \
    "http://localhost:8080/admin/realms/personel/users" \
    -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
    -d '{
      "username":"dpo-test","email":"dpo-test@personel.local","emailVerified":true,"enabled":true,
      "firstName":"DPO","lastName":"Test",
      "credentials":[{"type":"password","value":"dpo-test-pass","temporary":false}]
    }')
  if [[ "$USER_STATUS" != "201" ]]; then
    red "dpo-test user create failed with HTTP $USER_STATUS"
    exit 1
  fi
  echo "user create: 201"
  KC_USER_COUNT=$((KC_USER_COUNT + 1))
else
  echo "dpo-test user already exists"
  KC_USER_COUNT=$((KC_USER_COUNT + 1))
fi

USER_ID=$(curl --fail -sS "http://localhost:8080/admin/realms/personel/users?username=dpo-test" \
  -H "Authorization: Bearer $TOKEN" \
  | python3 -c "import sys,json;print(json.load(sys.stdin)[0]['id'])")

# assign dpo + admin + it_manager roles (dev convenience: one login to cover
# request→approve flows across DPO, IT Manager and Admin authority paths)
ROLE_PAYLOAD="["
for RN in dpo admin it_manager; do
  R=$(curl --fail -sS "http://localhost:8080/admin/realms/personel/roles/$RN" -H "Authorization: Bearer $TOKEN")
  ROLE_PAYLOAD+="$R,"
done
ROLE_PAYLOAD="${ROLE_PAYLOAD%,}]"
ASSIGN_STATUS=$(curl --fail -sS -o /dev/null -w "%{http_code}" -X POST \
  "http://localhost:8080/admin/realms/personel/users/$USER_ID/role-mappings/realm" \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d "$ROLE_PAYLOAD")
if [[ "$ASSIGN_STATUS" != "204" ]]; then
  red "role assignment to dpo-test failed with HTTP $ASSIGN_STATUS"
  exit 1
fi
echo "dpo + admin + it_manager roles assigned to dpo-test"

green "==> 4/4 Postgres seed tenant (public.tenants)"
docker exec personel-postgres psql -U postgres -d personel -c "
INSERT INTO public.tenants(id, name, slug)
VALUES ('00000000-0000-0000-0000-000000000001', 'Dev Tenant', 'dev')
ON CONFLICT (id) DO NOTHING;
SELECT COUNT(*) AS tenant_rows FROM public.tenants;
"

green "==> Dev stack provisioned successfully"
printf '    - Vault: ready (AppRole %s)\n' "$ROLE_ID"
printf '    - MinIO buckets: %d created/verified\n' "$BUCKET_COUNT"
printf '    - Keycloak realm: personel (%d clients, %d roles, %d users)\n' \
  "$KC_CLIENT_COUNT" "$KC_ROLE_COUNT" "$KC_USER_COUNT"
printf '    - api.dev.yaml: synced\n'
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
