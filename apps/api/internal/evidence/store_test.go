package evidence

import (
	"bytes"
	"context"
	"errors"
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
