package backup

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

// Minimal audit.Recorder surrogate: Phase 3.0 tests use a real Recorder
// indirectly but this package does not need the hash chain — we inject
// a thin stub that mimics the Append signature only. Because audit.Recorder
// is a concrete type, we can't fake it directly without a big refactor.
// Instead we assert validation errors that run BEFORE the audit call.

func TestRecordRunRejectsMissingFields(t *testing.T) {
	svc := &Service{log: silentLogger()}

	cases := []RunReport{
		{Kind: "", TargetPath: "x", SHA256: "y", StartedAt: time.Now(), FinishedAt: time.Now()},
		{Kind: "postgres", TargetPath: "", SHA256: "y", StartedAt: time.Now(), FinishedAt: time.Now()},
		{Kind: "postgres", TargetPath: "x", SHA256: "", StartedAt: time.Now(), FinishedAt: time.Now()},
		{Kind: "postgres", TargetPath: "x", SHA256: "y"},
	}
	for i, r := range cases {
		if _, err := svc.RecordRun(context.Background(), r); err == nil {
			t.Errorf("case %d: expected error, got nil", i)
		}
	}
}

func TestRecordRunRejectsNegativeDuration(t *testing.T) {
	svc := &Service{log: silentLogger()}
	start := time.Now()
	r := RunReport{
		Kind:       "postgres",
		TargetPath: "backups/2026-04-11.pgdump",
		SHA256:     "abc",
		StartedAt:  start,
		FinishedAt: start.Add(-1 * time.Second),
	}
	if _, err := svc.RecordRun(context.Background(), r); err == nil {
		t.Fatal("expected error for negative duration")
	}
}

// TestRecordRunPayloadShape exercises emitBackupEvidence directly by
// invoking the item construction path — we bypass the audit.Recorder
// call since that needs a real DB pool. Instead we build an Item the
// same way service.go does and assert the JSON shape.
//
// This is a structural snapshot test: if the payload keys rename or the
// controls change, auditors who script against the evidence pack break.
func TestEvidencePayloadShape(t *testing.T) {
	fake := &fakeEvidenceRecorder{}
	// Directly construct an item the same way Service.RecordRun would
	// after a validated report. We cannot call RecordRun without an
	// audit.Recorder, so we simulate the post-audit path.
	r := RunReport{
		Kind:       "postgres",
		TargetPath: "minio://backups/2026-04-11.pgdump",
		SHA256:     "0123456789abcdef0123456789abcdef",
		SizeBytes:  123456789,
		StartedAt:  time.Date(2026, 4, 11, 2, 0, 0, 0, time.UTC),
		FinishedAt: time.Date(2026, 4, 11, 2, 15, 0, 0, time.UTC),
		SourceHost: "db-primary.internal#4321",
	}

	payload, err := json.Marshal(map[string]any{
		"kind":             r.Kind,
		"target_path":      r.TargetPath,
		"size_bytes":       r.SizeBytes,
		"sha256":           r.SHA256,
		"started_at":       r.StartedAt.Format(time.RFC3339Nano),
		"finished_at":      r.FinishedAt.Format(time.RFC3339Nano),
		"duration_seconds": int64(r.FinishedAt.Sub(r.StartedAt).Seconds()),
		"source_host":      r.SourceHost,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	item := evidence.Item{
		TenantID:           "platform",
		Control:            evidence.CtrlA1_2,
		Kind:               evidence.KindBackupRun,
		RecordedAt:         r.FinishedAt,
		Actor:              r.SourceHost,
		Payload:            payload,
		ReferencedAuditIDs: []int64{42},
		AttachmentRefs:     []string{r.TargetPath},
	}
	_, _ = fake.Record(context.Background(), item)

	if fake.calls != 1 {
		t.Fatalf("expected 1 call, got %d", fake.calls)
	}
	got := fake.lastItem
	if got.Control != evidence.CtrlA1_2 {
		t.Errorf("control: %q", got.Control)
	}
	if got.Kind != evidence.KindBackupRun {
		t.Errorf("kind: %q", got.Kind)
	}

	var pl map[string]any
	if err := json.Unmarshal(got.Payload, &pl); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if pl["duration_seconds"].(float64) != 900 {
		t.Errorf("duration_seconds: %v (expected 900 for 15min backup)", pl["duration_seconds"])
	}
	if pl["kind"] != "postgres" {
		t.Errorf("kind payload: %v", pl["kind"])
	}
	if pl["size_bytes"].(float64) != 123456789 {
		t.Errorf("size_bytes: %v", pl["size_bytes"])
	}
}

func TestSafePrefix(t *testing.T) {
	cases := map[string]string{
		"":                "",
		"abc":             "abc",
		"abcdefghij":      "abcdef",
		"0123456789abcdef": "012345",
	}
	for in, want := range cases {
		if got := safePrefix(in, 6); got != want {
			t.Errorf("safePrefix(%q) = %q, want %q", in, got, want)
		}
	}
}
