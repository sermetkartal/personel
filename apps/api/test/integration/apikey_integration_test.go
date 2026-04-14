//go:build integration

// Faz 14 #147 — Integration test for Faz 6 #72 service-to-service API keys.
//
// Full lifecycle:
//   1. Admin creates an API key (scope=audit.read, TTL=1h)
//   2. Caller uses it to hit /v1/audit/events → 200
//   3. Caller uses it on /v1/endpoints/bulk-wipe → 403 (wrong scope)
//   4. Admin revokes → next call returns 401 (revoked_key)
//   5. Time-skip past TTL → 401 (expired_key)
//   6. Audit log has 3 rows: create, revoke, reject-expired
package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAPIKey_FullLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	pool := testDB(t)
	defer pool.Close()

	tenantID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO tenants (id, name, created_at) VALUES ($1, 'apikey-test', now())`, tenantID)
	require.NoError(t, err)

	// Scenarios (hooks to apikey.Service; currently scaffold):
	//
	// Step 1: apikey.Service.Create(ctx, tenantID, "audit-read", []string{"audit.read"}, time.Hour)
	//         → returns plaintext key K once, stores bcrypt hash.
	// Step 2: middleware.APIKeyAuth(K) → context carries tenant_id + scopes.
	// Step 3: audit.read scope request → 200.
	// Step 4: endpoints.wipe scope request → 403 insufficient_scope.
	// Step 5: apikey.Service.Revoke(ctx, keyID) → state=revoked.
	// Step 6: request with revoked K → 401 + audit `apikey.rejected`.
	// Step 7: time.Skip past TTL (inject clock) → 401 expired.
	//
	// The integration test MUST use a test clock (github.com/benbjohnson/clock
	// or equivalent) because real 1-hour TTLs are infeasible in CI.
	t.Log("apikey lifecycle scaffold — awaiting Faz 6 #72 apikey.Service")
}

func TestAPIKey_RateLimitPerKey(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	// Faz 6 #71 overlap: each API key gets its own rate-limit bucket.
	// 100 req/s burst, 50 req/s sustained. Above → 429 with
	// Retry-After header.
	t.Log("apikey rate-limit scaffold — awaiting Faz 6 #71")
}
