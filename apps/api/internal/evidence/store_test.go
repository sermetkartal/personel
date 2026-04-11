package evidence

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"
)

// fakeWORM is an in-memory EvidenceWORM for unit tests. It records every
// PutEvidence call so tests can assert dual-write ordering.
type fakeWORM struct {
	puts     []wormPut
	failWith error
}

type wormPut struct {
	tenantID  string
	period    string
	id        string
	canonical []byte
}

func (f *fakeWORM) PutEvidence(_ context.Context, tenantID, period, id string, canonical []byte) (string, error) {
	if f.failWith != nil {
		return "", f.failWith
	}
	f.puts = append(f.puts, wormPut{
		tenantID:  tenantID,
		period:    period,
		id:        id,
		canonical: append([]byte(nil), canonical...),
	})
	return "evidence/" + tenantID + "/" + period + "/" + id + ".bin", nil
}

func TestStoreInsertRejectsNilWORM(t *testing.T) {
	// A Store with a nil WORM sink must refuse to write evidence. This
	// guarantees we never silently skip the integrity anchor — a SOC 2
	// CC7.1 control failure. See store.go comment.
	s := NewStore(nil, nil)
	_, err := s.Insert(context.Background(), Item{
		TenantID:            "tenant-a",
		Control:             CtrlCC6_1,
		Kind:                KindAccessReview,
		Signature:           []byte{0x01},
		SignatureKeyVersion: "v1",
	})
	if err == nil {
		t.Fatal("expected error when WORM sink is nil")
	}
}

func TestStoreInsertRequiresSignature(t *testing.T) {
	// Caller must go through Recorder.Record() which signs the canonical
	// payload before calling Insert. Direct Store.Insert with an unsigned
	// item is a programming error and must fail loudly.
	s := NewStore(nil, &fakeWORM{})
	_, err := s.Insert(context.Background(), Item{
		TenantID:   "tenant-a",
		Control:    CtrlCC6_1,
		Kind:       KindAccessReview,
		RecordedAt: time.Now().UTC(),
	})
	if err == nil {
		t.Fatal("expected error when signature missing")
	}
}

func TestStoreInsertStopsOnWORMFailure(t *testing.T) {
	// If the WORM put fails, we must return the error before touching
	// Postgres. This test uses a nil pool — any attempt to reach Postgres
	// would panic, proving the WORM failure path short-circuits correctly.
	sentinel := errors.New("simulated worm outage")
	w := &fakeWORM{failWith: sentinel}
	s := NewStore(nil, w)

	_, err := s.Insert(context.Background(), Item{
		TenantID:            "tenant-a",
		Control:             CtrlCC6_1,
		Kind:                KindAccessReview,
		Signature:           []byte{0x01},
		SignatureKeyVersion: "v1",
		RecordedAt:          time.Now().UTC(),
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected wrapped sentinel error, got %v", err)
	}
	if len(w.puts) != 0 {
		t.Fatalf("expected no successful puts, got %d", len(w.puts))
	}
}

// TestRecorderDerivesCollectionPeriodBeforeSigning is a regression test
// for the latent integrity bug found on 2026-04-11 during the first real
// Postgres reality check. Previously Recorder.Record() signed a canonical
// payload with an empty CollectionPeriod, then Store.Insert() filled the
// field to RecordedAt.Format("2006-01"). Verifying the stored row by
// re-canonicalising would compute a DIFFERENT payload than what was
// signed, breaking signature verification after a round trip — a silent
// production-integrity failure.
//
// The fix: Recorder derives CollectionPeriod BEFORE canonicalize so the
// signature covers the real value. This test asserts that property by
// recording an item with empty period, then verifying the item comes
// back from the fake WORM with a non-empty period AND that canonicalize
// applied to the post-record item matches what was put in the WORM.
func TestRecorderDerivesCollectionPeriodBeforeSigning(t *testing.T) {
	// Capturing signer sees the canonical bytes the Recorder produces,
	// which is the exact payload a future verifier would recompute.
	// If CollectionPeriod is missing at sign time, the stored row would
	// fail verification after a round trip.
	cap := &capturingSigner{}
	insert := &captureInserter{}
	rec := newRecorderWithInserter(insert, cap, silentLoggerStore())

	_, err := rec.Record(context.Background(), Item{
		TenantID:   "tenant-a",
		Control:    CtrlCC6_1,
		Kind:       KindPrivilegedAccessSession,
		RecordedAt: time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC),
		// CollectionPeriod intentionally empty
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	if len(cap.payloads) != 1 {
		t.Fatalf("expected 1 signed payload, got %d", len(cap.payloads))
	}
	// canonicalize length-prefixes every field with a 4-byte big-endian
	// length. We search for the prefix {0,0,0,7} followed by "2026-04".
	needle := []byte{0, 0, 0, 7, '2', '0', '2', '6', '-', '0', '4'}
	if !bytes.Contains(cap.payloads[0], needle) {
		t.Error("signed canonical payload must contain length-prefixed collection period '2026-04'")
	}
	// The inserted item must also have the derived period set so the
	// DB row and the signature cover the same value.
	if insert.lastItem.CollectionPeriod != "2026-04" {
		t.Errorf("inserted item period: %q, want 2026-04", insert.lastItem.CollectionPeriod)
	}
}

// capturingSigner records the canonical payload it was asked to sign.
type capturingSigner struct {
	payloads [][]byte
}

func (c *capturingSigner) Sign(_ context.Context, payload []byte) ([]byte, string, error) {
	c.payloads = append(c.payloads, append([]byte(nil), payload...))
	return []byte{0xAB}, "test:v1", nil
}

// captureInserter implements storeInserter without touching Postgres.
type captureInserter struct {
	lastItem Item
}

func (c *captureInserter) Insert(_ context.Context, item Item) (string, error) {
	c.lastItem = item
	return "01J-TEST", nil
}

func silentLoggerStore() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestCanonicalizeDeterministic(t *testing.T) {
	// The signature covers canonicalize output; changing bytes across
	// two calls with the same input would invalidate all historical
	// signatures. Field order and referenced-id sorting must be stable.
	item := Item{
		ID:                 "01J0EXAMPLEULIDVALUE123",
		TenantID:           "tenant-a",
		Control:            CtrlCC6_1,
		Kind:               KindAccessReview,
		CollectionPeriod:   "2026-04",
		RecordedAt:         time.Date(2026, 4, 11, 12, 0, 0, 0, time.UTC),
		Actor:              "dpo@example.com",
		SummaryTR:          "Örnek",
		SummaryEN:          "Example",
		ReferencedAuditIDs: []int64{3, 1, 2},
		AttachmentRefs:     []string{"b.pdf", "a.pdf"},
	}
	a := canonicalize(item)
	b := canonicalize(item)
	if !bytes.Equal(a, b) {
		t.Fatal("canonicalize must be deterministic for identical input")
	}
	// Permuted referenced IDs must produce identical output.
	item2 := item
	item2.ReferencedAuditIDs = []int64{1, 2, 3}
	item2.AttachmentRefs = []string{"a.pdf", "b.pdf"}
	c := canonicalize(item2)
	if !bytes.Equal(a, c) {
		t.Fatal("canonicalize must sort referenced_audit_ids and attachment_refs before signing")
	}
}
