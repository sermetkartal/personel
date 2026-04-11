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
