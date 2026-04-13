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

// LoginWithAppRole authenticates against auth/approle/login with the
// supplied role_id + secret_id and returns a NEW vault SDK client whose
// token is the freshly issued single-use enrollment token. The returned
// client shares TLS configuration with the receiver but has an
// independent token, so subsequent operations against it will not stomp
// on the Admin API's own AppRole token.
//
// This is the entry point used by the unauthenticated POST
// /v1/agent-enroll handler. The token associated with the returned
// client should have just enough policy to call pki/sign/agent-cert
// once and nothing else (see policy agent-enroll.hcl).
func (c *Client) LoginWithAppRole(ctx context.Context, roleID, secretID string) (*vaultapi.Client, error) {
	// Clone the underlying configuration so the new client inherits the
	// same address + TLS settings without sharing the token.
	clone, err := c.raw.Clone()
	if err != nil {
		return nil, fmt.Errorf("vault: clone client: %w", err)
	}
	// Drop any inherited token before login so a stale token cannot mask
	// a real auth failure.
	clone.SetToken("")

	secret, err := clone.Logical().WriteWithContext(ctx, "auth/approle/login", map[string]interface{}{
		"role_id":   roleID,
		"secret_id": secretID,
	})
	if err != nil {
		return nil, fmt.Errorf("vault: approle login: %w", err)
	}
	if secret == nil || secret.Auth == nil || secret.Auth.ClientToken == "" {
		return nil, fmt.Errorf("vault: approle login returned no client token")
	}
	clone.SetToken(secret.Auth.ClientToken)
	return clone, nil
}

// IssuedAgentCert is the result of signing an agent CSR via the PKI
// engine. CertificatePEM is the leaf certificate, CAChainPEM is the
// concatenated PEM chain (root + any intermediates) suitable for
// embedding in the agent's pinned trust store.
type IssuedAgentCert struct {
	CertificatePEM string
	CAChainPEM     string
	SerialNumber   string
	NotAfter       time.Time
}

// SignAgentCSR calls pki/sign/agent-cert on the provided client (which
// must be authenticated as the agent-enrollment AppRole) with the given
// CSR PEM and common name. ttl is e.g. "720h" (30 days). Returns the
// signed leaf certificate, the CA chain, the serial number, and the
// not_after timestamp parsed from Vault's response.
//
// The signing client is passed in (rather than using c.raw) because the
// admin API's own root token must NEVER be used to sign agent certs —
// only the single-use enrollment token has the right scoped policy.
func (c *Client) SignAgentCSR(ctx context.Context, signClient *vaultapi.Client, csrPEM, commonName, ttl string) (*IssuedAgentCert, error) {
	if signClient == nil {
		return nil, fmt.Errorf("vault: sign agent csr: nil sign client")
	}
	secret, err := signClient.Logical().WriteWithContext(ctx, "pki/sign/agent-cert", map[string]interface{}{
		"csr":         csrPEM,
		"common_name": commonName,
		"ttl":         ttl,
		"format":      "pem",
	})
	if err != nil {
		return nil, fmt.Errorf("vault: pki sign: %w", err)
	}
	if secret == nil || secret.Data == nil {
		return nil, fmt.Errorf("vault: pki sign: nil response")
	}

	certPEM, ok := secret.Data["certificate"].(string)
	if !ok || certPEM == "" {
		return nil, fmt.Errorf("vault: pki sign: missing certificate")
	}
	serial, _ := secret.Data["serial_number"].(string)

	// CA chain. Vault returns either ca_chain ([]interface{}) or just
	// issuing_ca (string). Concatenate every PEM block we can find.
	var chain strings.Builder
	switch v := secret.Data["ca_chain"].(type) {
	case []interface{}:
		for _, e := range v {
			if s, ok := e.(string); ok && s != "" {
				if chain.Len() > 0 && !strings.HasSuffix(chain.String(), "\n") {
					chain.WriteByte('\n')
				}
				chain.WriteString(s)
			}
		}
	}
	if chain.Len() == 0 {
		if issuing, ok := secret.Data["issuing_ca"].(string); ok && issuing != "" {
			chain.WriteString(issuing)
		}
	}

	// not_after parses RFC3339 from Vault.
	var notAfter time.Time
	if s, ok := secret.Data["expiration"].(string); ok && s != "" {
		// Some Vault versions emit Unix int instead — fall through to
		// the json.Number path below if RFC3339 parse fails.
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			notAfter = t
		}
	}
	if notAfter.IsZero() {
		// expiration is sometimes a JSON number (Unix seconds).
		switch v := secret.Data["expiration"].(type) {
		case float64:
			notAfter = time.Unix(int64(v), 0).UTC()
		}
	}

	return &IssuedAgentCert{
		CertificatePEM: certPEM,
		CAChainPEM:     chain.String(),
		SerialNumber:   serial,
		NotAfter:       notAfter,
	}, nil
}

// RevokeCert revokes an issued PKI leaf by serial number via pki/revoke.
// The serial is accepted in either Vault's colon-separated form
// ("a1:b2:...") or the contiguous lowercase hex we store in
// endpoints.cert_serial; the function normalises to the colon form
// because Vault's pki/revoke endpoint is picky about the layout.
//
// This call uses the Admin API's own Vault token — not the single-use
// agent-enrollment AppRole — so the policy attached to that token must
// include `pki/revoke` (see infra/compose/vault/policies/api.hcl).
// A non-nil error is returned on any Vault failure; callers should
// treat revoke as best-effort and log at WARN on failure (the CRL is
// eventually consistent and the refresh path must still succeed).
func (c *Client) RevokeCert(ctx context.Context, serial string) error {
	if serial == "" {
		return fmt.Errorf("vault: revoke: empty serial")
	}
	// Normalise contiguous hex "a1b2..." → colon form "a1:b2:..." so
	// Vault's serial matcher is happy. If the caller already passed the
	// colon form we leave it alone.
	normalized := serial
	if !strings.ContainsRune(normalized, ':') {
		var b strings.Builder
		b.Grow(len(normalized) + len(normalized)/2)
		for i := 0; i < len(normalized); i += 2 {
			if i > 0 {
				b.WriteByte(':')
			}
			end := i + 2
			if end > len(normalized) {
				end = len(normalized)
			}
			b.WriteString(normalized[i:end])
		}
		normalized = b.String()
	}
	secret, err := c.raw.Logical().WriteWithContext(ctx, "pki/revoke", map[string]interface{}{
		"serial_number": normalized,
	})
	if err != nil {
		return fmt.Errorf("vault: pki revoke %q: %w", normalized, err)
	}
	// Vault returns revocation_time on success. Missing it is not a
	// hard error (older Vault versions omit it) but a nil secret is.
	if secret == nil {
		return fmt.Errorf("vault: pki revoke %q: nil response", normalized)
	}
	return nil
}

// DestroyTransitKey is the crypto-erase primitive used by the DSR
// fulfilment service (Faz 6 #69). It forces destruction of a Vault
// transit key, which — combined with deletion of the per-user PE-DEK
// material in the backend stores — renders any remaining ciphertext
// mathematically unrecoverable. This is the KVKK m.7 "right to
// erasure" technical control: we cannot rewind tape backups, but by
// destroying the wrapping key we guarantee the ciphertext they contain
// is forever unreadable.
//
// Vault rejects DELETE on transit keys unless the key's configuration
// has deletion_allowed=true. This endpoint assumes the DSR fulfilment
// workflow has already put the key into that state via an
// out-of-API runbook step OR that the PE-DEK keys were created with
// deletion_allowed=true from the start (recommended — see ADR 0013 A4).
//
// A non-nil error means the crypto-erase did NOT happen; callers MUST
// NOT mark the DSR as "fulfilled" in that case — the auditor needs to
// see the partial state.
func (c *Client) DestroyTransitKey(ctx context.Context, keyName string) error {
	if keyName == "" {
		return fmt.Errorf("vault: destroy transit key: empty key name")
	}
	path := fmt.Sprintf("transit/keys/%s", keyName)
	if _, err := c.raw.Logical().DeleteWithContext(ctx, path); err != nil {
		return fmt.Errorf("vault: destroy transit key %q: %w", keyName, err)
	}
	return nil
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
