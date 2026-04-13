package enricher

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	natslib "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// fakeJS is a minimal jsPublisher used by DLQPublisher tests. It
// records every Publish call and can be primed to return errors.
type fakeJS struct {
	calls       []fakePub
	returnErr   error
	returnSeq   uint64
}

type fakePub struct {
	Subject string
	Data    []byte
}

func (f *fakeJS) Publish(_ context.Context, subj string, data []byte, _ ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	// Copy data — caller may reuse the buffer.
	cpy := make([]byte, len(data))
	copy(cpy, data)
	f.calls = append(f.calls, fakePub{Subject: subj, Data: cpy})
	if f.returnErr != nil {
		return nil, f.returnErr
	}
	seq := f.returnSeq
	if seq == 0 {
		seq = uint64(len(f.calls))
	}
	return &jetstream.PubAck{Stream: DLQStreamName, Sequence: seq}, nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestDLQMessage_JSONRoundTrip(t *testing.T) {
	orig := &DLQMessage{
		OriginalSubject: "events.raw.tenant-1.process.start",
		OriginalHeaders: map[string]string{
			HeaderSchemaVersion: SchemaV1,
			"agent_id":          "endpoint-42",
		},
		OriginalPayload: []byte{0x08, 0x2A}, // proto-ish
		ErrorKind:       DLQKindEnrich,
		ErrorMessage:    "postgres: endpoint not found",
		FailedAt:        time.Date(2026, 4, 13, 14, 30, 0, 0, time.UTC),
		RetryCount:      2,
		TenantID:        "550e8400-e29b-41d4-a716-446655440000",
		BatchID:         99,
	}

	raw, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got DLQMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if got.OriginalSubject != orig.OriginalSubject {
		t.Errorf("subject: %q vs %q", got.OriginalSubject, orig.OriginalSubject)
	}
	if got.ErrorKind != orig.ErrorKind {
		t.Errorf("error_kind: %q vs %q", got.ErrorKind, orig.ErrorKind)
	}
	if got.ErrorMessage != orig.ErrorMessage {
		t.Errorf("error_message: %q vs %q", got.ErrorMessage, orig.ErrorMessage)
	}
	if got.RetryCount != orig.RetryCount {
		t.Errorf("retry_count: %d vs %d", got.RetryCount, orig.RetryCount)
	}
	if got.TenantID != orig.TenantID {
		t.Errorf("tenant_id: %q vs %q", got.TenantID, orig.TenantID)
	}
	if got.BatchID != orig.BatchID {
		t.Errorf("batch_id: %d vs %d", got.BatchID, orig.BatchID)
	}
	if len(got.OriginalPayload) != len(orig.OriginalPayload) {
		t.Errorf("payload length: %d vs %d", len(got.OriginalPayload), len(orig.OriginalPayload))
	}
	if got.OriginalHeaders[HeaderSchemaVersion] != SchemaV1 {
		t.Errorf("schema header did not round-trip")
	}
}

func TestDLQPublisher_PublishHappyPath(t *testing.T) {
	fake := &fakeJS{}
	dlq := NewDLQPublisher(fake, DLQPublisherConfig{MaxRetries: 3}, discardLogger())

	m := &DLQMessage{
		OriginalSubject: "events.raw.foo.bar",
		TenantID:        "tenant-abc",
		ErrorKind:       DLQKindDecode,
		ErrorMessage:    "unknown schema_version",
	}

	seq, err := dlq.Publish(context.Background(), m)
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if seq != 1 {
		t.Errorf("seq = %d, want 1", seq)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("expected 1 publish, got %d", len(fake.calls))
	}
	call := fake.calls[0]
	wantSubject := DLQSubjectBase + ".tenant-abc"
	if call.Subject != wantSubject {
		t.Errorf("subject = %q, want %q", call.Subject, wantSubject)
	}

	// Verify the body round-trips.
	var got DLQMessage
	if err := json.Unmarshal(call.Data, &got); err != nil {
		t.Fatalf("unmarshal published body: %v", err)
	}
	if got.ErrorKind != DLQKindDecode {
		t.Errorf("error_kind = %q", got.ErrorKind)
	}
	if got.FailedAt.IsZero() {
		t.Error("FailedAt was not auto-populated")
	}
}

func TestDLQPublisher_PublishPropagatesError(t *testing.T) {
	want := errors.New("boom")
	fake := &fakeJS{returnErr: want}
	dlq := NewDLQPublisher(fake, DLQPublisherConfig{}, discardLogger())

	_, err := dlq.Publish(context.Background(), &DLQMessage{
		TenantID:  "t1",
		ErrorKind: DLQKindEnrich,
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDLQPublisher_NilMessage(t *testing.T) {
	dlq := NewDLQPublisher(&fakeJS{}, DLQPublisherConfig{}, discardLogger())
	if _, err := dlq.Publish(context.Background(), nil); err == nil {
		t.Fatal("expected error on nil message")
	}
}

func TestDLQPublisher_EmptyTenantUsesUnknownSuffix(t *testing.T) {
	fake := &fakeJS{}
	dlq := NewDLQPublisher(fake, DLQPublisherConfig{}, discardLogger())

	if _, err := dlq.Publish(context.Background(), &DLQMessage{
		OriginalSubject: "events.raw.foo",
		ErrorKind:       DLQKindDecode,
	}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(fake.calls) != 1 {
		t.Fatal("no publish recorded")
	}
	want := DLQSubjectBase + ".unknown"
	if fake.calls[0].Subject != want {
		t.Errorf("subject = %q, want %q", fake.calls[0].Subject, want)
	}
}

func TestDLQPublisher_MaxRetriesDefault(t *testing.T) {
	dlq := NewDLQPublisher(&fakeJS{}, DLQPublisherConfig{}, discardLogger())
	if dlq.MaxRetries() != 3 {
		t.Errorf("MaxRetries default = %d, want 3", dlq.MaxRetries())
	}
}

func TestSanitiseSubjectToken(t *testing.T) {
	cases := map[string]string{
		"":               "unknown",
		"tenant-1":       "tenant-1",
		"tenant 1":       "tenant_1",
		"tenant.1":       "tenant_1",
		"tenant*1":       "tenant_1",
		"tenant>1":       "tenant_1",
		"550e8400-e29b":  "550e8400-e29b",
	}
	for in, want := range cases {
		if got := sanitiseSubjectToken(in); got != want {
			t.Errorf("sanitise(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRetryCountOf(t *testing.T) {
	h := natslib.Header{}
	if RetryCountOf(h) != 0 {
		t.Error("empty header should be 0")
	}
	h.Set(headerRetryCount, "5")
	if RetryCountOf(h) != 5 {
		t.Errorf("got %d, want 5", RetryCountOf(h))
	}
	h.Set(headerRetryCount, "garbage")
	if RetryCountOf(h) != 0 {
		t.Error("garbage header should be 0")
	}
	if RetryCountOf(nil) != 0 {
		t.Error("nil header should be 0")
	}
}

func TestFlattenHeaders(t *testing.T) {
	h := natslib.Header{}
	h.Add("foo", "1")
	h.Add("foo", "2")
	h.Set("bar", "baz")
	got := flattenHeaders(h)
	if got["foo"] != "1" {
		t.Errorf("foo = %q, want 1 (first value)", got["foo"])
	}
	if got["bar"] != "baz" {
		t.Errorf("bar = %q, want baz", got["bar"])
	}
	if len(got) != 2 {
		t.Errorf("got %d keys, want 2", len(got))
	}
}

func TestBuildDLQMessage(t *testing.T) {
	msg := &fakeMsg{
		subj:    "events.raw.tenant-z.process.start",
		data:    []byte("raw-proto-bytes"),
		headers: natslib.Header{},
	}
	msg.headers.Set(HeaderSchemaVersion, SchemaV1)
	msg.headers.Set(headerRetryCount, "2")

	out := BuildDLQMessage(msg, DLQKindEnrich, "pg down", "tenant-z", 1234)
	if out.OriginalSubject != msg.subj {
		t.Errorf("subject mismatch")
	}
	if out.RetryCount != 2 {
		t.Errorf("retry = %d, want 2", out.RetryCount)
	}
	if out.TenantID != "tenant-z" {
		t.Errorf("tenant mismatch")
	}
	if out.BatchID != 1234 {
		t.Errorf("batch mismatch")
	}
	if out.ErrorKind != DLQKindEnrich {
		t.Errorf("kind mismatch")
	}
	if out.OriginalHeaders[HeaderSchemaVersion] != SchemaV1 {
		t.Errorf("headers not flattened")
	}
	if string(out.OriginalPayload) != "raw-proto-bytes" {
		t.Errorf("payload mismatch")
	}
}

// fakeMsg is a minimal jetstream.Msg used only by BuildDLQMessage test.
// Only Subject/Data/Headers are exercised; the rest panic if called.
type fakeMsg struct {
	jetstream.Msg
	subj    string
	data    []byte
	headers natslib.Header
}

func (f *fakeMsg) Subject() string       { return f.subj }
func (f *fakeMsg) Data() []byte          { return f.data }
func (f *fakeMsg) Headers() natslib.Header { return f.headers }
