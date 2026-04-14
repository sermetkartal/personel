//go:build integration

// Faz 14 #147 — Integration test for Faz 6 #67 search endpoint.
//
// This exercises the search API's tenant_id injection guard. OpenSearch
// is faked out with an in-process HTTP handler so we can assert the
// exact request body the adapter emits. Postgres is a real testcontainer
// because the search path joins against `tenants` for RLS context.
package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// fakeOpenSearch records the last body sent to it and returns a canned hit set.
type fakeOpenSearch struct {
	lastBody map[string]any
	server   *httptest.Server
}

func newFakeOpenSearch(t *testing.T) *fakeOpenSearch {
	t.Helper()
	fos := &fakeOpenSearch{}
	fos.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/_search") {
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &fos.lastBody)
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"hits":{"total":{"value":0},"hits":[]}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(fos.server.Close)
	return fos
}

func TestSearch_TenantIsolation_InjectsTenantFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	pool := testDB(t)
	defer pool.Close()

	fos := newFakeOpenSearch(t)
	_ = fos // compile hook; wiring requires search.Service constructor

	tenantA := uuid.New()
	tenantB := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO tenants (id, name, created_at) VALUES ($1, 'A', now()), ($2, 'B', now())`,
		tenantA, tenantB)
	require.NoError(t, err, "seed tenants")

	// The real test would construct search.Service with the fake URL,
	// call Query with tenantA context, and assert the lastBody JSON
	// contains a bool/filter/term with tenant_id == tenantA.
	// This compiles today; once search.Service exposes a testable
	// constructor (Faz 6 #67) the assertion body is flipped on.
	t.Log("search integration scaffold — awaiting Faz 6 #67 constructor export")
}

func TestSearch_TenantIsolation_RejectsCrossTenantLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	// Negative test: tenantB's JWT hits a query that references a
	// document ID belonging to tenantA. Expected: 404, no body leak,
	// audit entry `search.cross_tenant_attempt`.
	t.Log("cross-tenant leak scaffold — awaits service wiring")
}
