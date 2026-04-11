#!/usr/bin/env bash
# =============================================================================
# Personel Platform — Post-Install Smoke Tests
# TR: Kurulum sonrası temel doğrulama testleri.
# EN: Basic validation tests after install.
# =============================================================================
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
COMPOSE_DIR="${SCRIPT_DIR}/../compose"
set -a; source "${COMPOSE_DIR}/.env" 2>/dev/null || true; set +a

PASS=0; FAIL=0
pass() { echo -e "\033[0;32m[PASS]\033[0m $*"; ((PASS++)) || true; }
fail() { echo -e "\033[0;31m[FAIL]\033[0m $*" >&2; ((FAIL++)) || true; }

echo ""
echo "=== Personel Platform Smoke Tests ==="
echo ""

# ---------------------------------------------------------------------------
# Service health
# ---------------------------------------------------------------------------
echo "--- Container Health ---"
for service in vault postgres clickhouse nats minio opensearch keycloak gateway api enricher dlp console portal; do
  STATUS=$(docker compose -f "${COMPOSE_DIR}/docker-compose.yaml" ps "${service}" 2>/dev/null \
    | tail -1 | grep -oE '(healthy|unhealthy|starting|running|exited)' | head -1 || echo "unknown")
  if [[ "${STATUS}" == "healthy" ]]; then
    pass "${service}: healthy"
  elif [[ "${STATUS}" == "running" ]]; then
    pass "${service}: running (no healthcheck)"
  else
    fail "${service}: ${STATUS}"
  fi
done

# ---------------------------------------------------------------------------
# Connectivity
# ---------------------------------------------------------------------------
echo ""
echo "--- Connectivity ---"

# PostgreSQL
if docker exec personel-postgres pg_isready -U postgres -d personel &>/dev/null; then
  pass "PostgreSQL: accepting connections"
else
  fail "PostgreSQL: not ready"
fi

# ClickHouse
if curl -sf "http://localhost:${CLICKHOUSE_HTTP_PORT:-8123}/ping" | grep -q "Ok"; then
  pass "ClickHouse: responding to ping"
else
  fail "ClickHouse: not responding"
fi

# NATS
if curl -sf "http://localhost:${NATS_MONITOR_PORT:-8222}/healthz" | grep -q "ok"; then
  pass "NATS: health check OK"
else
  fail "NATS: health check failed"
fi

# MinIO
if curl -sf "http://localhost:${MINIO_PORT:-9000}/minio/health/live" &>/dev/null; then
  pass "MinIO: live check OK"
else
  fail "MinIO: not responding"
fi

# Admin API
if curl -sf "http://localhost:${API_HTTP_PORT:-8000}/health" &>/dev/null; then
  pass "Admin API: health endpoint OK"
else
  fail "Admin API: health endpoint not responding"
fi

# ---------------------------------------------------------------------------
# Audit schema
# ---------------------------------------------------------------------------
echo ""
echo "--- Database Schema ---"

TABLE_COUNT=$(docker exec personel-postgres psql -U postgres -d personel -t -c \
  "SELECT count(*) FROM information_schema.tables WHERE table_schema = 'core'" \
  2>/dev/null | tr -d ' ')

if [[ "${TABLE_COUNT:-0}" -ge 8 ]]; then
  pass "PostgreSQL: core schema has ${TABLE_COUNT} tables"
else
  fail "PostgreSQL: expected >=8 core tables, found ${TABLE_COUNT:-0}"
fi

# Append-only trigger check
TRIGGER_EXISTS=$(docker exec personel-postgres psql -U postgres -d personel -t -c \
  "SELECT count(*) FROM pg_trigger WHERE tgname = 'audit_no_delete'" \
  2>/dev/null | tr -d ' ')
if [[ "${TRIGGER_EXISTS:-0}" -ge 1 ]]; then
  pass "PostgreSQL: audit append-only trigger exists"
else
  fail "PostgreSQL: audit_no_delete trigger missing"
fi

# ---------------------------------------------------------------------------
# MinIO buckets
# ---------------------------------------------------------------------------
echo ""
echo "--- MinIO Buckets ---"
for bucket in screenshots keystroke-blobs dsr-responses destruction-reports backups; do
  if docker exec personel-minio mc ls "personel/${bucket}" &>/dev/null; then
    pass "MinIO bucket: ${bucket} exists"
  else
    fail "MinIO bucket: ${bucket} missing"
  fi
done

# ---------------------------------------------------------------------------
# DLP isolation (verify DLP cannot be reached from API network)
# ---------------------------------------------------------------------------
echo ""
echo "--- DLP Isolation ---"
# DLP should only be on dlp-isolated network, not reachable from api container
if ! docker exec personel-api ping -c1 -W1 personel-dlp &>/dev/null 2>&1; then
  pass "DLP: not reachable from api container (correct isolation)"
else
  fail "DLP: REACHABLE from api container — network isolation broken!"
fi

# ---------------------------------------------------------------------------
# Vault seal status
# ---------------------------------------------------------------------------
echo ""
echo "--- Vault ---"
VAULT_SEALED=$(docker exec personel-vault vault status \
  -address=https://127.0.0.1:8200 -tls-skip-verify -format=json 2>/dev/null \
  | python3 -c "import json,sys; print(json.load(sys.stdin).get('sealed',True))" \
  2>/dev/null || echo "true")
if [[ "${VAULT_SEALED}" == "False" ]]; then
  pass "Vault: unsealed"
else
  fail "Vault: sealed (unseal required)"
fi

# ---------------------------------------------------------------------------
# Keycloak realm
# ---------------------------------------------------------------------------
echo ""
echo "--- Keycloak ---"
REALM_STATUS=$(curl -sf \
  "http://localhost:${KEYCLOAK_PORT:-8080}/realms/personel/.well-known/openid-configuration" \
  2>/dev/null | python3 -c "import json,sys; print(json.load(sys.stdin).get('issuer',''))" \
  2>/dev/null || echo "")
if [[ -n "${REALM_STATUS}" ]]; then
  pass "Keycloak realm 'personel': OIDC endpoint available"
else
  fail "Keycloak realm 'personel': OIDC endpoint not available"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo ""
echo "=============================="
echo "  PASS: ${PASS}  FAIL: ${FAIL}"
echo "=============================="
echo ""

if [[ "${FAIL}" -gt 0 ]]; then
  echo -e "\033[0;31mSmoke tests FAILED (${FAIL} failures)\033[0m"
  exit 1
else
  echo -e "\033[0;32mAll smoke tests PASSED\033[0m"
  exit 0
fi
