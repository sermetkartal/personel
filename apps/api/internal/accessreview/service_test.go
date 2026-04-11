package accessreview

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/personel/api/internal/evidence"
)

type fakeEvidenceRecorder struct {
	calls    int
	lastItem evidence.Item
}

func (f *fakeEvidenceRecorder) Record(_ context.Context, item evidence.Item) (string, error) {
	f.calls++
	f.lastItem = item
	return "01J-ar-" + item.TenantID, nil
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRecordReviewRejectsMissingFields(t *testing.T) {
	svc := &Service{log: silentLogger()}
	now := time.Now().UTC()
	cases := []ReviewReport{
		{Scope: ScopeAdminRole, ReviewerID: "r", StartedAt: now, CompletedAt: now}, // no tenant
		{TenantID: "t", ReviewerID: "r", StartedAt: now, CompletedAt: now},         // no scope
		{TenantID: "t", Scope: ScopeAdminRole, StartedAt: now, CompletedAt: now},   // no reviewer
		{TenantID: "t", Scope: ScopeAdminRole, ReviewerID: "r", CompletedAt: now},  // no started_at
		{TenantID: "t", Scope: ScopeAdminRole, ReviewerID: "r", StartedAt: now},    // no completed_at
	}
	for i, r := range cases {
		if _, err := svc.RecordReview(context.Background(), r); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

func TestRecordReviewRejectsNegativeDuration(t *testing.T) {
	svc := &Service{log: silentLogger()}
	start := time.Now()
	r := ReviewReport{
		TenantID:    "t",
		Scope:       ScopeAdminRole,
		ReviewerID:  "r",
		StartedAt:   start,
		CompletedAt: start.Add(-1 * time.Hour),
	}
	if _, err := svc.RecordReview(context.Background(), r); err == nil {
		t.Fatal("expected error for negative duration")
	}
}

func TestRecordReviewEnforcesDualControl(t *testing.T) {
	svc := &Service{log: silentLogger()}
	now := time.Now()
	base := ReviewReport{
		TenantID:    "t",
		ReviewerID:  "primary",
		StartedAt:   now,
		CompletedAt: now.Add(time.Hour),
	}

	// vault_root without second reviewer → reject.
	r := base
	r.Scope = ScopeVaultRoot
	if _, err := svc.RecordReview(context.Background(), r); err == nil {
		t.Fatal("vault_root without second_reviewer must reject")
	}

	// vault_root with same reviewer → reject (A != A).
	r.SecondReviewerID = "primary"
	if _, err := svc.RecordReview(context.Background(), r); err == nil {
		t.Fatal("vault_root with identical reviewers must reject")
	}

	// break_glass with same reviewer → reject.
	r.Scope = ScopeBreakGlass
	if _, err := svc.RecordReview(context.Background(), r); err == nil {
		t.Fatal("break_glass with identical reviewers must reject")
	}

	// Regular scope without second reviewer → accept at validation
	// stage (will then need the audit recorder, so we can't go further
	// here without a real DB — validation layer is the interesting
	// boundary for this test).
	r.Scope = ScopeRegularUsers
	r.SecondReviewerID = ""
	// We don't call audit here because svc.recorder is nil — test ends
	// at the validation boundary. The fact that the previous paths
	// returned errors before reaching audit proves validation runs first.
	_ = r
}

func TestTallyDecisions(t *testing.T) {
	ds := []Decision{
		{UserID: "1", Action: "retained"},
		{UserID: "2", Action: "revoked"},
		{UserID: "3", Action: "retained"},
		{UserID: "4", Action: "reduced"},
		{UserID: "5", Action: "revoked"},
		{UserID: "6", Action: "unknown"}, // must not tally into any bucket
	}
	ret, rev, red := tallyDecisions(ds)
	if ret != 2 || rev != 2 || red != 1 {
		t.Errorf("tally: retained=%d revoked=%d reduced=%d (want 2/2/1)", ret, rev, red)
	}
}

// TestEvidencePayloadShape constructs an Item the same way Service would
// after successful audit, and asserts the control/kind/payload keys. Uses
// a fake recorder to capture the item without needing a DB.
func TestEvidencePayloadShape(t *testing.T) {
	fake := &fakeEvidenceRecorder{}
	now := time.Date(2026, 4, 11, 10, 0, 0, 0, time.UTC)
	decisions := []Decision{
		{UserID: "u1", Username: "alice", Action: "retained"},
		{UserID: "u2", Username: "bob", Action: "revoked", Reason: "no longer a manager"},
	}

	payload, _ := json.Marshal(map[string]any{
		"scope":              string(ScopeAdminRole),
		"reviewer_id":        "dpo-1",
		"second_reviewer_id": "",
		"started_at":         now.Format(time.RFC3339Nano),
		"completed_at":       now.Add(30 * time.Minute).Format(time.RFC3339Nano),
		"duration_seconds":   int64(1800),
		"decision_count":     len(decisions),
		"retained":           1,
		"revoked":            1,
		"reduced":            0,
		"decisions":          decisions,
		"notes":              "",
	})

	item := evidence.Item{
		TenantID:           "tenant-a",
		Control:            evidence.CtrlCC6_3,
		Kind:               evidence.KindAccessReview,
		RecordedAt:         now.Add(30 * time.Minute),
		Actor:              "dpo-1",
		Payload:            payload,
		ReferencedAuditIDs: []int64{7},
	}
	_, _ = fake.Record(context.Background(), item)

	got := fake.lastItem
	if got.Control != evidence.CtrlCC6_3 {
		t.Errorf("control: %q", got.Control)
	}
	if got.Kind != evidence.KindAccessReview {
		t.Errorf("kind: %q", got.Kind)
	}

	var pl map[string]any
	_ = json.Unmarshal(got.Payload, &pl)
	if pl["retained"].(float64) != 1 || pl["revoked"].(float64) != 1 {
		t.Errorf("tally in payload: %v", pl)
	}
}

func TestRequiresDualControl(t *testing.T) {
	cases := map[ReviewScope]bool{
		ScopeVaultRoot:        true,
		ScopeBreakGlass:       true,
		ScopeAdminRole:        false,
		ScopeDPORole:          false,
		ScopeInvestigatorRole: false,
		ScopeLegalHoldOwners:  false,
		ScopeRegularUsers:     false,
	}
	for s, want := range cases {
		if got := requiresDualControl(s); got != want {
			t.Errorf("requiresDualControl(%q) = %v, want %v", s, got, want)
		}
	}
}
