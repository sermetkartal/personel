// Package vault wraps the HashiCorp Vault API client for the gateway's narrow
// use cases: cert serial deny-list reads and agent cert issuance (CsrSubmit).
// The DLP key hierarchy is NOT accessible from this client (see key-hierarchy.md).
package vault

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	vaultapi "github.com/hashicorp/vault/api"

	"github.com/personel/gateway/internal/config"
)

// Client is a Vault API wrapper with AppRole authentication and an in-memory
// cert serial deny-list cache (gateway-service policy scope only).
type Client struct {
	raw      *vaultapi.Client
	cfg      config.VaultConfig
	mu       sync.RWMutex
	denyList map[string]struct{}
}

// New creates a new Vault client and logs in with the configured AppRole
// credentials. The provided context is used only for the initial login.
func New(ctx context.Context, cfg config.VaultConfig) (*Client, error) {
	vcfg := vaultapi.DefaultConfig()
	vcfg.Address = cfg.Addr
	if cfg.CACert != "" {
		if err := vcfg.ConfigureTLS(&vaultapi.TLSConfig{CACert: cfg.CACert}); err != nil {
			return nil, fmt.Errorf("vault: configure TLS: %w", err)
		}
	}
	raw, err := vaultapi.NewClient(vcfg)
	if err != nil {
		return nil, fmt.Errorf("vault: new client: %w", err)
	}
	c := &Client{raw: raw, cfg: cfg, denyList: make(map[string]struct{})}
	if err := c.login(ctx); err != nil {
		return nil, fmt.Errorf("vault: initial login: %w", err)
	}
	return c, nil
}

// login authenticates with AppRole and sets the Vault client token.
func (c *Client) login(ctx context.Context) error {
	resp, err := c.raw.Logical().WriteWithContext(ctx, "auth/approle/login", map[string]interface{}{
		"role_id":   c.cfg.RoleID,
		"secret_id": c.cfg.SecretID,
	})
	if err != nil {
		return fmt.Errorf("vault: approle login: %w", err)
	}
	if resp == nil || resp.Auth == nil {
		return fmt.Errorf("vault: approle login returned no auth token")
	}
	c.raw.SetToken(resp.Auth.ClientToken)
	return nil
}

// RefreshDenyList fetches the current cert serial deny list from Vault KV v2
// at path kv/pki/deny-list. Called at startup and on NATS pki.v1.revoke events.
func (c *Client) RefreshDenyList(ctx context.Context) error {
	secret, err := c.raw.KVv2("kv").Get(ctx, "pki/deny-list")
	if err != nil {
		return fmt.Errorf("vault: read deny-list: %w", err)
	}
	if secret == nil {
		return nil
	}

	var serials []string
	switch v := secret.Data["serials"].(type) {
	case []interface{}:
		for _, s := range v {
			if str, ok := s.(string); ok {
				serials = append(serials, str)
			}
		}
	case string:
		if err := json.Unmarshal([]byte(v), &serials); err != nil {
			return fmt.Errorf("vault: parse deny-list serials JSON: %w", err)
		}
	}

	m := make(map[string]struct{}, len(serials))
	for _, s := range serials {
		m[s] = struct{}{}
	}
	c.mu.Lock()
	c.denyList = m
	c.mu.Unlock()
	return nil
}

// IsRevoked returns true if the given cert serial is on the in-memory deny list.
func (c *Client) IsRevoked(serial string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, ok := c.denyList[serial]
	return ok
}

// AddToDenyList adds a serial to the in-memory deny list immediately so that
// revocation takes effect in this process without waiting for the next
// RefreshDenyList cycle. The authoritative source is Vault KV; the Admin API
// owns writes there.
func (c *Client) AddToDenyList(serial string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.denyList[serial] = struct{}{}
}

// SignAgentCSR submits a PKCS#10 CSR to Vault PKI (agent intermediate) and
// returns the signed cert + chain. Used in the CsrSubmit → CsrResponse flow
// per the RotateCert protocol (mtls-pki.md §Rotation).
//
// tenantID must be the UUID string of the tenant whose Vault PKI mount is at
// pki/tenant/<tenantID>/agents.
func (c *Client) SignAgentCSR(ctx context.Context, tenantID string, csrDER []byte) (*SignedCert, error) {
	csrPEM := "-----BEGIN CERTIFICATE REQUEST-----\n" +
		base64.StdEncoding.EncodeToString(csrDER) +
		"\n-----END CERTIFICATE REQUEST-----\n"

	path := fmt.Sprintf("pki/tenant/%s/agents/sign/endpoint", tenantID)
	secret, err := c.raw.Logical().WriteWithContext(ctx, path, map[string]interface{}{
		"csr":    csrPEM,
		"ttl":    "336h", // 14 days per mtls-pki.md
		"format": "pem",
	})
	if err != nil {
		return nil, fmt.Errorf("vault: sign agent CSR: %w", err)
	}
	if secret == nil {
		return nil, fmt.Errorf("vault: sign agent CSR returned nil")
	}

	certPEM, _ := secret.Data["certificate"].(string)

	var expiration int64
	switch v := secret.Data["expiration"].(type) {
	case json.Number:
		expiration, _ = v.Int64()
	case float64:
		expiration = int64(v)
	}

	sc := &SignedCert{
		CertPEM:  []byte(certPEM),
		NotAfter: time.Unix(expiration, 0),
	}
	if caChainRaw, ok := secret.Data["ca_chain"].([]interface{}); ok {
		for _, item := range caChainRaw {
			if s, ok := item.(string); ok {
				sc.ChainPEM = append(sc.ChainPEM, []byte(s))
			}
		}
	}
	return sc, nil
}

// SignedCert carries the results of a Vault PKI sign operation.
type SignedCert struct {
	CertPEM  []byte
	ChainPEM [][]byte
	NotAfter time.Time
}
