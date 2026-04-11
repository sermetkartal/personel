//go:build integration

package integration

// RBAC tests are pure logic tests — no database required.
// They are placed in the integration package for co-location,
// but use the unit-test approach of constructing Principal directly.

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/personel/api/internal/auth"
)

// principal is a helper to build a *auth.Principal for a given role.
func principal(role auth.Role) *auth.Principal {
	return &auth.Principal{
		UserID:   "test-user",
		TenantID: "test-tenant",
		Roles:    []auth.Role{role},
	}
}

// TestRBAC_Screenshots verifies only Investigator and DPO can read screenshots.
func TestRBAC_Screenshots(t *testing.T) {
	allowed := []auth.Role{auth.RoleInvestigator, auth.RoleDPO}
	denied := []auth.Role{auth.RoleAdmin, auth.RoleManager, auth.RoleHR, auth.RoleAuditor, auth.RoleEmployee}

	for _, role := range allowed {
		assert.True(t, auth.Can(principal(role), auth.OpRead, auth.ResourceScreenshot),
			"role %s should be able to read screenshots", role)
	}
	for _, role := range denied {
		assert.False(t, auth.Can(principal(role), auth.OpRead, auth.ResourceScreenshot),
			"role %s should NOT be able to read screenshots", role)
	}
}

// TestRBAC_Screenclips verifies only Investigator and DPO can read screenclips.
func TestRBAC_Screenclips(t *testing.T) {
	allowed := []auth.Role{auth.RoleInvestigator, auth.RoleDPO}
	denied := []auth.Role{auth.RoleAdmin, auth.RoleManager, auth.RoleHR, auth.RoleAuditor, auth.RoleEmployee}

	for _, role := range allowed {
		assert.True(t, auth.Can(principal(role), auth.OpRead, auth.ResourceScreenclip),
			"role %s should be able to read screenclips", role)
	}
	for _, role := range denied {
		assert.False(t, auth.Can(principal(role), auth.OpRead, auth.ResourceScreenclip),
			"role %s should NOT be able to read screenclips", role)
	}
}

// TestRBAC_LegalHold verifies only DPO can place or release legal holds.
func TestRBAC_LegalHold(t *testing.T) {
	// Place (write)
	assert.True(t, auth.Can(principal(auth.RoleDPO), auth.OpWrite, auth.ResourceLegalHold),
		"DPO should be able to place legal holds")
	for _, role := range []auth.Role{auth.RoleAdmin, auth.RoleManager, auth.RoleHR, auth.RoleInvestigator, auth.RoleAuditor, auth.RoleEmployee} {
		assert.False(t, auth.Can(principal(role), auth.OpWrite, auth.ResourceLegalHold),
			"role %s should NOT be able to place legal holds", role)
	}

	// Release (delete)
	assert.True(t, auth.Can(principal(auth.RoleDPO), auth.OpDelete, auth.ResourceLegalHold),
		"DPO should be able to release legal holds")
	for _, role := range []auth.Role{auth.RoleAdmin, auth.RoleManager, auth.RoleHR, auth.RoleInvestigator, auth.RoleAuditor, auth.RoleEmployee} {
		assert.False(t, auth.Can(principal(role), auth.OpDelete, auth.ResourceLegalHold),
			"role %s should NOT be able to release legal holds", role)
	}
}

// TestRBAC_DestructionReport verifies only DPO can download destruction reports.
func TestRBAC_DestructionReport(t *testing.T) {
	assert.True(t, auth.Can(principal(auth.RoleDPO), auth.OpDownload, auth.ResourceDestruction),
		"DPO should be able to download destruction reports")
	for _, role := range []auth.Role{auth.RoleAdmin, auth.RoleManager, auth.RoleHR, auth.RoleInvestigator, auth.RoleAuditor, auth.RoleEmployee} {
		assert.False(t, auth.Can(principal(role), auth.OpDownload, auth.ResourceDestruction),
			"role %s should NOT be able to download destruction reports", role)
	}
}

// TestRBAC_LiveViewApproval verifies only HR can approve or reject live view requests.
func TestRBAC_LiveViewApproval(t *testing.T) {
	assert.True(t, auth.Can(principal(auth.RoleHR), auth.OpApprove, auth.ResourceLiveView),
		"HR should be able to approve live view")
	assert.True(t, auth.Can(principal(auth.RoleHR), auth.OpReject, auth.ResourceLiveView),
		"HR should be able to reject live view")
	for _, role := range []auth.Role{auth.RoleAdmin, auth.RoleManager, auth.RoleDPO, auth.RoleInvestigator, auth.RoleAuditor, auth.RoleEmployee} {
		assert.False(t, auth.Can(principal(role), auth.OpApprove, auth.ResourceLiveView),
			"role %s should NOT be able to approve live view", role)
		assert.False(t, auth.Can(principal(role), auth.OpReject, auth.ResourceLiveView),
			"role %s should NOT be able to reject live view", role)
	}
}

// TestRBAC_LiveViewTermination verifies only HR and DPO can terminate live view sessions.
func TestRBAC_LiveViewTermination(t *testing.T) {
	allowed := []auth.Role{auth.RoleHR, auth.RoleDPO}
	denied := []auth.Role{auth.RoleAdmin, auth.RoleManager, auth.RoleInvestigator, auth.RoleAuditor, auth.RoleEmployee}

	for _, role := range allowed {
		assert.True(t, auth.Can(principal(role), auth.OpTerminate, auth.ResourceLiveView),
			"role %s should be able to terminate live view", role)
	}
	for _, role := range denied {
		assert.False(t, auth.Can(principal(role), auth.OpTerminate, auth.ResourceLiveView),
			"role %s should NOT be able to terminate live view", role)
	}
}

// TestRBAC_AuditLog verifies only Auditor and DPO can read the audit log,
// and no role can write to it directly.
func TestRBAC_AuditLog(t *testing.T) {
	allowed := []auth.Role{auth.RoleAuditor, auth.RoleDPO}
	denied := []auth.Role{auth.RoleAdmin, auth.RoleManager, auth.RoleHR, auth.RoleInvestigator, auth.RoleEmployee}

	for _, role := range allowed {
		assert.True(t, auth.Can(principal(role), auth.OpRead, auth.ResourceAuditLog),
			"role %s should be able to read audit log", role)
	}
	for _, role := range denied {
		assert.False(t, auth.Can(principal(role), auth.OpRead, auth.ResourceAuditLog),
			"role %s should NOT be able to read audit log", role)
	}

	// No role may write to the audit log directly.
	for _, role := range append(allowed, denied...) {
		assert.False(t, auth.Can(principal(role), auth.OpWrite, auth.ResourceAuditLog),
			"no role should be able to write audit log directly: %s", role)
	}
}

// TestRBAC_Employee verifies employees can only access their own data and submit DSRs.
func TestRBAC_Employee(t *testing.T) {
	p := principal(auth.RoleEmployee)

	// Can access own data.
	assert.True(t, auth.Can(p, auth.OpRead, auth.ResourceMyData))

	// Can submit DSR.
	assert.True(t, auth.Can(p, auth.OpRequest, auth.ResourceDSR))

	// Cannot access other resources.
	restricted := []auth.Resource{
		auth.ResourceUser, auth.ResourceEndpoint, auth.ResourcePolicy,
		auth.ResourceScreenshot, auth.ResourceScreenclip, auth.ResourceReport,
		auth.ResourceAuditLog, auth.ResourceLegalHold, auth.ResourceDestruction,
		auth.ResourceDLPMatch,
	}
	for _, res := range restricted {
		assert.False(t, auth.Can(p, auth.OpRead, res),
			"employee should NOT be able to read %s", res)
	}
}

// TestRBAC_DSRRespond verifies only DPO can respond/assign DSRs.
func TestRBAC_DSRRespond(t *testing.T) {
	// Respond and assign are OpWrite / OpApprove in the RBAC matrix.
	for _, role := range []auth.Role{auth.RoleAdmin, auth.RoleManager, auth.RoleHR, auth.RoleInvestigator, auth.RoleAuditor, auth.RoleEmployee} {
		assert.False(t, auth.Can(principal(role), auth.OpWrite, auth.ResourceDSR),
			"role %s should NOT be able to respond to DSR", role)
		assert.False(t, auth.Can(principal(role), auth.OpApprove, auth.ResourceDSR),
			"role %s should NOT be able to assign DSR", role)
	}
	assert.True(t, auth.Can(principal(auth.RoleDPO), auth.OpWrite, auth.ResourceDSR))
	assert.True(t, auth.Can(principal(auth.RoleDPO), auth.OpApprove, auth.ResourceDSR))
}

// TestRBAC_NilPrincipal verifies Can() returns false for nil principal.
func TestRBAC_NilPrincipal(t *testing.T) {
	assert.False(t, auth.Can(nil, auth.OpRead, auth.ResourceReport))
}

// TestRBAC_AssertApproverDiffersFromRequester verifies the dual-control check.
func TestRBAC_AssertApproverDiffersFromRequester(t *testing.T) {
	approver := &auth.Principal{UserID: "approver-1", Roles: []auth.Role{auth.RoleHR}}
	requester := &auth.Principal{UserID: "requester-1", Roles: []auth.Role{auth.RoleManager}}
	same := &auth.Principal{UserID: "approver-1", Roles: []auth.Role{auth.RoleHR}}

	// Different users — should pass.
	assert.NoError(t, auth.AssertApproverDiffersFromRequester(approver.UserID, requester.UserID))

	// Same user ID — must fail.
	assert.Error(t, auth.AssertApproverDiffersFromRequester(same.UserID, approver.UserID),
		"same user cannot be both approver and requester")
}
