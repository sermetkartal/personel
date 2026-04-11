// Package policy — policy signing and NATS publication.
//
// Policies are signed with the control-plane Ed25519 key fetched from Vault at
// startup. The signed bundle is published to NATS subject
// "policy.v1.<tenant>.<endpoint>" for gateway → agent delivery.
package policy

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/personel/api/internal/nats"
	"github.com/personel/api/internal/vault"
)

// Publisher signs and publishes policy bundles.
type Publisher struct {
	nats        *nats.Publisher
	vaultClient *vault.Client
	subject     string
	log         *slog.Logger
}

// NewPublisher creates a Publisher.
func NewPublisher(n *nats.Publisher, vc *vault.Client, subject string, log *slog.Logger) *Publisher {
	return &Publisher{nats: n, vaultClient: vc, subject: subject, log: log}
}

// PolicyBundle is the signed payload sent to agents.
type PolicyBundle struct {
	Version      int64           `json:"version"`
	TenantID     string          `json:"tenant_id"`
	EndpointID   string          `json:"endpoint_id,omitempty"` // empty = broadcast to all
	PolicyID     string          `json:"policy_id"`
	Rules        json.RawMessage `json:"rules"`
	SignedAt     int64           `json:"signed_at"` // Unix UTC
	SigningKeyID string          `json:"signing_key_id"`
	Signature    []byte          `json:"signature"` // Ed25519 over canonical(bundle without signature)
}

// PublishToEndpoint signs the policy and publishes it.
func (p *Publisher) PublishToEndpoint(ctx context.Context, tenantID, endpointID, policyID string, rules json.RawMessage, version int64) error {
	bundle := PolicyBundle{
		Version:    version,
		TenantID:   tenantID,
		EndpointID: endpointID,
		PolicyID:   policyID,
		Rules:      rules,
		SignedAt:   nats.NowUnix(),
	}

	// Canonical JSON of everything except signature.
	payload, err := canonicalBundleBytes(bundle)
	if err != nil {
		return fmt.Errorf("policy: canonical: %w", err)
	}

	sig, keyID, err := p.vaultClient.SignWithControlKey(ctx, payload)
	if err != nil {
		return fmt.Errorf("policy: sign: %w", err)
	}
	bundle.Signature = sig
	bundle.SigningKeyID = keyID

	bundleBytes, err := json.Marshal(bundle)
	if err != nil {
		return fmt.Errorf("policy: marshal bundle: %w", err)
	}

	subject := fmt.Sprintf("%s.%s.%s", p.subject, tenantID, endpointID)
	if err := p.nats.Publish(ctx, subject, bundleBytes); err != nil {
		return fmt.Errorf("policy: nats publish: %w", err)
	}

	p.log.Info("policy: published",
		slog.String("tenant_id", tenantID),
		slog.String("endpoint_id", endpointID),
		slog.String("policy_id", policyID),
		slog.Int64("version", version),
	)
	return nil
}

// PublishToAll broadcasts a policy to all endpoints of a tenant.
func (p *Publisher) PublishToAll(ctx context.Context, tenantID, policyID string, rules json.RawMessage, version int64) error {
	return p.PublishToEndpoint(ctx, tenantID, "*", policyID, rules, version)
}

func canonicalBundleBytes(b PolicyBundle) ([]byte, error) {
	// Zero out signature before hashing.
	b.Signature = nil
	b.SigningKeyID = ""
	return json.Marshal(b)
}

// Verify verifies a bundle's signature against the provided public key.
func Verify(bundle *PolicyBundle, pubKey ed25519.PublicKey) error {
	payload, err := canonicalBundleBytes(*bundle)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pubKey, payload, bundle.Signature) {
		return fmt.Errorf("policy: signature verification failed")
	}
	return nil
}
