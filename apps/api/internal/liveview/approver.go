// Package liveview — approver constraint enforcement.
// This file is deliberately short; the constraint logic lives in auth.policy.go.
// This file re-exports the check at the liveview service boundary for clarity.
package liveview

import "github.com/personel/api/internal/auth"

// EnforceApproverConstraint validates that the approver sits in the IT
// authority hierarchy (it_manager or admin) AND differs from the
// requester. HR has no live-view authority in the Turkish enterprise
// model — company devices are IT-department property.
func EnforceApproverConstraint(approver *auth.Principal, requesterID string) error {
	if !auth.HasRole(approver, auth.RoleITManager) && !auth.HasRole(approver, auth.RoleAdmin) {
		return auth.ErrForbidden
	}
	return auth.AssertApproverDiffersFromRequester(approver.UserID, requesterID)
}
