package integrations

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"testing"
)

// fakeVault is a deterministic in-memory encryptor used by unit tests.
// It prepends a "stub:" sentinel so the test can assert that Upsert
// actually hit Encrypt, and decodes with base64 so round-tripping
// recovers the original plaintext.
type fakeVault struct {
	encCalls int
	decCalls int
	failEnc  bool
	failDec  bool
}

func (f *fakeVault) Encrypt(_ context.Context, key string, pt []byte) ([]byte, int, error) {
	f.encCalls++
	if f.failEnc {
		return nil, 0, errors.New("boom")
	}
	out := []byte("stub:" + key + ":" + base64.StdEncoding.EncodeToString(pt))
	return out, 42, nil
}

func (f *fakeVault) Decrypt(_ context.Context, key string, ct []byte) ([]byte, error) {
	f.decCalls++
	if f.failDec {
		return nil, errors.New("boom")
	}
	prefix := []byte("stub:" + key + ":")
	if len(ct) < len(prefix) {
		return nil, fmt.Errorf("bad ct")
	}
	return base64.StdEncoding.DecodeString(string(ct[len(prefix):]))
}

func silentLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// --- maskConfig ---

func TestMaskConfigReplacesSensitiveFields(t *testing.T) {
	in := map[string]any{
		"account_id":  "891169",
		"license_key": "SECRET_VALUE",
		"api_token":   "TOKEN",
		"foo":         "bar",
	}
	out := maskConfig(in)
	if out["account_id"] != "891169" {
		t.Errorf("expected account_id passthrough, got %v", out["account_id"])
	}
	if out["foo"] != "bar" {
		t.Errorf("expected foo passthrough, got %v", out["foo"])
	}
	if out["license_key"] != MaskedValue {
		t.Errorf("expected license_key masked, got %v", out["license_key"])
	}
	if out["api_token"] != MaskedValue {
		t.Errorf("expected api_token masked, got %v", out["api_token"])
	}
}

func TestMaskConfigEmptySensitiveStaysEmpty(t *testing.T) {
	in := map[string]any{"license_key": ""}
	out := maskConfig(in)
	if out["license_key"] != "" {
		t.Errorf("empty sensitive should stay empty, got %v", out["license_key"])
	}
}

// --- AllowedServices gate ---

func TestGetUnknownServiceRejected(t *testing.T) {
	svc := &Service{vault: &fakeVault{}, log: silentLog()}
	if _, err := svc.Get(context.Background(), "tenant", "not_a_service"); !errors.Is(err, ErrUnknownService) {
		t.Fatalf("expected ErrUnknownService, got %v", err)
	}
}

func TestUpsertUnknownServiceRejected(t *testing.T) {
	svc := &Service{vault: &fakeVault{}, log: silentLog()}
	err := svc.Upsert(context.Background(), "actor", "tenant", "oops", UpsertRequest{})
	if !errors.Is(err, ErrUnknownService) {
		t.Fatalf("expected ErrUnknownService, got %v", err)
	}
}

func TestDeleteUnknownServiceRejected(t *testing.T) {
	svc := &Service{vault: &fakeVault{}, log: silentLog()}
	err := svc.Delete(context.Background(), "actor", "tenant", "typo")
	if !errors.Is(err, ErrUnknownService) {
		t.Fatalf("expected ErrUnknownService, got %v", err)
	}
}

func TestDecryptUnknownServiceRejected(t *testing.T) {
	svc := &Service{vault: &fakeVault{}, log: silentLog()}
	if _, err := svc.Decrypt(context.Background(), "tenant", "bogus"); !errors.Is(err, ErrUnknownService) {
		t.Fatalf("expected ErrUnknownService, got %v", err)
	}
}

// --- Vault unavailable ---

func TestUpsertRejectsNilVault(t *testing.T) {
	svc := &Service{vault: nil, log: silentLog()}
	err := svc.Upsert(context.Background(), "actor", "tenant", "maxmind", UpsertRequest{
		Config: map[string]any{"account_id": "1"},
	})
	if !errors.Is(err, ErrVaultUnavailable) {
		t.Fatalf("expected ErrVaultUnavailable, got %v", err)
	}
}

// --- decryptToMap round trip ---

func TestDecryptToMapRoundTrip(t *testing.T) {
	fv := &fakeVault{}
	svc := &Service{vault: fv, log: silentLog()}
	original := map[string]any{"license_key": "real", "account_id": "891169"}
	pt, _ := json.Marshal(original)
	ct, _, err := fv.Encrypt(context.Background(), integrationsKey, pt)
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.decryptToMap(context.Background(), ct)
	if err != nil {
		t.Fatal(err)
	}
	if out["license_key"] != "real" || out["account_id"] != "891169" {
		t.Fatalf("round trip failed: %v", out)
	}
}

func TestDecryptToMapFailsWhenVaultFails(t *testing.T) {
	fv := &fakeVault{failDec: true}
	svc := &Service{vault: fv, log: silentLog()}
	if _, err := svc.decryptToMap(context.Background(), []byte("stub:integrations:x")); err == nil {
		t.Fatal("expected error from failing vault")
	}
}

// --- keysOf ---

func TestKeysOf(t *testing.T) {
	got := keysOf(map[string]any{"a": 1, "b": 2})
	if len(got) != 2 {
		t.Fatalf("expected 2 keys, got %v", got)
	}
}
