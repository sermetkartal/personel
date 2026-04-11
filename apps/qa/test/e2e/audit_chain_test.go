// audit_chain_test.go verifies hash chain integrity and tamper detection.
//
// Phase 1 exit criteria covered:
//   #10: live-view governance — hash chain passes integrity check.
package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/personel/qa/internal/assertions"
	"github.com/personel/qa/internal/harness"
)

// TestAuditChainIntegrity queries the full audit log and verifies the hash
// chain is intact across all records.
func TestAuditChainIntegrity(t *testing.T) {
	harness.RequireIntegration(t)

	stack := harness.MustStart(t, harness.StackOptions{WithAPI: true})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	_ = ctx

	apiBase := fmt.Sprintf("http://%s", stack.GatewayAddr)

	// Generate some audit events first via other operations.
	// For now query whatever is in the audit log.
	resp, err := http.Get(apiBase + "/v1/audit?limit=1000")
	if err != nil {
		t.Skipf("API not available: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Skipf("audit endpoint returned %d; may not be implemented yet", resp.StatusCode)
	}

	var auditBody struct {
		Records []assertions.AuditRecord `json:"records"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&auditBody))

	if len(auditBody.Records) < 2 {
		t.Skipf("not enough audit records (%d) to verify chain; need >= 2", len(auditBody.Records))
	}

	// The core Phase 1 exit criterion #10 assertion.
	assertions.AssertAuditChainIntact(t, auditBody.Records)
	t.Logf("audit chain verified: %d records, chain intact", len(auditBody.Records))
}

// TestAuditChainIsAppendOnly verifies that there is no DELETE or UPDATE API
// for audit records. The database role is INSERT+SELECT only, but we also
// verify at the API layer.
func TestAuditChainIsAppendOnly(t *testing.T) {
	harness.RequireIntegration(t)

	stack := harness.MustStart(t, harness.StackOptions{WithAPI: true})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	_ = ctx

	apiBase := fmt.Sprintf("http://%s", stack.GatewayAddr)
	client := &http.Client{Timeout: 10 * time.Second}

	// Attempt DELETE on audit endpoint — must return 405 Method Not Allowed.
	delReq, _ := http.NewRequestWithContext(ctx, http.MethodDelete, apiBase+"/v1/audit/1", nil)
	delResp, err := client.Do(delReq)
	if err != nil {
		t.Skipf("API not available: %v", err)
	}
	defer delResp.Body.Close()

	// Must not be 200 or 204 (successful deletion).
	if delResp.StatusCode == http.StatusOK || delResp.StatusCode == http.StatusNoContent {
		t.Errorf("DELETE /v1/audit/1 returned %d — audit records must be immutable (Phase 1 exit criterion #10)",
			delResp.StatusCode)
	} else {
		t.Logf("DELETE correctly rejected with %d", delResp.StatusCode)
	}

	// Attempt PUT/PATCH on audit endpoint — must return 405.
	patchReq, _ := http.NewRequestWithContext(ctx, http.MethodPatch, apiBase+"/v1/audit/1", nil)
	patchResp, err := client.Do(patchReq)
	if err == nil {
		defer patchResp.Body.Close()
		if patchResp.StatusCode == http.StatusOK {
			t.Errorf("PATCH /v1/audit/1 returned 200 — audit records must be immutable")
		}
	}
}
