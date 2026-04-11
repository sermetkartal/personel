package evidence

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"
)

// keyedSigner / keyedVerifier simulate Vault transit's key history by
// storing the payload→signature mapping per version. After a rotation,
// signing produces v2 signatures but verification of v1 signatures still
// succeeds against the retained v1 key.
type keyedSigner struct {
	currentVersion string
	// keys maps key version to a simple deterministic "algorithm":
	// for a given version vN, the signature is sha256-like scheme:
	// version byte + reversed payload first 16 bytes. Good enough to
	// prove the verifier logic without importing ed25519.
	keys map[string]bool
}

func newKeyedSigner(versions ...string) *keyedSigner {
	s := &keyedSigner{keys: map[string]bool{}}
	for _, v := range versions {
		s.keys[v] = true
	}
	if len(versions) > 0 {
		s.currentVersion = versions[len(versions)-1]
	}
	return s
}

func (s *keyedSigner) Sign(_ context.Context, payload []byte) ([]byte, string, error) {
	return fakeSignature(s.currentVersion, payload), s.currentVersion, nil
}

// Rotate adds a new key version and marks it current. Old versions
// remain in the key history, matching Vault transit default behaviour.
func (s *keyedSigner) Rotate(newVersion string) {
	s.keys[newVersion] = true
	s.currentVersion = newVersion
}

func (s *keyedSigner) Verify(_ context.Context, payload, sig []byte, keyVersion string) error {
	if !s.keys[keyVersion] {
		return errors.New("key version not in history: " + keyVersion)
	}
	want := fakeSignature(keyVersion, payload)
	if !bytes.Equal(sig, want) {
		return errors.New("signature mismatch for key " + keyVersion)
	}
	return nil
}

// fakeSignature produces a deterministic marker that encodes both the
// key version and a SHA-256 digest of the full payload, so both
// tampering and mismatched-version verifications fail cleanly. This
// mirrors Vault transit's per-version Ed25519 semantics closely enough
// for the rotation-survival invariant to be a meaningful test.
func fakeSignature(keyVersion string, payload []byte) []byte {
	h := sha256.Sum256(payload)
	out := make([]byte, 0, 8+len(keyVersion)+1+len(h))
	out = append(out, []byte("sig:")...)
	out = append(out, []byte(keyVersion)...)
	out = append(out, ':')
	out = append(out, h[:]...)
	return out
}

func TestVerifyItem_RoundtripCurrentKey(t *testing.T) {
	// Sign with v1 → verify with v1 → must pass.
	signer := newKeyedSigner("control-plane:v1")
	item := sampleItem(t)
	sig, keyVer, err := signer.Sign(context.Background(), canonicalize(item))
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	item.Signature = sig
	item.SignatureKeyVersion = keyVer

	if err := VerifyItem(context.Background(), signer, item); err != nil {
		t.Fatalf("verify current: %v", err)
	}
}

func TestVerifyItem_SurvivesKeyRotation(t *testing.T) {
	// Phase 3.0 5-year retention requirement: an item signed with v1
	// must remain verifiable after the key is rotated to v2 and v3.
	// If this test ever fails, WORM retention semantics are broken —
	// we'd be unable to prove authenticity of evidence older than one
	// rotation cycle, which defeats the entire Type II audit trail.
	signer := newKeyedSigner("control-plane:v1")
	oldItem := sampleItem(t)
	sig, keyVer, _ := signer.Sign(context.Background(), canonicalize(oldItem))
	oldItem.Signature = sig
	oldItem.SignatureKeyVersion = keyVer

	// Operator rotates the key twice.
	signer.Rotate("control-plane:v2")
	signer.Rotate("control-plane:v3")

	// New items should sign with v3 now.
	newItem := sampleItem(t)
	newItem.ID = "01J-NEW"
	newSig, newKeyVer, _ := signer.Sign(context.Background(), canonicalize(newItem))
	newItem.Signature = newSig
	newItem.SignatureKeyVersion = newKeyVer
	if newKeyVer != "control-plane:v3" {
		t.Errorf("expected new signatures to use v3, got %q", newKeyVer)
	}

	// Old item must still verify against v1 via key history.
	if err := VerifyItem(context.Background(), signer, oldItem); err != nil {
		t.Fatalf("verify after rotation: %v", err)
	}
	// New item verifies against v3.
	if err := VerifyItem(context.Background(), signer, newItem); err != nil {
		t.Fatalf("verify new: %v", err)
	}
}

func TestVerifyItem_DetectsTampering(t *testing.T) {
	// Tampering with the payload without re-signing must be caught.
	// This is the core integrity property of the WORM + signature chain.
	signer := newKeyedSigner("control-plane:v1")
	item := sampleItem(t)
	sig, keyVer, _ := signer.Sign(context.Background(), canonicalize(item))
	item.Signature = sig
	item.SignatureKeyVersion = keyVer

	// Attacker tampers with the summary.
	item.SummaryEN = "Tampered summary"

	if err := VerifyItem(context.Background(), signer, item); err == nil {
		t.Fatal("expected verify to fail on tampered payload")
	}
}

func TestVerifyItem_RejectsUnknownKeyVersion(t *testing.T) {
	// If an item references a key version that the verifier no longer
	// retains (operator set min_decryption_version past it), verify
	// must fail loudly — the SOC 2 chain is broken for that item.
	signer := newKeyedSigner("control-plane:v2") // v1 not present
	item := sampleItem(t)
	item.Signature = fakeSignature("control-plane:v1", canonicalize(item))
	item.SignatureKeyVersion = "control-plane:v1"

	if err := VerifyItem(context.Background(), signer, item); err == nil {
		t.Fatal("expected verify to fail for missing key version")
	}
}

func TestVerifyItem_NilVerifier(t *testing.T) {
	if err := VerifyItem(context.Background(), nil, sampleItem(t)); err == nil {
		t.Fatal("expected error for nil verifier")
	}
}

func sampleItem(t *testing.T) Item {
	t.Helper()
	return Item{
		ID:               "01J-SAMPLE",
		TenantID:         "tenant-a",
		Control:          CtrlCC6_1,
		Kind:             KindPrivilegedAccessSession,
		CollectionPeriod: "2026-04",
		RecordedAt:       time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC),
		Actor:            "admin",
		SummaryTR:        "Örnek",
		SummaryEN:        "Sample",
	}
}
