// Package vault — Vault client for signing and secret operations.
// The Admin API uses:
//   - transit/keys/control-plane-signing for Ed25519 signing
//   - AppRole auth (role_id + secret_id via systemd credentials)
//   - agent-enrollment AppRole (single-use Secret ID per enrollment)
//
// The Admin API does NOT have access to:
//   - transit/keys/tenant/*/tmk (DLP-only)
//   - kv/data/crypto/* (DLP-only)
package vault

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	vaultapi "github.com/hashicorp/vault/api"
)

// Client wraps the Vault SDK client with token renewal.
type Client struct {
	raw                    *vaultapi.Client
	controlPlaneSigningKey string
	renewMu                sync.Mutex
	renewInterval          time.Duration
	log                    *slog.Logger
	// stubMode disables real Vault calls; used by NewStubClient in tests.
	stubMode bool
}

// NewClient authenticates with Vault via AppRole and returns a Client.
func NewClient(ctx context.Context, addr, roleID, secretID, caPath, signingKey string, renewInterval time.Duration, log *slog.Logger) (*Client, error) {
	cfg := vaultapi.DefaultConfig()
	cfg.Address = addr

	if caPath != "" {
		if err := cfg.ConfigureTLS(&vaultapi.TLSConfig{CACert: caPath}); err != nil {
			return nil, fmt.Errorf("vault: configure tls: %w", err)
		}
	}

	raw, err := vaultapi.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("vault: new client: %w", err)
	}

	// AppRole login.
	data := map[string]interface{}{
		"role_id":   roleID,
		"secret_id": secretID,
	}
	secret, err := raw.Logical().WriteWithContext(ctx, "auth/approle/login", data)
	if err != nil {
		return nil, fmt.Errorf("vault: approle login: %w", err)
	}
	if secret == nil || secret.Auth == nil {
		return nil, fmt.Errorf("vault: approle login returned no auth")
	}
	raw.SetToken(secret.Auth.ClientToken)

	c := &Client{
		raw:                    raw,
		controlPlaneSigningKey: signingKey,
		renewInterval:          renewInterval,
		log:                    log,
	}

	return c, nil
}

// StartRenewal starts a background goroutine that renews the token.
// Returns when ctx is cancelled.
func (c *Client) StartRenewal(ctx context.Context) {
	ticker := time.NewTicker(c.renewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.renewMu.Lock()
			_, err := c.raw.Auth().Token().RenewSelfWithContext(ctx, 0)
			c.renewMu.Unlock()
			if err != nil {
				c.log.Error("vault: token renewal failed", slog.Any("error", err))
			} else {
				c.log.Debug("vault: token renewed")
			}
		}
	}
}

// Sign implements the audit.CheckpointSigner interface (and any other
// interface that expects a generic Sign(ctx, payload) signer). It delegates
// to SignWithControlKey.
func (c *Client) Sign(ctx context.Context, payload []byte) ([]byte, string, error) {
	return c.SignWithControlKey(ctx, payload)
}

// SignWithControlKey signs the payload with the control-plane Ed25519 signing key.
// Returns (signature_bytes, key_version_id, error).
func (c *Client) SignWithControlKey(ctx context.Context, payload []byte) ([]byte, string, error) {
	if c.stubMode {
		return c.overrideSignWithControlKey(ctx, payload)
	}
	encoded := base64.StdEncoding.EncodeToString(payload)
	path := fmt.Sprintf("transit/sign/%s", c.controlPlaneSigningKey)

	secret, err := c.raw.Logical().WriteWithContext(ctx, path, map[string]interface{}{
		"input":          encoded,
		"prehashed":      false,
		"signature_algorithm": "pkcs1v15", // Ed25519 ignores this but Vault requires the field
		"marshaling_algorithm": "asn1",
	})
	if err != nil {
		return nil, "", fmt.Errorf("vault: sign: %w", err)
	}
	if secret == nil || secret.Data == nil {
		return nil, "", fmt.Errorf("vault: sign returned nil data")
	}

	sigStr, ok := secret.Data["signature"].(string)
	if !ok {
		return nil, "", fmt.Errorf("vault: sign: unexpected signature type")
	}

	// Vault returns "vault:v1:<base64>" format.
	const prefix = "vault:v1:"
	if len(sigStr) <= len(prefix) {
		return nil, "", fmt.Errorf("vault: sign: unexpected signature format")
	}
	sigBytes, err := base64.StdEncoding.DecodeString(sigStr[len(prefix):])
	if err != nil {
		return nil, "", fmt.Errorf("vault: sign: decode signature: %w", err)
	}

	keyVersion := "v1"
	if kv, ok := secret.Data["key_version"].(float64); ok {
		keyVersion = fmt.Sprintf("v%d", int(kv))
	}

	return sigBytes, fmt.Sprintf("%s:%s", c.controlPlaneSigningKey, keyVersion), nil
}

// Verify checks a signature produced by Sign against the Vault transit
// verify endpoint. Satisfies the evidence.Verifier interface — the same
// key history that retains v1 after a rotation to v2/v3 is what makes
// this viable for the 5-year evidence retention window.
//
// keyVersion is the combined identifier Sign returned, e.g.
// "control-plane-signing:v3". This function parses out the key name and
// version, reconstructs Vault's "vault:v3:<base64>" wire format, and
// calls transit/verify/{keyName}. Returns nil on a valid signature or a
// descriptive error on any failure (mismatch, unknown version, network).
//
// Operators MUST NOT set min_decryption_version past any version that
// may still have live evidence items, or this endpoint will fail for
// those items and the SOC 2 chain breaks.
func (c *Client) Verify(ctx context.Context, payload, signature []byte, keyVersion string) error {
	if c.stubMode {
		return c.overrideVerify(ctx, payload, signature, keyVersion)
	}

	keyName, versionNum, err := parseKeyVersion(keyVersion)
	if err != nil {
		return fmt.Errorf("vault: verify: %w", err)
	}

	input := base64.StdEncoding.EncodeToString(payload)
	sigWire := fmt.Sprintf("vault:v%d:%s", versionNum, base64.StdEncoding.EncodeToString(signature))
	path := fmt.Sprintf("transit/verify/%s", keyName)

	secret, err := c.raw.Logical().WriteWithContext(ctx, path, map[string]interface{}{
		"input":                input,
		"signature":            sigWire,
		"prehashed":            false,
		"signature_algorithm":  "pkcs1v15",
		"marshaling_algorithm": "asn1",
	})
	if err != nil {
		return fmt.Errorf("vault: verify %q: %w", keyVersion, err)
	}
	if secret == nil || secret.Data == nil {
		return fmt.Errorf("vault: verify %q returned nil data", keyVersion)
	}

	valid, ok := secret.Data["valid"].(bool)
	if !ok {
		return fmt.Errorf("vault: verify %q returned unexpected response shape", keyVersion)
	}
	if !valid {
		return fmt.Errorf("vault: verify %q: signature invalid", keyVersion)
	}
	return nil
}

// parseKeyVersion splits "name:vN" into ("name", N). The combined format
// is what Sign returns so call sites can pass it back to Verify verbatim.
func parseKeyVersion(s string) (string, int, error) {
	// Last colon is the separator to allow key names that contain ':'
	// (unusual but Vault does not forbid them).
	i := strings.LastIndex(s, ":")
	if i <= 0 || i == len(s)-1 {
		return "", 0, fmt.Errorf("invalid key version format %q (want name:vN)", s)
	}
	name := s[:i]
	ver := s[i+1:]
	if len(ver) < 2 || ver[0] != 'v' {
		return "", 0, fmt.Errorf("invalid key version format %q (want name:vN)", s)
	}
	var n int
	if _, err := fmt.Sscanf(ver[1:], "%d", &n); err != nil || n <= 0 {
		return "", 0, fmt.Errorf("invalid key version number in %q", s)
	}
	return name, n, nil
}

// IssueEnrollmentSecretID issues a single-use enrollment Secret ID for an agent.
// This uses the agent-enrollment AppRole, NOT the admin-api AppRole.
func (c *Client) IssueEnrollmentSecretID(ctx context.Context) (string, error) {
	secret, err := c.raw.Logical().WriteWithContext(ctx, "auth/approle/role/agent-enrollment/secret-id", map[string]interface{}{
		"metadata": `{"source":"admin-api-enrollment"}`,
	})
	if err != nil {
		return "", fmt.Errorf("vault: issue enrollment secret id: %w", err)
	}
	if secret == nil || secret.Data == nil {
		return "", fmt.Errorf("vault: issue enrollment secret id: nil response")
	}
	sid, ok := secret.Data["secret_id"].(string)
	if !ok {
		return "", fmt.Errorf("vault: issue enrollment secret id: unexpected type")
	}
	return sid, nil
}

// GetEnrollmentRoleID returns the agent-enrollment AppRole role_id.
func (c *Client) GetEnrollmentRoleID(ctx context.Context) (string, error) {
	secret, err := c.raw.Logical().ReadWithContext(ctx, "auth/approle/role/agent-enrollment/role-id")
	if err != nil {
		return "", fmt.Errorf("vault: get enrollment role id: %w", err)
	}
	if secret == nil || secret.Data == nil {
		return "", fmt.Errorf("vault: get enrollment role id: nil response")
	}
	rid, ok := secret.Data["role_id"].(string)
	if !ok {
		return "", fmt.Errorf("vault: get enrollment role id: unexpected type")
	}
	return rid, nil
}
