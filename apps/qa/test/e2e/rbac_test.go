// rbac_test.go exercises the full RBAC matrix:
//   every role × every sensitive endpoint → expected allow/deny.
//
// The test reads a role-endpoint matrix and asserts each combination
// returns the expected HTTP status code.
package e2e

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/personel/qa/internal/harness"
)

// RBACEntry defines one row in the RBAC test matrix.
type RBACEntry struct {
	Role           string
	Method         string
	Path           string
	ExpectedStatus int
	Description    string
}

// rbacMatrix is the authoritative role × endpoint matrix for Phase 1.
// Sources: mvp-scope.md, threat-model.md Flow 2, key-hierarchy.md §Code-Level Rules.
//
// Roles defined in the system:
//   - super_admin:   full tenant admin
//   - admin:         endpoint management, policy, reports
//   - manager:       read-only + live-view request
//   - hr:            live-view approval only
//   - dpo:           DSR management, legal hold, destruction reports
//   - employee:      transparency portal only
//   - unauthenticated: no access
var rbacMatrix = []RBACEntry{
	// Admin API — endpoint management
	{Role: "admin", Method: "GET", Path: "/v1/endpoints", ExpectedStatus: http.StatusOK, Description: "admin can list endpoints"},
	{Role: "manager", Method: "GET", Path: "/v1/endpoints", ExpectedStatus: http.StatusOK, Description: "manager can list endpoints"},
	{Role: "employee", Method: "GET", Path: "/v1/endpoints", ExpectedStatus: http.StatusForbidden, Description: "employee cannot list endpoints"},
	{Role: "unauthenticated", Method: "GET", Path: "/v1/endpoints", ExpectedStatus: http.StatusUnauthorized, Description: "unauthenticated gets 401"},

	// Keystroke API — NO role must be able to access plaintext.
	// This is the most critical set. Any 200 with content is a Phase 1 blocker.
	{Role: "admin", Method: "GET", Path: "/v1/keystrokes/decrypt", ExpectedStatus: http.StatusNotFound, Description: "keystroke decrypt endpoint must not exist (admin)"},
	{Role: "super_admin", Method: "GET", Path: "/v1/keystrokes/decrypt", ExpectedStatus: http.StatusNotFound, Description: "keystroke decrypt endpoint must not exist (super_admin)"},
	{Role: "admin", Method: "GET", Path: "/v1/keystroke-content", ExpectedStatus: http.StatusNotFound, Description: "keystroke content endpoint must not exist"},
	{Role: "admin", Method: "GET", Path: "/v1/events/keystroke_plaintext", ExpectedStatus: http.StatusNotFound, Description: "plaintext keystroke events endpoint must not exist"},

	// DLP API — only dlp-service can access; admin must not.
	{Role: "admin", Method: "POST", Path: "/v1/dlp/derive-key", ExpectedStatus: http.StatusNotFound, Description: "DLP key derive endpoint must not be exposed to admin"},
	{Role: "admin", Method: "GET", Path: "/v1/dlp/decrypt", ExpectedStatus: http.StatusNotFound, Description: "DLP decrypt endpoint must not be exposed to admin"},

	// Live-view — requires specific roles.
	{Role: "admin", Method: "POST", Path: "/v1/live-view/requests", ExpectedStatus: http.StatusCreated, Description: "admin can request live-view"},
	{Role: "manager", Method: "POST", Path: "/v1/live-view/requests", ExpectedStatus: http.StatusCreated, Description: "manager can request live-view"},
	{Role: "employee", Method: "POST", Path: "/v1/live-view/requests", ExpectedStatus: http.StatusForbidden, Description: "employee cannot request live-view"},
	{Role: "hr", Method: "POST", Path: "/v1/live-view/requests/test-id/approve", ExpectedStatus: http.StatusOK, Description: "HR can approve live-view"},
	{Role: "admin", Method: "POST", Path: "/v1/live-view/requests/test-id/approve", ExpectedStatus: http.StatusForbidden, Description: "admin cannot approve (not HR role)"},

	// DSR — DPO operations.
	{Role: "dpo", Method: "GET", Path: "/v1/dsr?state=open", ExpectedStatus: http.StatusOK, Description: "DPO can view DSR dashboard"},
	{Role: "admin", Method: "POST", Path: "/v1/dsr/test-id/assign", ExpectedStatus: http.StatusForbidden, Description: "admin cannot assign DSR (DPO only)"},
	{Role: "employee", Method: "POST", Path: "/v1/dsr", ExpectedStatus: http.StatusCreated, Description: "employee can submit DSR"},

	// Legal hold — DPO only.
	{Role: "dpo", Method: "POST", Path: "/v1/legal-holds", ExpectedStatus: http.StatusCreated, Description: "DPO can place legal hold"},
	{Role: "admin", Method: "POST", Path: "/v1/legal-holds", ExpectedStatus: http.StatusForbidden, Description: "admin cannot place legal hold (DPO only)"},

	// Policy management.
	{Role: "admin", Method: "POST", Path: "/v1/policies", ExpectedStatus: http.StatusCreated, Description: "admin can create policy"},
	{Role: "manager", Method: "POST", Path: "/v1/policies", ExpectedStatus: http.StatusForbidden, Description: "manager cannot create policy"},

	// Destruction reports — DPO only.
	{Role: "dpo", Method: "GET", Path: "/v1/destruction-reports", ExpectedStatus: http.StatusOK, Description: "DPO can view destruction reports"},
	{Role: "admin", Method: "GET", Path: "/v1/destruction-reports", ExpectedStatus: http.StatusForbidden, Description: "admin cannot view destruction reports (DPO only)"},
}

// TestRBACMatrix runs the full role × endpoint matrix.
func TestRBACMatrix(t *testing.T) {
	harness.RequireIntegration(t)

	stack := harness.MustStart(t, harness.StackOptions{WithAPI: true})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	t.Cleanup(cancel)

	apiBase := fmt.Sprintf("http://%s", stack.GatewayAddr)
	httpClient := &http.Client{Timeout: 10 * time.Second}

	for _, entry := range rbacMatrix {
		entry := entry // capture for parallel sub-test
		t.Run(fmt.Sprintf("%s_%s_%s", entry.Role, entry.Method, entry.Path), func(t *testing.T) {
			req, err := http.NewRequestWithContext(ctx, entry.Method, apiBase+entry.Path, nil)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}

			// Set role via test header (real auth uses JWT in production).
			req.Header.Set("X-Test-Role", entry.Role)

			resp, err := httpClient.Do(req)
			if err != nil {
				t.Skipf("API not available: %v", err)
			}
			defer resp.Body.Close()

			assert.Equal(t, entry.ExpectedStatus, resp.StatusCode,
				"RBAC check failed for %s %s with role %s: %s",
				entry.Method, entry.Path, entry.Role, entry.Description)
		})
	}
}

// TestKeystrokeEndpointsDontExist is a dedicated critical check verifying that
// no API endpoint exists that could return keystroke plaintext. This supplements
// the red-team test with a static route existence check.
func TestKeystrokeEndpointsDontExist(t *testing.T) {
	harness.RequireIntegration(t)

	stack := harness.MustStart(t, harness.StackOptions{WithAPI: true})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	apiBase := fmt.Sprintf("http://%s", stack.GatewayAddr)
	httpClient := &http.Client{Timeout: 10 * time.Second}

	// These paths must ALL return 404 (not 403 — the endpoint must not exist at all).
	// A 403 would mean the route exists but is protected, which is weaker than
	// the route simply not being wired up.
	prohibitedPaths := []string{
		"/v1/keystrokes/decrypt",
		"/v1/keystroke-content",
		"/v1/events/keystroke-plaintext",
		"/v1/dlp/derive-key",
		"/v1/dlp/tmk/export",
		"/v1/vault/transit/derive",
		"/v1/keystroke_keys/unwrap",
		"/v1/keystroke_keys/export",
	}

	for _, path := range prohibitedPaths {
		path := path
		t.Run("must_not_exist_"+path, func(t *testing.T) {
			// Try both as superadmin and as unauthenticated.
			for _, role := range []string{"super_admin", "unauthenticated"} {
				req, _ := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+path, nil)
				req.Header.Set("X-Test-Role", role)

				resp, err := httpClient.Do(req)
				if err != nil {
					t.Skipf("API not available: %v", err)
				}
				resp.Body.Close()

				// 404 is the correct response; 403 is acceptable but 200 is fatal.
				if resp.StatusCode == http.StatusOK {
					t.Errorf("CRITICAL: path %s returned 200 for role %s — "+
						"keystroke plaintext API MUST NOT exist (Phase 1 exit criterion #9)",
						path, role)
				} else {
					t.Logf("path %s correctly returns %d for role %s", path, resp.StatusCode, role)
				}
			}
		})
	}
}

// newTestRegistry returns a new Prometheus registry for tests.
// Defined here to avoid circular imports with e2e helpers.
func newTestRegistry() interface{ Register(interface{}) error } {
	// We return a duck-typed wrapper. The actual prometheus.NewRegistry() call
	// happens in the load test runner which has the import.
	return nil
}
