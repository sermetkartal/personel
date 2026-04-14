package license

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// makeLicense is a helper that signs a Claims struct with a freshly
// generated keypair and writes it to a temp file. Returns the public
// key, the license path, and cleanup function.
func makeLicense(t *testing.T, claims Claims) (ed25519.PublicKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	canonical, err := canonicalize(claims)
	if err != nil {
		t.Fatalf("canonicalize: %v", err)
	}
	sig := ed25519.Sign(priv, canonical)

	rawClaims, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	file := File{
		Claims:    rawClaims,
		Signature: base64.StdEncoding.EncodeToString(sig),
		KeyID:     "test-vendor-key",
	}
	blob, err := json.Marshal(file)
	if err != nil {
		t.Fatalf("marshal file: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "license.json")
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return pub, path
}

func TestValidLicenseLoads(t *testing.T) {
	claims := Claims{
		CustomerID:   "acme-corp",
		Tier:         TierBusiness,
		MaxEndpoints: 100,
		Features:     []string{"uba", "ocr"},
		IssuedAt:     time.Now().Add(-24 * time.Hour),
		ExpiresAt:    time.Now().Add(30 * 24 * time.Hour),
	}
	pub, path := makeLicense(t, claims)

	svc := NewService(Options{
		LicensePath: path,
		PublicKey:   pub,
		Log:         slog.Default(),
	})

	if svc.State() != StateValid {
		t.Fatalf("state = %s, want valid (err=%v)", svc.State(), svc.LastError())
	}
	c := svc.Claims()
	if c == nil {
		t.Fatal("Claims() returned nil")
	}
	if c.CustomerID != "acme-corp" {
		t.Errorf("customer_id = %q, want acme-corp", c.CustomerID)
	}
	if !svc.HasFeature("uba") {
		t.Error("expected uba feature enabled")
	}
	if svc.HasFeature("siem") {
		t.Error("unexpected siem feature")
	}
}

func TestCapacityCheck(t *testing.T) {
	claims := Claims{
		CustomerID:   "acme",
		Tier:         TierStarter,
		MaxEndpoints: 50,
		IssuedAt:     time.Now().Add(-1 * time.Hour),
		ExpiresAt:    time.Now().Add(30 * 24 * time.Hour),
	}
	pub, path := makeLicense(t, claims)
	svc := NewService(Options{LicensePath: path, PublicKey: pub})

	if err := svc.CheckCapacity(25); err != nil {
		t.Errorf("CheckCapacity(25): %v", err)
	}
	if err := svc.CheckCapacity(50); err != nil {
		t.Errorf("CheckCapacity(50) (at cap): %v", err)
	}
	if err := svc.CheckCapacity(51); err == nil {
		t.Error("CheckCapacity(51) should fail")
	}
}

func TestExpiredEntersGracePeriod(t *testing.T) {
	claims := Claims{
		CustomerID:   "acme",
		Tier:         TierTrial,
		MaxEndpoints: 50,
		IssuedAt:     time.Now().Add(-60 * 24 * time.Hour),
		ExpiresAt:    time.Now().Add(-2 * 24 * time.Hour), // 2 days ago
	}
	pub, path := makeLicense(t, claims)
	svc := NewService(Options{LicensePath: path, PublicKey: pub})

	if svc.State() != StateGrace {
		t.Fatalf("state = %s, want grace", svc.State())
	}
	if err := svc.RequireWritable(); err == nil {
		t.Error("RequireWritable should fail in grace period")
	}
}

func TestExpiredBeyondGrace(t *testing.T) {
	claims := Claims{
		CustomerID:   "acme",
		Tier:         TierTrial,
		MaxEndpoints: 50,
		IssuedAt:     time.Now().Add(-90 * 24 * time.Hour),
		ExpiresAt:    time.Now().Add(-10 * 24 * time.Hour), // 10 days past
	}
	pub, path := makeLicense(t, claims)
	svc := NewService(Options{LicensePath: path, PublicKey: pub})

	if svc.State() != StateExpired {
		t.Fatalf("state = %s, want expired", svc.State())
	}
}

func TestInvalidSignatureRejected(t *testing.T) {
	claims := Claims{
		CustomerID:   "acme",
		MaxEndpoints: 10,
		ExpiresAt:    time.Now().Add(30 * 24 * time.Hour),
	}
	goodPub, path := makeLicense(t, claims)

	// Swap public key for a different one → signature no longer verifies.
	wrongPub, _, _ := ed25519.GenerateKey(rand.Reader)
	_ = goodPub

	svc := NewService(Options{LicensePath: path, PublicKey: wrongPub})
	if svc.State() != StateInvalid {
		t.Fatalf("state = %s, want invalid", svc.State())
	}
}

func TestFingerprintMismatch(t *testing.T) {
	claims := Claims{
		CustomerID:   "acme",
		MaxEndpoints: 10,
		Fingerprint:  "deadbeef",
		ExpiresAt:    time.Now().Add(30 * 24 * time.Hour),
	}
	pub, path := makeLicense(t, claims)

	svc := NewService(Options{
		LicensePath:         path,
		PublicKey:           pub,
		HardwareFingerprint: "different",
	})
	if svc.State() != StateInvalid {
		t.Fatalf("state = %s, want invalid", svc.State())
	}
}

func TestMissingLicensePermissive(t *testing.T) {
	svc := NewService(Options{
		LicensePath:  "/nonexistent/license.json",
		AllowMissing: true,
	})
	if svc.State() != StateValid {
		t.Fatalf("state = %s, want valid (permissive)", svc.State())
	}
	if svc.Claims().CustomerID != "dev-permissive" {
		t.Errorf("customer_id = %q, want dev-permissive", svc.Claims().CustomerID)
	}
}

func TestMissingLicenseStrict(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	svc := NewService(Options{
		LicensePath:  "/nonexistent/license.json",
		PublicKey:    pub,
		AllowMissing: false,
	})
	if svc.State() != StateMissing {
		t.Fatalf("state = %s, want missing", svc.State())
	}
}

func TestCanonicalizeDeterministic(t *testing.T) {
	// Same claims must produce identical canonical bytes regardless
	// of feature slice ordering.
	a := Claims{
		CustomerID:   "x",
		Tier:         TierBusiness,
		MaxEndpoints: 10,
		Features:     []string{"uba", "ocr", "hris"},
		IssuedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		ExpiresAt:    time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	b := a
	b.Features = []string{"hris", "ocr", "uba"} // shuffled

	ca, _ := canonicalize(a)
	cb, _ := canonicalize(b)
	if string(ca) != string(cb) {
		t.Errorf("canonicalize not deterministic\na=%s\nb=%s", ca, cb)
	}
}

func TestOnlineClientDisabled(t *testing.T) {
	c := NewOnlineClient("", nil)
	status, valid, err := c.Validate(context.Background(), Heartbeat{CustomerID: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !valid || !status.Valid {
		t.Error("empty URL should yield valid-disabled status")
	}
}

func TestComputeFingerprint(t *testing.T) {
	fp1 := ComputeFingerprint("machine-1", "aa:bb:cc:dd:ee:ff")
	fp2 := ComputeFingerprint("machine-1", "aa:bb:cc:dd:ee:ff")
	fp3 := ComputeFingerprint("machine-2", "aa:bb:cc:dd:ee:ff")

	if fp1 != fp2 {
		t.Error("same inputs → different fingerprint")
	}
	if fp1 == fp3 {
		t.Error("different machine-id → same fingerprint")
	}
	if len(fp1) != 64 {
		t.Errorf("fingerprint len = %d, want 64 hex chars", len(fp1))
	}
}
