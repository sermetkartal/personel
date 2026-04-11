// Package accessreview — evidence collector for quarterly access reviews.
//
// ADR 0018 + docs/policies/access-review.md mandate quarterly review of
// privileged-role access for the high-risk roles (Admin, DPO, Investigator,
// Legal-Hold, Vault-root, break-glass) plus semi-annual review for the
// rest. This package records the outcome of each completed review as a
// SOC 2 evidence item mapped to CC6.3 (access removal + periodic review).
//
// The review itself is performed out-of-band: the DPO or manager pulls
// the current access list from the console (or HRIS export), walks it
// with the relevant role owner, and submits the outcome via the API.
// This service does not automate the review decision — it records the
// signed outcome so auditors can verify cadence + sign-off evidence.
package accessreview

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/evidence"
)

// ReviewScope identifies which role or access class was reviewed.
type ReviewScope string

const (
	// ScopeAdminRole = all users with role=admin.
	ScopeAdminRole ReviewScope = "admin_role"
	// ScopeDPORole = all users with role=dpo.
	ScopeDPORole ReviewScope = "dpo_role"
	// ScopeInvestigatorRole = all users with role=investigator.
	ScopeInvestigatorRole ReviewScope = "investigator_role"
	// ScopeLegalHoldOwners = users holding active legal holds.
	ScopeLegalHoldOwners ReviewScope = "legal_hold_owners"
	// ScopeVaultRoot = root-level Vault operators.
	ScopeVaultRoot ReviewScope = "vault_root"
	// ScopeBreakGlass = break-glass emergency accounts.
	ScopeBreakGlass ReviewScope = "break_glass"
	// ScopeRegularUsers = ordinary users (semi-annual cadence).
	ScopeRegularUsers ReviewScope = "regular_users"
)

// Decision is the per-user outcome recorded during a review.
type Decision struct {
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	Action    string `json:"action"`     // "retained", "revoked", "reduced"
	Reason    string `json:"reason,omitempty"`
}

// ReviewReport is submitted by the DPO/manager after completing a
// quarterly or semi-annual access review ceremony.
type ReviewReport struct {
	// TenantID scopes the review.
	TenantID string `json:"tenant_id"`

	// Scope identifies the role or access class reviewed.
	Scope ReviewScope `json:"scope"`

	// ReviewerID is the user who performed the review (DPO or manager).
	ReviewerID string `json:"reviewer_id"`

	// SecondReviewerID is optional for dual-control-required scopes
	// (ScopeVaultRoot, ScopeBreakGlass). Empty for single-reviewer scopes.
	SecondReviewerID string `json:"second_reviewer_id,omitempty"`

	// StartedAt / CompletedAt frame the review ceremony duration.
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`

	// Decisions list the per-user outcomes. Length 0 is valid if the
	// scope had no users at review time (record the empty review
	// anyway so auditors see the cadence was honoured).
	Decisions []Decision `json:"decisions"`

	// Notes captures any free-form commentary the reviewer wants to
	// surface (e.g. "manager asked to defer user X review to Q3").
	Notes string `json:"notes,omitempty"`
}

// Service records access review evidence.
type Service struct {
	recorder         *audit.Recorder
	evidenceRecorder evidence.Recorder
	log              *slog.Logger
}

// NewService creates an access review service. evidenceRecorder may be nil
// in scaffold mode; the service then only writes the audit entry.
func NewService(rec *audit.Recorder, er evidence.Recorder, log *slog.Logger) *Service {
	return &Service{recorder: rec, evidenceRecorder: er, log: log}
}

// RecordReview validates and persists an access review outcome. Returns
// the evidence item ID (or empty in scaffold mode) and any validation or
// audit error. Evidence emission errors are swallowed + logged.
func (s *Service) RecordReview(ctx context.Context, r ReviewReport) (string, error) {
	if r.TenantID == "" || r.ReviewerID == "" {
		return "", fmt.Errorf("accessreview: tenant_id and reviewer_id are required")
	}
	if r.Scope == "" {
		return "", fmt.Errorf("accessreview: scope is required")
	}
	if r.StartedAt.IsZero() || r.CompletedAt.IsZero() {
		return "", fmt.Errorf("accessreview: started_at and completed_at are required")
	}
	if r.CompletedAt.Before(r.StartedAt) {
		return "", fmt.Errorf("accessreview: completed_at must be >= started_at")
	}
	// Dual-control scopes require a second reviewer distinct from the
	// primary. This matches docs/policies/access-review.md §3.
	if requiresDualControl(r.Scope) {
		if r.SecondReviewerID == "" {
			return "", fmt.Errorf("accessreview: scope %q requires second_reviewer_id", r.Scope)
		}
		if r.SecondReviewerID == r.ReviewerID {
			return "", fmt.Errorf("accessreview: second_reviewer_id must differ from reviewer_id")
		}
	}

	auditID, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    r.ReviewerID,
		TenantID: r.TenantID,
		Action:   audit.ActionAccessReviewCompleted,
		Target:   fmt.Sprintf("scope:%s", r.Scope),
		Details: map[string]any{
			"scope":              string(r.Scope),
			"second_reviewer_id": r.SecondReviewerID,
			"decision_count":     len(r.Decisions),
		},
	})
	if err != nil {
		return "", fmt.Errorf("accessreview: audit: %w", err)
	}

	if s.evidenceRecorder == nil {
		return "", nil
	}

	retained, revoked, reduced := tallyDecisions(r.Decisions)

	payload, err := json.Marshal(map[string]any{
		"scope":              string(r.Scope),
		"reviewer_id":        r.ReviewerID,
		"second_reviewer_id": r.SecondReviewerID,
		"started_at":         r.StartedAt.Format(time.RFC3339Nano),
		"completed_at":       r.CompletedAt.Format(time.RFC3339Nano),
		"duration_seconds":   int64(r.CompletedAt.Sub(r.StartedAt).Seconds()),
		"decision_count":     len(r.Decisions),
		"retained":           retained,
		"revoked":            revoked,
		"reduced":            reduced,
		"decisions":          r.Decisions,
		"notes":              r.Notes,
	})
	if err != nil {
		s.log.ErrorContext(ctx, "accessreview: evidence payload marshal failed",
			slog.String("error", err.Error()))
		return "", nil
	}

	item := evidence.Item{
		TenantID:   r.TenantID,
		Control:    evidence.CtrlCC6_3,
		Kind:       evidence.KindAccessReview,
		RecordedAt: r.CompletedAt,
		Actor:      r.ReviewerID,
		SummaryTR: fmt.Sprintf(
			"Erişim gözden geçirmesi tamamlandı — kapsam %s, %d karar (%d iptal)",
			r.Scope, len(r.Decisions), revoked,
		),
		SummaryEN: fmt.Sprintf(
			"Access review completed — scope=%s decisions=%d revoked=%d",
			r.Scope, len(r.Decisions), revoked,
		),
		Payload:            payload,
		ReferencedAuditIDs: []int64{auditID},
	}

	id, err := s.evidenceRecorder.Record(ctx, item)
	if err != nil {
		s.log.ErrorContext(ctx, "accessreview: SOC 2 evidence emission failed",
			slog.String("scope", string(r.Scope)),
			slog.String("error", err.Error()))
		return "", nil
	}
	return id, nil
}

// requiresDualControl returns true for scopes where the policy demands
// two distinct reviewers. Hardcoded here so a single source of truth
// exists in code; matches docs/policies/access-review.md.
func requiresDualControl(s ReviewScope) bool {
	switch s {
	case ScopeVaultRoot, ScopeBreakGlass:
		return true
	}
	return false
}

func tallyDecisions(ds []Decision) (retained, revoked, reduced int) {
	for _, d := range ds {
		switch d.Action {
		case "retained":
			retained++
		case "revoked":
			revoked++
		case "reduced":
			reduced++
		}
	}
	return
}
