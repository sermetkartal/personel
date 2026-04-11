// Package playback — dual-control approval gate for playback.
//
// Per ADR 0019 §Playback flow: playback requires the same dual-control state
// machine as session initiation. This gate calls the Admin API's internal
// endpoint to verify that a playback request for the recording has been
// approved by an HR approver (different user, HR role).
//
// The 30-minute approval validity window is enforced by the Admin API;
// livrec-service trusts the Admin API's response and records the check in audit.
package playback

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// ApprovalStatus is the response from the Admin API approval check.
type ApprovalStatus struct {
	Approved    bool      `json:"approved"`
	ApproverID  string    `json:"approver_id"`
	RequesterID string    `json:"requester_id"`
	ApprovedAt  time.Time `json:"approved_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// ApprovalGate verifies dual-control playback approval against the Admin API.
type ApprovalGate struct {
	adminBaseURL string
	internalToken string
	client       *http.Client
	log          *slog.Logger
}

// NewApprovalGate constructs an ApprovalGate that calls adminBaseURL for
// approval verification using internalToken as a Bearer credential.
func NewApprovalGate(adminBaseURL, internalToken string, log *slog.Logger) *ApprovalGate {
	return &ApprovalGate{
		adminBaseURL:  adminBaseURL,
		internalToken: internalToken,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		log: log,
	}
}

// CheckPlaybackApproval verifies that recordingID has an active, non-expired
// playback approval for requesterID at the Admin API.
//
// Returns nil if the approval is valid and non-expired.
// Returns an error if approval is missing, expired, or the Admin API is
// unreachable (fail-closed: no approval = no playback).
func (g *ApprovalGate) CheckPlaybackApproval(ctx context.Context, recordingID, requesterID string) (*ApprovalStatus, error) {
	url := fmt.Sprintf("%s/v1/internal/livrec/playback-approval?recording_id=%s&requester_id=%s",
		g.adminBaseURL, recordingID, requesterID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("approval_gate: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+g.internalToken)
	req.Header.Set("Accept", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("approval_gate: admin api call failed (fail-closed): %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("approval_gate: no playback approval found for recording %s", recordingID)
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("approval_gate: playback approval denied or expired for recording %s", recordingID)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("approval_gate: unexpected status %d from admin api", resp.StatusCode)
	}

	var status ApprovalStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("approval_gate: decode response: %w", err)
	}

	if !status.Approved {
		return nil, fmt.Errorf("approval_gate: approval is not in approved state for recording %s", recordingID)
	}
	if time.Now().After(status.ExpiresAt) {
		return nil, fmt.Errorf("approval_gate: approval expired at %s for recording %s",
			status.ExpiresAt.Format(time.RFC3339), recordingID)
	}

	// Enforce approver != requester at the livrec boundary as a defence-in-depth
	// check (the Admin API is the authoritative enforcer; this is belt-and-suspenders).
	if status.ApproverID == status.RequesterID {
		g.log.Error("dual-control invariant violated: approver equals requester",
			slog.String("recording_id", recordingID),
			slog.String("approver_id", status.ApproverID),
		)
		return nil, fmt.Errorf("approval_gate: approver must differ from requester (invariant violation)")
	}

	return &status, nil
}
