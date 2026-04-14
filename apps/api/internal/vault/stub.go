// Package vault — stub client for testing.
//
// NewStubClient returns a Client configured to avoid any real Vault calls.
// It uses the stub sign path that returns deterministic fake signatures.
// ONLY for use in tests and integration tests.
package vault

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"

	vaultapi "github.com/hashicorp/vault/api"
)

// NewStubClient creates a vault Client whose SignWithControlKey returns a
// deterministic fake signature without connecting to a real Vault server.
// The returned client must NOT call StartRenewal or IssueEnrollmentSecretID.
func NewStubClient() *Client {
	// Construct a Client with a zeroed raw client (not connected).
	// Only SignWithControlKey is overrideable via the stubSign field.
	cfg := vaultapi.DefaultConfig()
	cfg.Address = "http://127.0.0.1:0" // unreachable — no calls expected
	raw, _ := vaultapi.NewClient(cfg)
	raw.SetToken("stub-token")

	return &Client{
		raw:                    raw,
		controlPlaneSigningKey: "stub-key",
		renewInterval:          0,
		log:                    slog.New(slog.NewTextHandler(os.Stderr, nil)),
		stubMode:               true,
	}
}

// signStub returns a deterministic 64-byte fake Ed25519 signature
// derived from a SHA-256 hash of the payload (two blocks concatenated).
// This is NOT a real Ed25519 signature — it is only for testing.
func signStub(payload []byte) []byte {
	h1 := sha256.Sum256(payload)
	h2 := sha256.Sum256(h1[:])
	out := make([]byte, 64)
	copy(out[:32], h1[:])
	copy(out[32:], h2[:])
	return out
}

// overrideSignWithControlKey is called by SignWithControlKey when stubMode is true.
func (c *Client) overrideSignWithControlKey(_ context.Context, payload []byte) ([]byte, string, error) {
	return signStub(payload), "stub-key:v1", nil
}

// overrideEncrypt is called by Encrypt when stubMode is true. Produces
// a deterministic "vault:v1:<base64(plaintext)>" string so that round-
// tripping through Decrypt recovers the original bytes. This is NOT
// real encryption — tests rely on the wire format only.
func (c *Client) overrideEncrypt(_ context.Context, _ string, plaintext []byte) ([]byte, int, error) {
	encoded := base64.StdEncoding.EncodeToString(plaintext)
	return []byte("vault:v1:" + encoded), 1, nil
}

// overrideDecrypt is called by Decrypt when stubMode is true. Strips
// the "vault:v1:" prefix and returns the decoded bytes.
func (c *Client) overrideDecrypt(_ context.Context, _ string, ciphertext []byte) ([]byte, error) {
	const prefix = "vault:v1:"
	s := string(ciphertext)
	if len(s) < len(prefix) || s[:len(prefix)] != prefix {
		return nil, fmt.Errorf("vault stub: unknown ciphertext format")
	}
	out, err := base64.StdEncoding.DecodeString(s[len(prefix):])
	if err != nil {
		return nil, fmt.Errorf("vault stub: decode: %w", err)
	}
	return out, nil
}

// overrideVerify is called by Verify when stubMode is true. Recomputes
// the deterministic stub signature and compares byte-for-byte. Only
// accepts "stub-key:v1" since the stub never rotates.
func (c *Client) overrideVerify(_ context.Context, payload, signature []byte, keyVersion string) error {
	if keyVersion != "stub-key:v1" {
		return fmt.Errorf("vault stub: unknown key version %q", keyVersion)
	}
	want := signStub(payload)
	if !bytes.Equal(want, signature) {
		return fmt.Errorf("vault stub: signature mismatch")
	}
	return nil
}
