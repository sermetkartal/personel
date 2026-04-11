// Package vault provides the Vault client used by livrec-service.
//
// This client handles two operations:
//   - LVMK derivation: transit/derive/lvmk-<tenant> (separate from TMK)
//   - Control-plane signing: transit/sign/control-plane-signer (for export manifests)
//
// Per ADR 0019 §Key hierarchy: this AppRole ("live-view-recorder") has NO access
// to transit/keys/tenant/*/tmk, which is strictly dlp-service only.
// Per ADR 0009: zero cross-contamination between LVMK and TMK.
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
// Thread-safe; all exported methods are safe for concurrent use.
type Client struct {
	raw             *vaultapi.Client
	lvmkPath        string // e.g. "transit/derive/lvmk"
	signerPath      string // e.g. "transit/sign/control-plane-signer"
	renewInterval   time.Duration
	renewMu         sync.Mutex
	log             *slog.Logger
}

// NewClient authenticates with Vault via AppRole and returns a ready Client.
// addr, roleID, secretID are required. caPath may be empty for insecure-TLS
// environments (dev only; production must supply CA).
func NewClient(
	ctx context.Context,
	addr, roleID, secretID, caPath string,
	lvmkPath, signerPath string,
	renewInterval time.Duration,
	log *slog.Logger,
) (*Client, error) {
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

	secret, err := raw.Logical().WriteWithContext(ctx, "auth/approle/login", map[string]interface{}{
		"role_id":   roleID,
		"secret_id": secretID,
	})
	if err != nil {
		return nil, fmt.Errorf("vault: approle login: %w", err)
	}
	if secret == nil || secret.Auth == nil {
		return nil, fmt.Errorf("vault: approle login: no auth returned")
	}
	raw.SetToken(secret.Auth.ClientToken)

	return &Client{
		raw:           raw,
		lvmkPath:      lvmkPath,
		signerPath:    signerPath,
		renewInterval: renewInterval,
		log:           log,
	}, nil
}

// StartRenewal runs a background token-renewal loop until ctx is cancelled.
// Call this in a goroutine after NewClient.
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
			}
		}
	}
}

// DeriveLVMK derives a 256-bit session key for the given tenant using the
// LVMK transit derive path. The context string is the canonical HKDF context:
//
//	session_id || tenant_id || "lv-session-dek-v1"
//
// Per ADR 0019: the derived key is held only in memory; the wrapped form is
// stored in Postgres by the caller via DerivedWrap.
func (c *Client) DeriveLVMK(ctx context.Context, tenantID, derivationContext string) ([]byte, int, error) {
	path := fmt.Sprintf("%s-%s", c.lvmkPath, tenantID)

	secret, err := c.raw.Logical().WriteWithContext(ctx, path, map[string]interface{}{
		"context":    base64.StdEncoding.EncodeToString([]byte(derivationContext)),
		"key_length": 32,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("vault: lvmk derive: %w", err)
	}
	if secret == nil || secret.Data == nil {
		return nil, 0, fmt.Errorf("vault: lvmk derive: nil response")
	}

	derivedStr, ok := secret.Data["derived_key"].(string)
	if !ok {
		return nil, 0, fmt.Errorf("vault: lvmk derive: missing derived_key field")
	}
	keyBytes, err := base64.StdEncoding.DecodeString(derivedStr)
	if err != nil {
		return nil, 0, fmt.Errorf("vault: lvmk derive: decode derived_key: %w", err)
	}
	if len(keyBytes) != 32 {
		return nil, 0, fmt.Errorf("vault: lvmk derive: expected 32 bytes, got %d", len(keyBytes))
	}

	version := 1
	if v, ok := secret.Data["key_version"].(float64); ok {
		version = int(v)
	}

	return keyBytes, version, nil
}

// WrapDEK wraps a 32-byte plaintext DEK under the LVMK for a tenant,
// returning the Vault-wrapped ciphertext suitable for storage in Postgres.
// The wrapped form can later be unwrapped by calling UnwrapDEK.
func (c *Client) WrapDEK(ctx context.Context, tenantID string, dek []byte) (string, error) {
	encPath := fmt.Sprintf("transit/encrypt/lvmk-%s", tenantID)
	secret, err := c.raw.Logical().WriteWithContext(ctx, encPath, map[string]interface{}{
		"plaintext": base64.StdEncoding.EncodeToString(dek),
	})
	if err != nil {
		return "", fmt.Errorf("vault: wrap dek: %w", err)
	}
	if secret == nil || secret.Data == nil {
		return "", fmt.Errorf("vault: wrap dek: nil response")
	}
	wrapped, ok := secret.Data["ciphertext"].(string)
	if !ok {
		return "", fmt.Errorf("vault: wrap dek: missing ciphertext")
	}
	return wrapped, nil
}

// UnwrapDEK decrypts a Vault-wrapped DEK ciphertext back to the 32-byte
// plaintext session key. Used during playback after dual-control approval.
// The plaintext key must be zeroed by the caller after use.
func (c *Client) UnwrapDEK(ctx context.Context, tenantID, wrappedDEK string) ([]byte, error) {
	decPath := fmt.Sprintf("transit/decrypt/lvmk-%s", tenantID)
	secret, err := c.raw.Logical().WriteWithContext(ctx, decPath, map[string]interface{}{
		"ciphertext": wrappedDEK,
	})
	if err != nil {
		return nil, fmt.Errorf("vault: unwrap dek: %w", err)
	}
	if secret == nil || secret.Data == nil {
		return nil, fmt.Errorf("vault: unwrap dek: nil response")
	}
	ptStr, ok := secret.Data["plaintext"].(string)
	if !ok {
		return nil, fmt.Errorf("vault: unwrap dek: missing plaintext")
	}
	dek, err := base64.StdEncoding.DecodeString(ptStr)
	if err != nil {
		return nil, fmt.Errorf("vault: unwrap dek: decode: %w", err)
	}
	return dek, nil
}

// SignManifest signs payload bytes with the control-plane signing key.
// Used exclusively by export/forensic.go. Returns (signatureBytes, keyVersionID, error).
func (c *Client) SignManifest(ctx context.Context, payload []byte) ([]byte, string, error) {
	encoded := base64.StdEncoding.EncodeToString(payload)
	secret, err := c.raw.Logical().WriteWithContext(ctx, c.signerPath, map[string]interface{}{
		"input":                encoded,
		"prehashed":            false,
		"marshaling_algorithm": "asn1",
	})
	if err != nil {
		return nil, "", fmt.Errorf("vault: sign manifest: %w", err)
	}
	if secret == nil || secret.Data == nil {
		return nil, "", fmt.Errorf("vault: sign manifest: nil response")
	}
	sigStr, ok := secret.Data["signature"].(string)
	if !ok {
		return nil, "", fmt.Errorf("vault: sign manifest: missing signature")
	}

	const prefix = "vault:v1:"
	if len(sigStr) <= len(prefix) {
		return nil, "", fmt.Errorf("vault: sign manifest: unexpected format")
	}
	sigBytes, err := base64.StdEncoding.DecodeString(sigStr[len(prefix):])
	if err != nil {
		return nil, "", fmt.Errorf("vault: sign manifest: decode: %w", err)
	}

	ver := "v1"
	if kv, ok := secret.Data["key_version"].(float64); ok {
		ver = fmt.Sprintf("v%d", int(kv))
	}
	return sigBytes, ver, nil
}
