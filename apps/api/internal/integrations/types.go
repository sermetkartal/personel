// Package integrations implements the per-tenant third-party service
// credential vault surfaced through the console settings page (Wave 9
// Sprint 3A). Supported services are explicitly allowlisted below —
// free-form service names are rejected by the service layer so a typo
// cannot silently create a dead row that no collector reads.
//
// Every config is encrypted via Vault transit (key `integrations`) and
// only the ciphertext + key_version lands in Postgres. The Decrypt path
// is restricted to internal packages (collectors that need plaintext
// credentials at dial time) — the public GET endpoints always return
// masked shapes so plaintext never crosses the JSON boundary.
package integrations

import (
	"time"

	"github.com/google/uuid"
)

// Service names allowlisted for integrations. Any value not in this
// map is rejected by Upsert at the service boundary.
var AllowedServices = map[string]struct{}{
	"maxmind":    {},
	"cloudflare": {},
	"pagerduty":  {},
	"slack":      {},
	"sentry":     {},
}

// SensitiveFields is the set of config keys that must be masked in
// any response that crosses the HTTP boundary. Any key in this set is
// replaced with MaskedValue when List / Get returns.
var SensitiveFields = map[string]struct{}{
	// maxmind
	"license_key": {},
	// cloudflare
	"api_token": {},
	// pagerduty
	"integration_key": {},
	// slack
	"webhook_url": {},
	// sentry
	"dsn": {},
}

// MaskedValue is the opaque placeholder returned in every GET response
// where a sensitive field used to live. The console shows this as "set
// but hidden" so the operator knows a value is present without being
// able to read it back.
const MaskedValue = "***masked***"

// IntegrationRecord is one per-tenant integration row. When returned
// by List / Get the Config map is masked — sensitive fields are
// replaced with MaskedValue. The internal Decrypt path bypasses
// masking and returns the raw plaintext.
type IntegrationRecord struct {
	ID           uuid.UUID      `json:"id"`
	TenantID     uuid.UUID      `json:"tenant_id"`
	ServiceName  string         `json:"service_name"`
	Config       map[string]any `json:"config"`
	KeyVersion   int            `json:"key_version"`
	Enabled      bool           `json:"enabled"`
	UpdatedAt    time.Time      `json:"updated_at"`
	AuditActorID *uuid.UUID     `json:"audit_actor_id,omitempty"`
}

// UpsertRequest is the body of PUT /v1/settings/integrations/{service}.
type UpsertRequest struct {
	Config  map[string]any `json:"config"`
	Enabled bool           `json:"enabled"`
}

// maskConfig returns a copy of cfg with every sensitive field replaced
// by MaskedValue. Non-sensitive fields are passed through unchanged.
// The returned map is safe to serialise over HTTP.
func maskConfig(cfg map[string]any) map[string]any {
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		if _, sensitive := SensitiveFields[k]; sensitive {
			if v == nil || v == "" {
				out[k] = ""
			} else {
				out[k] = MaskedValue
			}
			continue
		}
		out[k] = v
	}
	return out
}
