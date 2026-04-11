package policy

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/personel/api/internal/auth"
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

func TestEmitPushEvidenceHappyPath(t *testing.T) {
	fake := &fakeEvidenceRecorder{}
	svc := &Service{log: silentLogger(), evidenceRecorder: fake}

	pol := &Policy{
		ID:       "pol-123",
		TenantID: "tenant-a",
		Name:     "default-strict",
		Rules:    json.RawMessage(`{"dlp":{"enabled":false}}`),
		Version:  7,
	}
	p := &auth.Principal{UserID: "admin-9", TenantID: "tenant-a"}

	svc.emitPushEvidence(context.Background(), p, pol, "ep-42", 555)

	if fake.calls != 1 {
		t.Fatalf("expected 1 Record call, got %d", fake.calls)
	}
	got := fake.lastItem
	if got.Control != evidence.CtrlCC8_1 {
		t.Errorf("control mismatch: %q", got.Control)
	}
	if got.Kind != evidence.KindChangeAuthorization {
		t.Errorf("kind mismatch: %q", got.Kind)
	}
	if len(got.ReferencedAuditIDs) != 1 || got.ReferencedAuditIDs[0] != 555 {
		t.Errorf("expected referenced audit ID 555, got %v", got.ReferencedAuditIDs)
	}

	var payload map[string]any
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatalf("invalid JSON payload: %v", err)
	}
	if payload["policy_id"] != "pol-123" {
		t.Errorf("payload policy_id: %v", payload["policy_id"])
	}
	if payload["target_endpoint"] != "ep-42" {
		t.Errorf("payload target_endpoint: %v", payload["target_endpoint"])
	}
	if payload["policy_version"].(float64) != 7 {
		t.Errorf("payload policy_version: %v", payload["policy_version"])
	}
	// The full rules JSON must be embedded so auditors can verify the
	// exact config that was deployed at this point in time.
	rules, ok := payload["rules"].(map[string]any)
	if !ok {
		t.Fatalf("rules not a nested object: %v", payload["rules"])
	}
	if _, ok := rules["dlp"]; !ok {
		t.Errorf("rules.dlp missing from evidence payload")
	}
}

func TestEmitPushEvidenceBroadcastTarget(t *testing.T) {
	// Empty endpointID → broadcast push → target_endpoint recorded as "*".
	fake := &fakeEvidenceRecorder{}
	svc := &Service{log: silentLogger(), evidenceRecorder: fake}
	pol := &Policy{ID: "p", TenantID: "t", Name: "n", Rules: json.RawMessage(`{}`), Version: 1}
	p := &auth.Principal{UserID: "admin", TenantID: "t"}
	svc.emitPushEvidence(context.Background(), p, pol, "", 1)

	var payload map[string]any
	_ = json.Unmarshal(fake.lastItem.Payload, &payload)
	if payload["target_endpoint"] != "*" {
		t.Errorf("expected target_endpoint=*, got %v", payload["target_endpoint"])
	}
}

func TestEmitPushEvidenceNoRecorderIsNoop(t *testing.T) {
	// Scaffold mode — no recorder wired, must not panic.
	svc := &Service{log: silentLogger()}
	pol := &Policy{ID: "p", TenantID: "t", Name: "n", Rules: json.RawMessage(`{}`), Version: 1}
	p := &auth.Principal{UserID: "admin", TenantID: "t"}
	svc.emitPushEvidence(context.Background(), p, pol, "ep", 1)
}
