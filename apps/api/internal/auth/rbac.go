// Package auth — role definitions and RBAC decision engine.
//
// Role constants match the Keycloak realm roles. The Can() function is the
// single authoritative gate for all permission decisions in the Admin API.
package auth

import "strings"

// Role represents a Keycloak realm role.
type Role string

const (
	RoleAdmin       Role = "admin"
	RoleManager     Role = "manager"
	RoleHR          Role = "hr"
	RoleDPO         Role = "dpo"
	RoleInvestigator Role = "investigator"
	RoleAuditor     Role = "auditor"
	RoleEmployee    Role = "employee"
)

// parseRole maps a raw string from Keycloak to a typed Role.
func parseRole(s string) (Role, bool) {
	switch strings.ToLower(s) {
	case string(RoleAdmin):
		return RoleAdmin, true
	case string(RoleManager):
		return RoleManager, true
	case string(RoleHR):
		return RoleHR, true
	case string(RoleDPO):
		return RoleDPO, true
	case string(RoleInvestigator):
		return RoleInvestigator, true
	case string(RoleAuditor):
		return RoleAuditor, true
	case string(RoleEmployee):
		return RoleEmployee, true
	default:
		return "", false
	}
}

// Resource constants used in Can() decisions.
type Resource string

const (
	ResourceUser        Resource = "user"
	ResourceEndpoint    Resource = "endpoint"
	ResourcePolicy      Resource = "policy"
	ResourceScreenshot  Resource = "screenshot"
	ResourceScreenclip  Resource = "screenclip"
	ResourceLiveView    Resource = "live_view"
	ResourceReport      Resource = "report"
	ResourceAuditLog    Resource = "audit_log"
	ResourceDSR         Resource = "dsr"
	ResourceLegalHold   Resource = "legal_hold"
	ResourceDestruction Resource = "destruction_report"
	ResourceEmployee    Resource = "employee"
	ResourceDLPMatch    Resource = "dlp_match"
	ResourceMyData      Resource = "my_data" // employee self-service
	ResourceSilence     Resource = "silence"
)

// Op constants for permission checks.
type Op string

const (
	OpRead    Op = "read"
	OpWrite   Op = "write"
	OpDelete  Op = "delete"
	OpApprove Op = "approve"
	OpReject  Op = "reject"
	OpRequest Op = "request"
	OpTerminate Op = "terminate"
	OpDownload Op = "download"
)

// Can returns true if any of the principal's roles grants (role, op, resource).
// This is the sole entry-point for RBAC decisions; no handler may
// hardcode role comparisons outside this function.
//
// Critical security rules encoded here:
//  1. No role except Investigator and DPO may view raw screenshots or screenclips.
//  2. No role may read keystroke content — the API has no path to it by design.
//  3. Legal hold placement/release is DPO-only.
//  4. Destruction report download is DPO-only.
//  5. Live view approval requires HR role (enforced here AND in live view service).
func Can(p *Principal, op Op, resource Resource) bool {
	if p == nil {
		return false
	}
	for _, r := range p.Roles {
		if can(r, op, resource) {
			return true
		}
	}
	return false
}

// HasRole returns true if the principal holds the given role.
func HasRole(p *Principal, role Role) bool {
	if p == nil {
		return false
	}
	for _, r := range p.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// can is the internal per-role matrix.
// Explicitly deny > implicit deny > explicit allow.
//
//nolint:cyclop
func can(role Role, op Op, resource Resource) bool {
	// ------------------------------------------------------------------
	// HARD DENIALS (no role can do these through the Admin API)
	// These are documented in key-hierarchy.md and enforced cryptographically,
	// but also blocked at the RBAC layer for defence-in-depth.
	// ------------------------------------------------------------------

	// Keystroke content: API has zero path to it; no role can "read" it.
	// (This constant doesn't exist in Resource list intentionally — adding it
	// is a security regression. CI linter checks for it.)

	// Screenshots/screenclips: only Investigator and DPO may read.
	if (resource == ResourceScreenshot || resource == ResourceScreenclip) && op == OpRead {
		return role == RoleInvestigator || role == RoleDPO
	}

	// Legal hold: place/release — DPO only.
	if resource == ResourceLegalHold && (op == OpWrite || op == OpDelete) {
		return role == RoleDPO
	}

	// Destruction report download — DPO only.
	if resource == ResourceDestruction && op == OpDownload {
		return role == RoleDPO
	}

	// Live view approval — HR only.
	if resource == ResourceLiveView && (op == OpApprove || op == OpReject) {
		return role == RoleHR
	}

	// Live view termination — HR or DPO.
	if resource == ResourceLiveView && op == OpTerminate {
		return role == RoleHR || role == RoleDPO
	}

	// DSR respond/assign — DPO only.
	if resource == ResourceDSR && (op == OpApprove || op == OpWrite) {
		return role == RoleDPO
	}

	// Audit log — read: Auditor, DPO. No one can write directly.
	if resource == ResourceAuditLog {
		if op == OpRead {
			return role == RoleAuditor || role == RoleDPO
		}
		return false
	}

	// ------------------------------------------------------------------
	// Role grants
	// ------------------------------------------------------------------
	switch role {
	case RoleAdmin:
		switch resource {
		case ResourceUser, ResourceEndpoint, ResourcePolicy:
			return true
		case ResourceReport:
			return op == OpRead
		case ResourceLiveView:
			return op == OpRequest || op == OpRead
		case ResourceDLPMatch:
			return op == OpRead
		case ResourceSilence:
			return op == OpRead
		case ResourceEmployee:
			return op == OpRead || op == OpWrite
		}

	case RoleManager:
		switch resource {
		case ResourceReport:
			return op == OpRead
		case ResourceLiveView:
			return op == OpRequest || op == OpRead
		case ResourceEmployee:
			return op == OpRead
		case ResourceSilence:
			return op == OpRead
		}

	case RoleHR:
		switch resource {
		case ResourceLiveView:
			// approve/reject are handled above; HR can also read
			return op == OpRead
		case ResourceEmployee:
			return op == OpRead || op == OpWrite
		case ResourceReport:
			return op == OpRead
		}

	case RoleDPO:
		// DPO can read almost everything.
		switch resource {
		case ResourceUser, ResourceEndpoint, ResourcePolicy,
			ResourceReport, ResourceEmployee, ResourceDLPMatch,
			ResourceLiveView, ResourceDSR, ResourceLegalHold,
			ResourceDestruction, ResourceSilence, ResourceScreenshot,
			ResourceScreenclip:
			return true
		}

	case RoleInvestigator:
		switch resource {
		case ResourceScreenshot, ResourceScreenclip:
			return op == OpRead || op == OpDownload
		case ResourceReport:
			return op == OpRead
		case ResourceEmployee:
			return op == OpRead
		case ResourceLiveView:
			return op == OpRequest || op == OpRead
		}

	case RoleAuditor:
		switch resource {
		case ResourceAuditLog:
			return op == OpRead
		case ResourceReport:
			return op == OpRead
		case ResourceEmployee:
			return op == OpRead
		}

	case RoleEmployee:
		// Employees only access their own data via transparency endpoints.
		if resource == ResourceMyData {
			return true
		}
		// Employees can submit DSRs.
		if resource == ResourceDSR && op == OpRequest {
			return true
		}
	}

	return false
}
