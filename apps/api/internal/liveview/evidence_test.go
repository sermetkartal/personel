package liveview

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/personel/api/internal/evidence"
)

// fakeEvidenceRecorder captures the last Record call for assertions.
type fakeEvidenceRecorder struct {
	lastItem evidence.Item
	calls    int
	err      error
}

func (f *fakeEvidenceRecorder) Record(_ context.Context, item evidence.Item) (string, error) {
	f.calls++
	f.lastItem = item
	if f.err != nil {
		return "", f.err
	}
	return "01J" + item.TenantID, nil
}

// silentLogger returns a slog.Logger that discards output; used in unit tests
// that would otherwise spam the test runner.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestEmitSessionEvidenceHappyPath(t *testing.T) {
	fake := &fakeEvidenceRecorder{}
	svc := &Service{
		log:              silentLogger(),
		evidenceRecorder: fake,
	}

	approver := "hr-user-42"
	started := time.Date(2026, 4, 11, 10, 0, 0, 0, time.UTC)
	ended := started.Add(12 * time.Minute)

	sess := &Session{
		ID:                "sess-abc",
		TenantID:          "tenant-a",
		EndpointID:        "ep-17",
		RequesterID:       "admin-user-7",
		ApproverID:        &approver,
		ReasonCode:        "data_loss_investigation",
		Justification:     "KVKK m.6 olay inceleme",
		RequestedDuration: 15 * time.Minute,
		StartedAt:         &started,
	}

	svc.emitSessionEvidence(context.Background(), sess, StateEnded, "end", 99, ended)

	if fake.calls != 1 {
		t.Fatalf("expected 1 Record call, got %d", fake.calls)
	}

	got := fake.lastItem
	if got.TenantID != "tenant-a" {
		t.Errorf("tenant_id mismatch: %q", got.TenantID)
	}
	if got.Control != evidence.CtrlCC6_1 {
		t.Errorf("control mismatch: %q", got.Control)
	}
	if got.Kind != evidence.KindPrivilegedAccessSession {
		t.Errorf("kind mismatch: %q", got.Kind)
	}
	if len(got.ReferencedAuditIDs) != 1 || got.ReferencedAuditIDs[0] != 99 {
		t.Errorf("referenced_audit_ids should cite terminate audit ID 99, got %v", got.ReferencedAuditIDs)
	}

	var payload map[string]any
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	if payload["session_id"] != "sess-abc" {
		t.Errorf("payload session_id: %v", payload["session_id"])
	}
	// Duration must reflect real elapsed time (12 min = 720s), not the
	// requested cap (15 min = 900s). Auditors care about the actual
	// ceremony duration for CC6.3 evidence of timely access removal.
	if payload["actual_seconds"].(float64) != 720 {
		t.Errorf("expected actual_seconds=720, got %v", payload["actual_seconds"])
	}
	if payload["requested_seconds"].(float64) != 900 {
		t.Errorf("expected requested_seconds=900, got %v", payload["requested_seconds"])
	}
}

func TestEmitSessionEvidenceSkipsWhenNoRecorder(t *testing.T) {
	// Scaffold mode: no recorder wired ⇒ method must be a silent no-op.
	svc := &Service{log: silentLogger()}
	approver := "hr-42"
	sess := &Session{
		ID:          "s",
		TenantID:    "t",
		ApproverID:  &approver,
		RequesterID: "r",
	}
	svc.emitSessionEvidence(context.Background(), sess, StateEnded, "end", 1, time.Now())
	// No panic = success.
}

func TestEmitSessionEvidenceSkipsWithoutApprover(t *testing.T) {
	// Defence-in-depth: if we somehow reach the evidence path without an
	// approver, we must not emit a malformed item. The warning log is
	// sufficient signal.
	fake := &fakeEvidenceRecorder{}
	svc := &Service{log: silentLogger(), evidenceRecorder: fake}
	sess := &Session{ID: "s", TenantID: "t", RequesterID: "r"} // ApproverID nil
	svc.emitSessionEvidence(context.Background(), sess, StateEnded, "end", 1, time.Now())
	if fake.calls != 0 {
		t.Fatalf("expected Record to be skipped, got %d calls", fake.calls)
	}
}

func TestEmitSessionEvidenceSwallowsRecorderError(t *testing.T) {
	// Evidence emission is best-effort — an error from the Recorder must
	// not propagate up to terminateSession's caller. The caller already
	// terminated the session successfully.
	fake := &fakeEvidenceRecorder{err: errors.New("vault offline")}
	svc := &Service{log: silentLogger(), evidenceRecorder: fake}
	approver := "hr-42"
	started := time.Now().Add(-5 * time.Minute)
	sess := &Session{
		ID:          "s",
		TenantID:    "t",
		ApproverID:  &approver,
		RequesterID: "r",
		StartedAt:   &started,
	}
	// Must not panic, must not return (void method), must log internally.
	svc.emitSessionEvidence(context.Background(), sess, StateEnded, "end", 1, time.Now())
	if fake.calls != 1 {
		t.Fatalf("expected 1 failed Record attempt, got %d", fake.calls)
	}
}
