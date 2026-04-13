// Package endpoint — unit tests for the enrollment token format and
// the CSR validation helpers. Full DB+Vault integration is covered by
// apps/api/test/integration; these tests pin down the pure helpers so
// a typo in the base64 alphabet or the CSR PEM-type matching can't
// slip through.
package endpoint

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
)

func TestEncodeEnrollmentToken_RoundTrip(t *testing.T) {
	t.Parallel()

	payload := enrollmentTokenPayload{
		RoleID:    "3737d80c-7b47-07df-9c36-20d68b628f6e",
		SecretID:  "s.abcdef0123456789",
		EnrollURL: "https://api.personel.example.com/v1/agent-enroll",
	}
	encoded, err := encodeEnrollmentToken(payload)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if encoded == "" {
		t.Fatal("encode: empty result")
	}
	// base64 url-no-padding: no '+', no '/', no '=' characters.
	if strings.ContainsAny(encoded, "+/=") {
		t.Errorf("encode: wrong alphabet, got %q", encoded)
	}
	// Decode and verify the JSON round-trips every field.
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	var got enrollmentTokenPayload
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != payload {
		t.Errorf("round trip mismatch:\n got: %+v\nwant: %+v", got, payload)
	}
}

func TestEncodeEnrollmentToken_TenantNotInPayload(t *testing.T) {
	t.Parallel()
	// Defence-in-depth: the client MUST NOT be able to inject a
	// tenant_id via the opaque token. enrollmentTokenPayload has three
	// fields; any future addition that looks tenant-ish should force a
	// deliberate test update so the security story stays explicit.
	payload := enrollmentTokenPayload{
		RoleID:    "role",
		SecretID:  "secret",
		EnrollURL: "https://example/v1/agent-enroll",
	}
	encoded, _ := encodeEnrollmentToken(payload)
	raw, _ := base64.RawURLEncoding.DecodeString(encoded)
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"tenant_id", "tenant", "issued_for_tenant"} {
		if _, bad := m[k]; bad {
			t.Errorf("token payload must not carry %q — tenant binding is server-side only", k)
		}
	}
	// Exactly the three expected fields.
	want := map[string]bool{"role_id": true, "secret_id": true, "enroll_url": true}
	if len(m) != len(want) {
		t.Errorf("token payload has %d fields, want %d", len(m), len(want))
	}
	for k := range m {
		if !want[k] {
			t.Errorf("unexpected token payload field %q", k)
		}
	}
}

func TestFormatSerialHex(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"colon vault form", "a1:B2:C3:d4", "a1b2c3d4"},
		{"already contiguous", "a1b2c3d4", "a1b2c3d4"},
		{"upper contiguous", "A1B2C3D4", "a1b2c3d4"},
		{"non-hex preserved", "not-a-serial!", "not-a-serial!"},
	}
	for _, c := range cases {
		got := formatSerialHex(c.in)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// generateTestCSR builds a valid P-256 CSR for use in CSR validation
// tests. Returns the PEM block.
func generateTestCSR(t *testing.T, commonName string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: commonName},
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, key)
	if err != nil {
		t.Fatalf("create csr: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
}

func TestParseAndVerifyCSR_Valid(t *testing.T) {
	t.Parallel()
	pemStr := generateTestCSR(t, "test-host.personel.internal")
	csr, pin, err := parseAndVerifyCSR(pemStr)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if csr == nil {
		t.Fatal("csr nil")
	}
	if pin == "" {
		t.Error("spki pin empty")
	}
	// SPKI pin must be a base64 std (44 chars for a SHA-256).
	if decoded, err := base64.StdEncoding.DecodeString(pin); err != nil || len(decoded) != 32 {
		t.Errorf("spki pin should decode to 32 bytes, got err=%v len=%d", err, len(decoded))
	}
}

func TestParseAndVerifyCSR_Invalid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"garbage", "not a PEM block"},
		{"wrong PEM type", "-----BEGIN FOO-----\nMIIB\n-----END FOO-----\n"},
	}
	for _, c := range cases {
		if _, _, err := parseAndVerifyCSR(c.in); err == nil {
			t.Errorf("%s: expected error, got nil", c.name)
		}
	}
}

func TestRequestValidate(t *testing.T) {
	t.Parallel()
	good := &AgentEnrollRequest{
		RoleID:   "r",
		SecretID: "s",
		CSRPEM:   "pem",
		Hostname: "host",
	}
	if err := good.validate(); err != nil {
		t.Errorf("good: unexpected error %v", err)
	}
	bad := []*AgentEnrollRequest{
		{SecretID: "s", CSRPEM: "pem", Hostname: "h"},               // missing role_id
		{RoleID: "r", CSRPEM: "pem", Hostname: "h"},                 // missing secret_id
		{RoleID: "r", SecretID: "s", Hostname: "h"},                 // missing csr
		{RoleID: "r", SecretID: "s", CSRPEM: "p"},                   // missing hostname
		{RoleID: "r", SecretID: "s", CSRPEM: "p", Hostname: strings.Repeat("a", 300)},
	}
	for i, b := range bad {
		if err := b.validate(); err == nil {
			t.Errorf("bad[%d]: expected error", i)
		}
	}
	var nilReq *AgentEnrollRequest
	if err := nilReq.validate(); err == nil {
		t.Error("nil request should be rejected")
	}
}
