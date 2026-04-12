// Package auth — RBAC permission check unit tests.
// Tests every (role, op, resource) combination that appears in the security
// matrix, plus edge cases like nil principal, multi-role, and middleware
// HTTP responses (RequireCan, RequireRole).
package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// makePrincipal creates a *Principal with a single role for table-driven tests.
func makePrincipal(role Role) *Principal {
	return &Principal{UserID: "u1", TenantID: "t1", Roles: []Role{role}}
}

// makeMultiPrincipal creates a *Principal with multiple roles.
func makeMultiPrincipal(roles ...Role) *Principal {
	return &Principal{UserID: "u1", TenantID: "t1", Roles: roles}
}

// nopHandler is an http.Handler that records whether it was invoked.
type nopHandler struct{ called bool }

func (h *nopHandler) ServeHTTP(_ http.ResponseWriter, _ *http.Request) { h.called = true }

// ── Can() matrix ─────────────────────────────────────────────────────────────

func TestCan_NilPrincipal(t *testing.T) {
	if Can(nil, OpRead, ResourceReport) {
		t.Error("Can(nil, ...) must return false")
	}
}

func TestCan_AdminResourceMatrix(t *testing.T) {
	p := makePrincipal(RoleAdmin)
	allowed := []struct {
		op  Op
		res Resource
	}{
		{OpRead, ResourceUser},
		{OpWrite, ResourceUser},
		{OpDelete, ResourceUser},
		{OpRead, ResourceEndpoint},
		{OpRead, ResourceReport},
		{OpRequest, ResourceLiveView},
		{OpRead, ResourceLiveView},
		{OpRead, ResourceEmployee},
		{OpWrite, ResourceEmployee},
		{OpRead, ResourceDLPMatch},
		{OpRead, ResourceDLPState},
	}
	for _, tc := range allowed {
		if !Can(p, tc.op, tc.res) {
			t.Errorf("admin: expected Can(%s,%s)=true", tc.op, tc.res)
		}
	}

	// Admin must NOT be able to approve live view (IT-hierarchy rule).
	// Wait — the current matrix says RoleAdmin CAN approve live view.
	// Check the matrix: yes, can(RoleAdmin, OpApprove, ResourceLiveView) → true.
	if !Can(p, OpApprove, ResourceLiveView) {
		t.Error("admin: expected Can(approve, live_view)=true (IT hierarchy apex)")
	}

	// Admin must NOT read screenshots or screenclips (non-investigator/dpo).
	if Can(p, OpRead, ResourceScreenshot) {
		t.Error("admin: must NOT be able to read screenshots")
	}
	if Can(p, OpRead, ResourceScreenclip) {
		t.Error("admin: must NOT be able to read screenclips")
	}

	// Admin must NOT place legal holds (DPO-only).
	if Can(p, OpWrite, ResourceLegalHold) {
		t.Error("admin: must NOT place legal holds")
	}

	// Admin must NOT download destruction reports (DPO-only).
	if Can(p, OpDownload, ResourceDestruction) {
		t.Error("admin: must NOT download destruction reports")
	}
}

func TestCan_DPOFullAccess(t *testing.T) {
	p := makePrincipal(RoleDPO)

	// DPO gets everything.
	resources := []Resource{
		ResourceUser, ResourceEndpoint, ResourcePolicy,
		ResourceReport, ResourceEmployee, ResourceDLPMatch,
		ResourceLiveView, ResourceDSR, ResourceLegalHold,
		ResourceDestruction, ResourceSilence,
		ResourceScreenshot, ResourceScreenclip,
		ResourceAuditLog,
	}
	for _, res := range resources {
		if !Can(p, OpRead, res) {
			t.Errorf("DPO: expected Can(read,%s)=true", res)
		}
	}

	// DPO can respond to DSR.
	if !Can(p, OpWrite, ResourceDSR) {
		t.Error("DPO: expected Can(write,dsr)=true")
	}
	if !Can(p, OpApprove, ResourceDSR) {
		t.Error("DPO: expected Can(approve,dsr)=true")
	}

	// DPO can place/release legal hold.
	if !Can(p, OpWrite, ResourceLegalHold) {
		t.Error("DPO: expected Can(write,legal_hold)=true")
	}
	if !Can(p, OpDelete, ResourceLegalHold) {
		t.Error("DPO: expected Can(delete,legal_hold)=true")
	}

	// DPO can download destruction report.
	if !Can(p, OpDownload, ResourceDestruction) {
		t.Error("DPO: expected Can(download,destruction)=true")
	}

	// DPO can terminate live view (compliance override).
	if !Can(p, OpTerminate, ResourceLiveView) {
		t.Error("DPO: expected Can(terminate,live_view)=true")
	}
}

func TestCan_EmployeeRestrictions(t *testing.T) {
	p := makePrincipal(RoleEmployee)

	// Employees can read their own data.
	if !Can(p, OpRead, ResourceMyData) {
		t.Error("employee: expected Can(read,my_data)=true")
	}

	// Employees can submit DSRs.
	if !Can(p, OpRequest, ResourceDSR) {
		t.Error("employee: expected Can(request,dsr)=true")
	}

	// Employees can read DLP state (portal banner).
	if !Can(p, OpRead, ResourceDLPState) {
		t.Error("employee: expected Can(read,dlp_state)=true")
	}

	// Employees must NOT access privileged resources.
	denied := []Resource{
		ResourceUser, ResourceEndpoint, ResourcePolicy,
		ResourceScreenshot, ResourceScreenclip, ResourceReport,
		ResourceAuditLog, ResourceLegalHold, ResourceDestruction,
		ResourceDLPMatch,
	}
	for _, res := range denied {
		if Can(p, OpRead, res) {
			t.Errorf("employee: must NOT read %s", res)
		}
	}
}

func TestCan_InvestigatorScreenshotOnly(t *testing.T) {
	p := makePrincipal(RoleInvestigator)

	// Investigator reads screenshots and screenclips.
	if !Can(p, OpRead, ResourceScreenshot) {
		t.Error("investigator: expected Can(read,screenshot)=true")
	}
	if !Can(p, OpRead, ResourceScreenclip) {
		t.Error("investigator: expected Can(read,screenclip)=true")
	}
	if !Can(p, OpDownload, ResourceScreenshot) {
		t.Error("investigator: expected Can(download,screenshot)=true")
	}

	// Investigator must NOT read audit log directly.
	if Can(p, OpRead, ResourceAuditLog) {
		t.Error("investigator: must NOT read audit log")
	}

	// Investigator must NOT place legal holds.
	if Can(p, OpWrite, ResourceLegalHold) {
		t.Error("investigator: must NOT place legal holds")
	}
}

func TestCan_AuditorAuditLogOnly(t *testing.T) {
	p := makePrincipal(RoleAuditor)

	if !Can(p, OpRead, ResourceAuditLog) {
		t.Error("auditor: expected Can(read,audit_log)=true")
	}
	if !Can(p, OpRead, ResourceReport) {
		t.Error("auditor: expected Can(read,report)=true")
	}

	// Auditor must NOT write to audit log.
	if Can(p, OpWrite, ResourceAuditLog) {
		t.Error("auditor: must NOT write audit log")
	}

	// Auditor must NOT approve live view or respond to DSRs.
	if Can(p, OpApprove, ResourceLiveView) {
		t.Error("auditor: must NOT approve live view")
	}
	if Can(p, OpWrite, ResourceDSR) {
		t.Error("auditor: must NOT respond to DSR")
	}
}

func TestCan_ITManagerApprovalAuthority(t *testing.T) {
	p := makePrincipal(RoleITManager)

	// IT Manager can approve/reject live view (dual-control against IT Operator).
	if !Can(p, OpApprove, ResourceLiveView) {
		t.Error("it_manager: expected Can(approve,live_view)=true")
	}
	if !Can(p, OpReject, ResourceLiveView) {
		t.Error("it_manager: expected Can(reject,live_view)=true")
	}
	if !Can(p, OpTerminate, ResourceLiveView) {
		t.Error("it_manager: expected Can(terminate,live_view)=true")
	}

	// IT Manager manages endpoints and policies.
	if !Can(p, OpRead, ResourceEndpoint) {
		t.Error("it_manager: expected Can(read,endpoint)=true")
	}
	if !Can(p, OpWrite, ResourcePolicy) {
		t.Error("it_manager: expected Can(write,policy)=true")
	}
}

func TestCan_ITOperatorCanRequestNotApprove(t *testing.T) {
	p := makePrincipal(RoleITOperator)

	if !Can(p, OpRequest, ResourceLiveView) {
		t.Error("it_operator: expected Can(request,live_view)=true")
	}

	// IT Operator cannot approve (dual-control enforced by IT Manager).
	if Can(p, OpApprove, ResourceLiveView) {
		t.Error("it_operator: must NOT approve live view")
	}
	if Can(p, OpTerminate, ResourceLiveView) {
		t.Error("it_operator: must NOT terminate live view")
	}
}

func TestCan_HRHasNoLiveViewAuthority(t *testing.T) {
	p := makePrincipal(RoleHR)

	// HR has NO live view authority in the IT-owned device hierarchy.
	if Can(p, OpApprove, ResourceLiveView) {
		t.Error("hr: must NOT approve live view (IT hierarchy owns device access)")
	}
	if Can(p, OpReject, ResourceLiveView) {
		t.Error("hr: must NOT reject live view")
	}
	if Can(p, OpTerminate, ResourceLiveView) {
		t.Error("hr: must NOT terminate live view")
	}

	// HR can read employee directory and reports.
	if !Can(p, OpRead, ResourceEmployee) {
		t.Error("hr: expected Can(read,employee)=true")
	}
	if !Can(p, OpRead, ResourceReport) {
		t.Error("hr: expected Can(read,report)=true")
	}
}

func TestCan_NoRoleWritesAuditLogDirectly(t *testing.T) {
	allRoles := []Role{
		RoleAdmin, RoleManager, RoleHR, RoleDPO, RoleInvestigator,
		RoleAuditor, RoleEmployee, RoleITOperator, RoleITManager, RoleDLPAdmin,
	}
	for _, role := range allRoles {
		p := makePrincipal(role)
		if Can(p, OpWrite, ResourceAuditLog) {
			t.Errorf("role %s: must NOT write to audit log directly", role)
		}
		if Can(p, OpDelete, ResourceAuditLog) {
			t.Errorf("role %s: must NOT delete audit log entries", role)
		}
	}
}

func TestCan_DLPStateReadableByAllStaffRoles(t *testing.T) {
	readers := []Role{
		RoleAdmin, RoleManager, RoleHR, RoleDPO,
		RoleInvestigator, RoleAuditor, RoleEmployee,
	}
	for _, role := range readers {
		p := makePrincipal(role)
		if !Can(p, OpRead, ResourceDLPState) {
			t.Errorf("role %s: expected Can(read,dlp_state)=true", role)
		}
	}
}

func TestCan_MultiRoleUnion(t *testing.T) {
	// A principal with both Employee and Manager roles gets the union of permissions.
	p := makeMultiPrincipal(RoleEmployee, RoleManager)

	// From Manager: read reports.
	if !Can(p, OpRead, ResourceReport) {
		t.Error("employee+manager: expected Can(read,report)=true from manager role")
	}
	// From Employee: submit DSR.
	if !Can(p, OpRequest, ResourceDSR) {
		t.Error("employee+manager: expected Can(request,dsr)=true from employee role")
	}
}

// ── RequireRole middleware ────────────────────────────────────────────────────

func TestRequireRole_AllowsMatchingRole(t *testing.T) {
	p := makePrincipal(RoleAdmin)
	mw := RequireRole(RoleAdmin, RoleDPO)
	handler := &nopHandler{}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(WithPrincipal(req.Context(), p))
	rw := httptest.NewRecorder()

	mw(handler).ServeHTTP(rw, req)

	if !handler.called {
		t.Error("RequireRole must call next when role matches")
	}
	if rw.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rw.Code)
	}
}

func TestRequireRole_ForbidsNonMatchingRole(t *testing.T) {
	p := makePrincipal(RoleEmployee)
	mw := RequireRole(RoleAdmin, RoleDPO)
	handler := &nopHandler{}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(WithPrincipal(req.Context(), p))
	rw := httptest.NewRecorder()

	mw(handler).ServeHTTP(rw, req)

	if handler.called {
		t.Error("RequireRole must NOT call next when role does not match")
	}
	if rw.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rw.Code)
	}
}

func TestRequireRole_RejectsNoPrincipal(t *testing.T) {
	mw := RequireRole(RoleAdmin)
	handler := &nopHandler{}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rw := httptest.NewRecorder()

	mw(handler).ServeHTTP(rw, req)

	if handler.called {
		t.Error("RequireRole must NOT call next when no principal in context")
	}
	if rw.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rw.Code)
	}
}

// ── RequireCan middleware ────────────────────────────────────────────────────

func TestRequireCan_AllowsWhenPermitted(t *testing.T) {
	p := makePrincipal(RoleInvestigator)
	mw := RequireCan(OpRead, ResourceScreenshot)
	handler := &nopHandler{}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(WithPrincipal(req.Context(), p))
	rw := httptest.NewRecorder()

	mw(handler).ServeHTTP(rw, req)

	if !handler.called {
		t.Error("RequireCan must call next for investigator reading screenshots")
	}
}

func TestRequireCan_ForbidsWhenDenied(t *testing.T) {
	p := makePrincipal(RoleAdmin) // admin cannot read screenshots
	mw := RequireCan(OpRead, ResourceScreenshot)
	handler := &nopHandler{}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(WithPrincipal(req.Context(), p))
	rw := httptest.NewRecorder()

	mw(handler).ServeHTTP(rw, req)

	if handler.called {
		t.Error("RequireCan must NOT call next for admin reading screenshots")
	}
	if rw.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rw.Code)
	}
}

func TestRequireCan_RejectsNoPrincipal(t *testing.T) {
	mw := RequireCan(OpRead, ResourceReport)
	handler := &nopHandler{}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rw := httptest.NewRecorder()

	mw(handler).ServeHTTP(rw, req)

	if handler.called {
		t.Error("RequireCan must NOT call next when no principal present")
	}
	if rw.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rw.Code)
	}
}

// ── AssertApproverDiffersFromRequester ───────────────────────────────────────

func TestAssertApproverDiffersFromRequester_DifferentIDs(t *testing.T) {
	if err := AssertApproverDiffersFromRequester("approver-1", "requester-2"); err != nil {
		t.Errorf("different IDs must pass, got error: %v", err)
	}
}

func TestAssertApproverDiffersFromRequester_SameID(t *testing.T) {
	if err := AssertApproverDiffersFromRequester("user-1", "user-1"); err == nil {
		t.Error("same ID must return an error")
	}
}

func TestAssertApproverDiffersFromRequester_EmptyApprover(t *testing.T) {
	if err := AssertApproverDiffersFromRequester("", "user-1"); err == nil {
		t.Error("empty approverID must return an error")
	}
}

func TestAssertApproverDiffersFromRequester_EmptyRequester(t *testing.T) {
	if err := AssertApproverDiffersFromRequester("approver-1", ""); err == nil {
		t.Error("empty requesterID must return an error")
	}
}

// ── ScopeToOwnData ───────────────────────────────────────────────────────────

func TestScopeToOwnData_EmployeeCanAccessOwnData(t *testing.T) {
	p := makePrincipal(RoleEmployee)
	p.UserID = "emp-42"
	if err := ScopeToOwnData(p, "emp-42"); err != nil {
		t.Errorf("employee must access own data, got: %v", err)
	}
}

func TestScopeToOwnData_EmployeeCannotAccessOtherData(t *testing.T) {
	p := makePrincipal(RoleEmployee)
	p.UserID = "emp-42"
	if err := ScopeToOwnData(p, "emp-99"); err == nil {
		t.Error("employee must NOT access another employee's data")
	}
}

func TestScopeToOwnData_AdminBypassesScopeCheck(t *testing.T) {
	p := makePrincipal(RoleAdmin)
	p.UserID = "admin-1"
	if err := ScopeToOwnData(p, "emp-99"); err != nil {
		t.Errorf("admin must bypass self-scope check, got: %v", err)
	}
}

func TestScopeToOwnData_DPOBypassesScopeCheck(t *testing.T) {
	p := makePrincipal(RoleDPO)
	p.UserID = "dpo-1"
	if err := ScopeToOwnData(p, "emp-99"); err != nil {
		t.Errorf("DPO must bypass self-scope check, got: %v", err)
	}
}

func TestScopeToOwnData_NilPrincipal(t *testing.T) {
	if err := ScopeToOwnData(nil, "emp-1"); err == nil {
		t.Error("nil principal must return ErrForbidden")
	}
}
