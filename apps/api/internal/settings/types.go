// Package settings implements the tenant-level configuration surface
// for CA mode and retention policy (Wave 9 Sprint 3A). Integrations
// (internal/integrations) and backup targets (internal/backup) live in
// their own packages because they carry Vault-encrypted payloads and
// their own schemas — settings here is only the JSONB + string fields
// that land directly on the tenants row.
//
// Every mutation is gated on admin role at the router layer and every
// mutation writes an audit entry BEFORE the DB UPDATE. Retention policy
// mutations enforce KVKK minimums inline: values below the legal floor
// are rejected and the rejection is audit-logged.
package settings

import "fmt"

// CaMode is one of three supported production CA modes.
type CaMode string

const (
	CaModeLetsencrypt CaMode = "letsencrypt"
	CaModeInternal    CaMode = "internal"
	CaModeCommercial  CaMode = "commercial"
)

// allowedCaModes is the allowlist enforced on write.
var allowedCaModes = map[CaMode]struct{}{
	CaModeLetsencrypt: {},
	CaModeInternal:    {},
	CaModeCommercial:  {},
}

// CaModeInfo is the response body for GET /v1/settings/ca-mode.
type CaModeInfo struct {
	Mode   CaMode         `json:"mode"`
	Config map[string]any `json:"config,omitempty"`
}

// UpdateCaModeRequest is the body of PATCH /v1/settings/ca-mode.
type UpdateCaModeRequest struct {
	Mode   CaMode         `json:"mode"`
	Config map[string]any `json:"config,omitempty"`
}

// validateCaMode rejects unknown modes and checks that the config
// shape is at least the bare minimum for the requested mode.
func validateCaMode(req UpdateCaModeRequest) error {
	if _, ok := allowedCaModes[req.Mode]; !ok {
		return fmt.Errorf("settings: unknown ca_mode %q (expected letsencrypt|internal|commercial)", req.Mode)
	}
	switch req.Mode {
	case CaModeLetsencrypt:
		if req.Config == nil {
			return fmt.Errorf("settings: letsencrypt requires config with dns_provider + email")
		}
		if _, ok := req.Config["dns_provider"]; !ok {
			return fmt.Errorf("settings: letsencrypt config requires dns_provider")
		}
		if _, ok := req.Config["email"]; !ok {
			return fmt.Errorf("settings: letsencrypt config requires email")
		}
	case CaModeCommercial:
		if req.Config == nil {
			return fmt.Errorf("settings: commercial requires config with csr_key + cert_chain_key")
		}
		if _, ok := req.Config["csr_key"]; !ok {
			return fmt.Errorf("settings: commercial config requires csr_key")
		}
		if _, ok := req.Config["cert_chain_key"]; !ok {
			return fmt.Errorf("settings: commercial config requires cert_chain_key")
		}
	case CaModeInternal:
		// internal takes no required config. An empty map is fine.
	}
	return nil
}

// RetentionPolicy carries all tenant-overridable retention dials.
// Every value is measured in days except AuditYears which is measured
// in years to match the KVKK m.12 legal wording. A non-NULL key set
// by the operator persists; missing keys fall through to the system
// default on read.
type RetentionPolicy struct {
	AuditYears     int `json:"audit_years"`
	EventDays      int `json:"event_days"`
	ScreenshotDays int `json:"screenshot_days"`
	KeystrokeDays  int `json:"keystroke_days"`
	LiveViewDays   int `json:"live_view_days"`
	DsrDays        int `json:"dsr_days"`
}

// DefaultRetentionPolicy is the system-wide starting point. Tenants
// may override individual keys via PATCH /v1/settings/retention but
// never below the KVKK minimums enforced by validateRetention.
var DefaultRetentionPolicy = RetentionPolicy{
	AuditYears:     5,
	EventDays:      365,
	ScreenshotDays: 30,
	KeystrokeDays:  180,
	LiveViewDays:   30,
	DsrDays:        3650,
}

// KVKK minimum floors. These are the legal-mandate-derived lower
// bounds; any PATCH that drops below these is rejected.
const (
	minAuditYears     = 5    // KVKK m.12
	minEventDays      = 365  // KVKK m.7 / m.5 proportionality window
	minScreenshotDays = 30   // Proportionality default
	minKeystrokeDays  = 180  // ADR 0013 DLP opt-in ceremony
	minDsrDays        = 3650 // KVKK m.11 10-year record
)

// validateRetention enforces KVKK floors. LiveViewDays has no floor
// because live view recording is always opt-in (ADR 0019) — a tenant
// can legally set it to 0.
func validateRetention(p RetentionPolicy) error {
	if p.AuditYears < minAuditYears {
		return fmt.Errorf("settings: audit_years must be >= %d (KVKK m.12)", minAuditYears)
	}
	if p.EventDays < minEventDays {
		return fmt.Errorf("settings: event_days must be >= %d (KVKK proportionality)", minEventDays)
	}
	if p.ScreenshotDays < minScreenshotDays {
		return fmt.Errorf("settings: screenshot_days must be >= %d", minScreenshotDays)
	}
	if p.KeystrokeDays < minKeystrokeDays {
		return fmt.Errorf("settings: keystroke_days must be >= %d (ADR 0013)", minKeystrokeDays)
	}
	if p.DsrDays < minDsrDays {
		return fmt.Errorf("settings: dsr_days must be >= %d (KVKK m.11 10 year)", minDsrDays)
	}
	if p.LiveViewDays < 0 {
		return fmt.Errorf("settings: live_view_days must be >= 0")
	}
	return nil
}
