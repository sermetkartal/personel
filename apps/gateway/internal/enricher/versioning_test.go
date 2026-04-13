package enricher

import (
	"errors"
	"testing"

	"google.golang.org/protobuf/proto"

	personelv1 "github.com/personel/proto/personel/v1"
)

// helper: a valid v1 payload (an empty EventBatch marshalled).
func validV1Payload(t *testing.T) []byte {
	t.Helper()
	batch := &personelv1.EventBatch{BatchId: 42}
	raw, err := proto.Marshal(batch)
	if err != nil {
		t.Fatalf("marshal test batch: %v", err)
	}
	return raw
}

func TestVersionedDecoder_DispatchV1(t *testing.T) {
	vd := NewDefaultDecoder()
	raw := validV1Payload(t)

	headers := map[string][]string{
		HeaderSchemaVersion: {SchemaV1},
	}
	batch, version, err := vd.Dispatch(headers, raw)
	if err != nil {
		t.Fatalf("dispatch v1: unexpected error: %v", err)
	}
	if version != SchemaV1 {
		t.Errorf("version = %q, want %q", version, SchemaV1)
	}
	if batch == nil {
		t.Fatal("batch is nil")
	}
	if batch.GetBatchId() != 42 {
		t.Errorf("batch_id = %d, want 42", batch.GetBatchId())
	}
}

func TestVersionedDecoder_FallbackToV1OnMissingHeader(t *testing.T) {
	vd := NewDefaultDecoder()
	raw := validV1Payload(t)

	// No headers at all.
	batch, version, err := vd.Dispatch(nil, raw)
	if err != nil {
		t.Fatalf("dispatch (nil headers): unexpected error: %v", err)
	}
	if version != SchemaV1 {
		t.Errorf("version = %q, want %q (fallback)", version, SchemaV1)
	}
	if batch == nil {
		t.Fatal("batch is nil")
	}

	// Empty headers map.
	batch, version, err = vd.Dispatch(map[string][]string{}, raw)
	if err != nil {
		t.Fatalf("dispatch (empty headers): unexpected error: %v", err)
	}
	if version != SchemaV1 {
		t.Errorf("version = %q, want %q (fallback)", version, SchemaV1)
	}

	// Empty string value.
	batch, version, err = vd.Dispatch(map[string][]string{
		HeaderSchemaVersion: {""},
	}, raw)
	if err != nil {
		t.Fatalf("dispatch (empty value): unexpected error: %v", err)
	}
	if version != SchemaV1 {
		t.Errorf("version = %q, want %q (fallback)", version, SchemaV1)
	}
}

func TestVersionedDecoder_DispatchV2ReturnsNotImplemented(t *testing.T) {
	vd := NewDefaultDecoder()
	headers := map[string][]string{
		HeaderSchemaVersion: {SchemaV2},
	}
	_, version, err := vd.Dispatch(headers, []byte{0x00})
	if err == nil {
		t.Fatal("expected ErrNotImplemented, got nil")
	}
	if !errors.Is(err, ErrNotImplemented) {
		t.Errorf("error = %v, want ErrNotImplemented", err)
	}
	if version != SchemaV2 {
		t.Errorf("version = %q, want %q", version, SchemaV2)
	}
}

func TestVersionedDecoder_DispatchUnknownVersion(t *testing.T) {
	vd := NewDefaultDecoder()
	headers := map[string][]string{
		HeaderSchemaVersion: {"v99"},
	}
	_, version, err := vd.Dispatch(headers, []byte{0x00})
	if err == nil {
		t.Fatal("expected ErrUnknownSchemaVersion, got nil")
	}
	if !errors.Is(err, ErrUnknownSchemaVersion) {
		t.Errorf("error = %v, want ErrUnknownSchemaVersion", err)
	}
	if version != "v99" {
		t.Errorf("version = %q, want %q", version, "v99")
	}
}

func TestVersionedDecoder_DispatchCaseInsensitiveHeader(t *testing.T) {
	vd := NewDefaultDecoder()
	raw := validV1Payload(t)

	// Canonicalised by a NATS client as "Schema-Version".
	headers := map[string][]string{
		"Schema-Version": {SchemaV1},
	}
	_, version, err := vd.Dispatch(headers, raw)
	if err != nil {
		t.Fatalf("case-insensitive: unexpected error: %v", err)
	}
	if version != SchemaV1 {
		t.Errorf("version = %q, want %q", version, SchemaV1)
	}
}

func TestVersionedDecoder_V1DecoderRejectsGarbage(t *testing.T) {
	vd := NewDefaultDecoder()
	headers := map[string][]string{
		HeaderSchemaVersion: {SchemaV1},
	}
	// Random non-proto bytes.
	_, _, err := vd.Dispatch(headers, []byte{0xFF, 0xFE, 0xFD, 0x01, 0x02})
	if err == nil {
		t.Fatal("expected decode error on garbage, got nil")
	}
	if errors.Is(err, ErrUnknownSchemaVersion) {
		t.Error("garbage payload leaked ErrUnknownSchemaVersion; must surface raw decode error")
	}
	if errors.Is(err, ErrNotImplemented) {
		t.Error("garbage payload leaked ErrNotImplemented")
	}
}

func TestNewVersionedDecoder_RejectsDuplicates(t *testing.T) {
	_, err := NewVersionedDecoder(v1Decoder{}, v1Decoder{})
	if err == nil {
		t.Fatal("expected duplicate rejection, got nil")
	}
}

func TestNewVersionedDecoder_RejectsNil(t *testing.T) {
	_, err := NewVersionedDecoder(nil)
	if err == nil {
		t.Fatal("expected nil-decoder rejection, got nil")
	}
}

func TestVersionedDecoder_KnownVersions(t *testing.T) {
	vd := NewDefaultDecoder()
	got := vd.KnownVersions()
	if len(got) != 2 {
		t.Fatalf("expected 2 versions, got %d: %v", len(got), got)
	}
	haveV1, haveV2 := false, false
	for _, v := range got {
		if v == SchemaV1 {
			haveV1 = true
		}
		if v == SchemaV2 {
			haveV2 = true
		}
	}
	if !haveV1 || !haveV2 {
		t.Errorf("KnownVersions() missing v1 or v2: %v", got)
	}
}
