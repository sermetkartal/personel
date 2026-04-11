package dsr

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
	return "01J" + item.TenantID, nil
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestEmitRespondEvidenceWithinSLA(t *testing.T) {
	fake := &fakeEvidenceRecorder{}
	svc := &Service{log: silentLogger(), evidenceRecorder: fake}

	// Deadline 10 days in the future ⇒ within SLA.
	req := &Request{
		ID:             "dsr-1",
		TenantID:       "tenant-a",
		EmployeeUserID: "emp-7",
		RequestType:    RequestType("erasure"),
		ScopeJSON:      []byte(`{"categories":["screenshots","keystrokes"]}`),
		CreatedAt:      time.Now().Add(-20 * 24 * time.Hour),
		SLADeadline:    time.Now().Add(10 * 24 * time.Hour),
	}

	svc.emitRespondEvidence(context.Background(), req, "dpo-3", "dsr-artifacts/dsr-1.zip", 42)

	if fake.calls != 1 {
		t.Fatalf("expected 1 Record call, got %d", fake.calls)
	}
	got := fake.lastItem
	if got.Control != evidence.CtrlP7_1 {
		t.Errorf("control mismatch: %q", got.Control)
	}
	if got.Kind != evidence.KindComplianceAttestation {
		t.Errorf("kind mismatch: %q", got.Kind)
	}
	if len(got.AttachmentRefs) != 1 || got.AttachmentRefs[0] != "dsr-artifacts/dsr-1.zip" {
		t.Errorf("attachment refs: %v", got.AttachmentRefs)
	}

	var payload map[string]any
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatalf("invalid payload JSON: %v", err)
	}
	if payload["within_sla"] != true {
		t.Errorf("expected within_sla=true, got %v", payload["within_sla"])
	}
	if payload["dsr_id"] != "dsr-1" {
		t.Errorf("dsr_id: %v", payload["dsr_id"])
	}
	if payload["extended"] != false {
		t.Errorf("extended: %v", payload["extended"])
	}
}

func TestEmitRespondEvidenceOverdue(t *testing.T) {
	// Deadline 1 hour in the past ⇒ overdue. Evidence still emitted —
	// auditors NEED to see the overdue record for CC7.3 incident evidence
	// (it's the trigger for a DPO incident report).
	fake := &fakeEvidenceRecorder{}
	svc := &Service{log: silentLogger(), evidenceRecorder: fake}
	req := &Request{
		ID:             "dsr-late",
		TenantID:       "tenant-a",
		EmployeeUserID: "emp-7",
		RequestType:    RequestType("access"),
		ScopeJSON:      []byte(`{}`),
		CreatedAt:      time.Now().Add(-31 * 24 * time.Hour),
		SLADeadline:    time.Now().Add(-1 * time.Hour),
	}

	svc.emitRespondEvidence(context.Background(), req, "dpo-3", "dsr-artifacts/dsr-late.zip", 99)

	if fake.calls != 1 {
		t.Fatalf("expected 1 Record call, got %d", fake.calls)
	}
	var payload map[string]any
	_ = json.Unmarshal(fake.lastItem.Payload, &payload)
	if payload["within_sla"] != false {
		t.Errorf("expected within_sla=false, got %v", payload["within_sla"])
	}
	if payload["seconds_before_deadline"].(float64) >= 0 {
		t.Errorf("expected negative seconds_before_deadline for overdue DSR, got %v",
			payload["seconds_before_deadline"])
	}
}

func TestEmitRespondEvidenceNilRequest(t *testing.T) {
	// Defensive: auditAndRespond might return a nil request in an error
	// path. Emission must be a silent no-op in that case.
	fake := &fakeEvidenceRecorder{}
	svc := &Service{log: silentLogger(), evidenceRecorder: fake}
	svc.emitRespondEvidence(context.Background(), nil, "dpo", "x", 1)
	if fake.calls != 0 {
		t.Fatalf("expected 0 Record calls for nil request, got %d", fake.calls)
	}
}
