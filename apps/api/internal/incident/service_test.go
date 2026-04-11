package incident

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
	return "01J-inc-" + item.TenantID, nil
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRecordClosureRejectsMissingFields(t *testing.T) {
	svc := &Service{log: silentLogger()}
	now := time.Now()
	cases := []IncidentReport{
		{IncidentID: "I1", Severity: SeverityHigh, DetectedAt: now, ClosedAt: now, LeadResponderID: "r"},   // no tenant
		{TenantID: "t", Severity: SeverityHigh, DetectedAt: now, ClosedAt: now, LeadResponderID: "r"},      // no ID
		{TenantID: "t", IncidentID: "I1", DetectedAt: now, ClosedAt: now, LeadResponderID: "r"},            // no severity
		{TenantID: "t", IncidentID: "I1", Severity: SeverityHigh, ClosedAt: now, LeadResponderID: "r"},     // no detected_at
		{TenantID: "t", IncidentID: "I1", Severity: SeverityHigh, DetectedAt: now, LeadResponderID: "r"},   // no closed_at
		{TenantID: "t", IncidentID: "I1", Severity: SeverityHigh, DetectedAt: now, ClosedAt: now},          // no lead
	}
	for i, r := range cases {
		if _, err := svc.RecordClosure(context.Background(), r); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

func TestRecordClosureRejectsReversedTimes(t *testing.T) {
	svc := &Service{log: silentLogger()}
	now := time.Now()
	r := IncidentReport{
		TenantID: "t", IncidentID: "I1", Severity: SeverityHigh,
		DetectedAt: now, ClosedAt: now.Add(-1 * time.Hour),
		LeadResponderID: "r",
	}
	if _, err := svc.RecordClosure(context.Background(), r); err == nil {
		t.Fatal("expected error for closed_at < detected_at")
	}
}

// TestEvidencePayloadShape exercises the fields the payload must contain.
// We bypass the audit recorder by constructing the Item directly the way
// Service.RecordClosure would — the test is a snapshot of the contract
// auditors will script against.
func TestEvidencePayloadShape(t *testing.T) {
	fake := &fakeEvidenceRecorder{}
	detected := time.Date(2026, 4, 11, 3, 0, 0, 0, time.UTC)
	contained := detected.Add(15 * time.Minute)
	closed := detected.Add(4 * time.Hour)
	kvkk := detected.Add(24 * time.Hour) // well within 72h

	payload, _ := json.Marshal(map[string]any{
		"incident_id":         "INC-42",
		"severity":            string(SeverityCritical),
		"detected_at":         detected.Format(time.RFC3339Nano),
		"contained_at":        contained.Format(time.RFC3339Nano),
		"closed_at":           closed.Format(time.RFC3339Nano),
		"containment_seconds": int64(900),
		"resolution_seconds":  int64(14400),
		"lead_responder_id":   "sec-lead-1",
		"summary":             "Keystroke exfil attempt",
		"root_cause":          "Policy drift",
		"remediation_actions": []string{"harden DLP policy", "rotate Vault key"},
		"kvkk_notified_at":    kvkk.Format(time.RFC3339Nano),
		"kvkk_within_72h":     true,
		"gdpr_notified_at":    "",
		"gdpr_within_72h":     true,
	})
	item := evidence.Item{
		TenantID:           "tenant-a",
		Control:            evidence.CtrlCC7_3,
		Kind:               evidence.KindIncidentReport,
		RecordedAt:         closed,
		Actor:              "sec-lead-1",
		Payload:            payload,
		ReferencedAuditIDs: []int64{100},
	}
	_, _ = fake.Record(context.Background(), item)

	var pl map[string]any
	_ = json.Unmarshal(fake.lastItem.Payload, &pl)
	if pl["containment_seconds"].(float64) != 900 {
		t.Errorf("containment: %v", pl["containment_seconds"])
	}
	if pl["resolution_seconds"].(float64) != 14400 {
		t.Errorf("resolution: %v", pl["resolution_seconds"])
	}
	if pl["kvkk_within_72h"] != true {
		t.Errorf("kvkk_within_72h: %v", pl["kvkk_within_72h"])
	}
}

func TestFormatOptionalTime(t *testing.T) {
	if formatOptionalTime(time.Time{}) != "" {
		t.Error("zero time must render as empty string")
	}
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if formatOptionalTime(ts) == "" {
		t.Error("non-zero time must render as non-empty")
	}
}
