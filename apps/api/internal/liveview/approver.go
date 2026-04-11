// Package liveview — approver constraint enforcement.
// This file is deliberately short; the constraint logic lives in auth.policy.go.
// This file re-exports the check at the liveview service boundary for clarity.
package liveview

import "github.com/personel/api/internal/auth"

// EnforceApproverConstraint validates that the approver is HR and differs from the requester.
// Returns a descriptive error if either constraint is violated.
func EnforceApproverConstraint(approver *auth.Principal, requesterID string) error {
	if !auth.HasRole(approver, auth.RoleHR) {
		return auth.ErrForbidden
	}
	return auth.AssertApproverDiffersFromRequester(approver.UserID, requesterID)
}
