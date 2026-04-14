package kvkk

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeDocs records PutDocument calls so happy-path tests can assert the
// object key scheme and payload bytes without booting a MinIO instance.
type fakeDocs struct {
	puts []fakeDocsPut
}

type fakeDocsPut struct {
	key         string
	data        []byte
	contentType string
}

func (f *fakeDocs) PutDocument(_ context.Context, key string, data []byte, ct string) error {
	f.puts = append(f.puts, fakeDocsPut{key: key, data: append([]byte(nil), data...), contentType: ct})
	return nil
}

// Minimal PDF bytes — just enough to pass validatePDF. The first four
// bytes must be "%PDF"; the rest is ignored by SHA-256 and MinIO.
var minimalPDF = []byte("%PDF-1.4 tiny test payload, not a real pdf")

// --- validatePDF / sha256Hex ---

func TestValidatePDFRejectsEmpty(t *testing.T) {
	if err := validatePDF(nil); err == nil {
		t.Fatal("expected error for empty")
	}
	if err := validatePDF([]byte{}); err == nil {
		t.Fatal("expected error for zero-length")
	}
}

func TestValidatePDFRejectsOversized(t *testing.T) {
	big := bytes.Repeat([]byte("%PDF"), MaxDocumentBytes) // way over limit
	if err := validatePDF(big); err == nil {
		t.Fatal("expected error for oversized upload")
	}
}

func TestValidatePDFRejectsNonPDFHeader(t *testing.T) {
	if err := validatePDF([]byte("NOPE this is plain text")); err == nil {
		t.Fatal("expected error for non-PDF header")
	}
}

func TestValidatePDFAcceptsMinimalPDF(t *testing.T) {
	if err := validatePDF(minimalPDF); err != nil {
		t.Fatalf("expected minimal PDF to pass: %v", err)
	}
}

func TestSha256HexDeterministic(t *testing.T) {
	a := sha256Hex([]byte("hello"))
	b := sha256Hex([]byte("hello"))
	if a != b {
		t.Fatalf("sha256Hex not deterministic: %q vs %q", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("expected 64 hex chars, got %d", len(a))
	}
	if sha256Hex([]byte("hello")) == sha256Hex([]byte("world")) {
		t.Fatal("distinct inputs hashed the same")
	}
}

// --- Service validation guards that run BEFORE any recorder.Append ---

func TestUpdateVerbisRejectsEmptyRegistration(t *testing.T) {
	svc := &Service{log: silentLogger(), now: time.Now}
	err := svc.UpdateVerbis(context.Background(), "actor", "tenant",
		UpdateVerbisRequest{RegistrationNumber: "   "})
	if err == nil {
		t.Fatal("expected empty registration_number to reject")
	}
}

func TestUpdateVerbisRejectsTooLong(t *testing.T) {
	svc := &Service{log: silentLogger(), now: time.Now}
	err := svc.UpdateVerbis(context.Background(), "actor", "tenant",
		UpdateVerbisRequest{RegistrationNumber: strings.Repeat("a", 200)})
	if err == nil {
		t.Fatal("expected too-long registration_number to reject")
	}
}

func TestPublishAydinlatmaRejectsEmpty(t *testing.T) {
	svc := &Service{log: silentLogger(), now: time.Now}
	_, err := svc.PublishAydinlatma(context.Background(), "actor", "tenant",
		PublishAydinlatmaRequest{Markdown: ""})
	if err == nil {
		t.Fatal("expected empty markdown to reject")
	}
	_, err = svc.PublishAydinlatma(context.Background(), "actor", "tenant",
		PublishAydinlatmaRequest{Markdown: "     \n\t"})
	if err == nil {
		t.Fatal("expected whitespace-only markdown to reject")
	}
}

func TestPublishAydinlatmaRejectsOversized(t *testing.T) {
	svc := &Service{log: silentLogger(), now: time.Now}
	_, err := svc.PublishAydinlatma(context.Background(), "actor", "tenant",
		PublishAydinlatmaRequest{Markdown: strings.Repeat("x", 300*1024)})
	if err == nil {
		t.Fatal("expected oversized markdown to reject")
	}
}

func TestUploadDpaRejectsMissingSignatories(t *testing.T) {
	svc := &Service{log: silentLogger(), now: time.Now}
	_, err := svc.UploadDpa(context.Background(), "actor", "tenant", UploadDpaRequest{
		PDFBytes:    minimalPDF,
		SignedAt:    time.Now(),
		Signatories: nil,
	})
	if err == nil {
		t.Fatal("expected rejection when signatories list is empty")
	}
}

func TestUploadDpaRejectsZeroSignedAt(t *testing.T) {
	svc := &Service{log: silentLogger(), now: time.Now}
	_, err := svc.UploadDpa(context.Background(), "actor", "tenant", UploadDpaRequest{
		PDFBytes:    minimalPDF,
		Signatories: []DpaSignatory{{Name: "X"}},
	})
	if err == nil {
		t.Fatal("expected rejection when signed_at is zero")
	}
}

func TestUploadDpaRejectsNoDocStore(t *testing.T) {
	svc := &Service{log: silentLogger(), now: time.Now} // docs = nil
	_, err := svc.UploadDpa(context.Background(), "actor", "tenant", UploadDpaRequest{
		PDFBytes:    minimalPDF,
		SignedAt:    time.Now(),
		Signatories: []DpaSignatory{{Name: "X", Role: "CTO", Organization: "Personel", SignedAt: time.Now()}},
	})
	if err == nil {
		t.Fatal("expected ErrNoDocumentStore")
	}
}

func TestUploadDpiaRejectsZeroCompletedAt(t *testing.T) {
	svc := &Service{log: silentLogger(), now: time.Now}
	_, err := svc.UploadDpia(context.Background(), "actor", "tenant", UploadDpiaRequest{
		PDFBytes: minimalPDF,
	})
	if err == nil {
		t.Fatal("expected rejection when completed_at is zero")
	}
}

func TestRecordConsentRejectsUnknownType(t *testing.T) {
	svc := &Service{log: silentLogger(), now: time.Now}
	req := RecordConsentRequest{
		UserID:      uuid.New(),
		ConsentType: "cookies", // not in AllowedConsentTypes
		SignedAt:    time.Now(),
	}
	_, err := svc.RecordConsent(context.Background(), "actor", "tenant", req, minimalPDF)
	if err == nil {
		t.Fatal("expected rejection for unknown consent_type")
	}
}

func TestRecordConsentRejectsNilUserID(t *testing.T) {
	svc := &Service{log: silentLogger(), now: time.Now}
	req := RecordConsentRequest{
		UserID:      uuid.Nil,
		ConsentType: "dlp",
		SignedAt:    time.Now(),
	}
	_, err := svc.RecordConsent(context.Background(), "actor", "tenant", req, minimalPDF)
	if err == nil {
		t.Fatal("expected rejection for nil user_id")
	}
}

func TestRecordConsentRejectsZeroSignedAt(t *testing.T) {
	svc := &Service{log: silentLogger(), now: time.Now}
	req := RecordConsentRequest{
		UserID:      uuid.New(),
		ConsentType: "dlp",
	}
	_, err := svc.RecordConsent(context.Background(), "actor", "tenant", req, minimalPDF)
	if err == nil {
		t.Fatal("expected rejection for zero signed_at")
	}
}

func TestRevokeConsentRejectsUnknownType(t *testing.T) {
	svc := &Service{log: silentLogger(), now: time.Now}
	err := svc.RevokeConsent(context.Background(), "actor", "tenant", uuid.New(), "not-a-type")
	if err == nil {
		t.Fatal("expected rejection for unknown consent_type")
	}
}

// AllowedConsentTypes is the defence-in-depth whitelist — drift here
// would silently admit a typo as a new consent category. Lock the
// exact set under test.
func TestAllowedConsentTypesLocked(t *testing.T) {
	expected := []string{
		"dlp",
		"live_view_recording",
		"screen_capture_high_freq",
		"cross_department_transfer",
	}
	if len(AllowedConsentTypes) != len(expected) {
		t.Fatalf("AllowedConsentTypes size drifted: got %d, want %d",
			len(AllowedConsentTypes), len(expected))
	}
	for _, key := range expected {
		if _, ok := AllowedConsentTypes[key]; !ok {
			t.Errorf("AllowedConsentTypes missing %q", key)
		}
	}
}

// Sanity-check the document store adapter contract with a fake: the
// path through PutDocument must deliver the exact bytes and content-type
// we expect. This protects against a future refactor swapping the fake
// out for a broken interface.
func TestFakeDocStoreContract(t *testing.T) {
	fake := &fakeDocs{}
	err := fake.PutDocument(context.Background(), "kvkk/t/dpa/x.pdf", minimalPDF, "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.puts) != 1 {
		t.Fatalf("expected 1 put, got %d", len(fake.puts))
	}
	p := fake.puts[0]
	if p.key != "kvkk/t/dpa/x.pdf" {
		t.Errorf("key: %q", p.key)
	}
	if !bytes.Equal(p.data, minimalPDF) {
		t.Errorf("data mismatch")
	}
	if p.contentType != "application/pdf" {
		t.Errorf("content-type: %q", p.contentType)
	}
}
