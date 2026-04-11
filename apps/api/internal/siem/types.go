// Package siem implements the SIEM (Security Information and Event
// Management) integration framework for Personel Phase 2.7.
//
// Design mirrors the HRIS package (apps/api/internal/hris):
//
//  1. Exporter interface + compile-time Factory registry
//  2. Per-tenant configuration with Vault-resolved credentials
//  3. OCSF (Open Cybersecurity Schema Framework) as the canonical wire
//     format, adapter-specific transformation layered on top
//  4. Non-blocking publish with in-memory bounded buffer; drops under
//     backpressure with a dropped_events counter
//
// Phase 2.7 ships two real adapters:
//   - Splunk HTTP Event Collector (HEC)
//   - Microsoft Sentinel Log Ingestion API (via Log Analytics DCR)
//
// Future adapters (Phase 3): Elastic Security, Sumo Logic, Chronicle,
// QRadar, Panther.
package siem

import (
	"context"
	"time"
)

// EventType identifies the kind of security event being exported.
// Maps to OCSF class_uid values where possible.
type EventType string

const (
	// EventAuditEntry is any row from the hash-chained audit_log table.
	// OCSF class_uid: 6003 (API Activity).
	EventAuditEntry EventType = "audit.entry"
	// EventLoginSuccess is a successful Keycloak OIDC login.
	// OCSF class_uid: 3001 (Authentication).
	EventLoginSuccess EventType = "login.success"
	// EventLoginFailure is a failed OIDC login or expired session.
	// OCSF class_uid: 3001 (Authentication) + activity_id 2 (Logon Failure).
	EventLoginFailure EventType = "login.failure"
	// EventDSROpened is a new KVKK m.11 data subject request.
	// OCSF class_uid: 6005 (Account Change) — policy event.
	EventDSROpened EventType = "dsr.opened"
	// EventLegalHoldPlaced is a DPO placing a legal hold on data.
	// OCSF class_uid: 6005.
	EventLegalHoldPlaced EventType = "legal_hold.placed"
	// EventLiveViewStarted is a dual-approved live view session starting.
	// OCSF class_uid: 2006 (Compliance Finding).
	EventLiveViewStarted EventType = "live_view.started"
	// EventDLPMatch is an ML/DLP engine rule match (when DLP is enabled).
	// OCSF class_uid: 2005 (Data Loss Prevention).
	EventDLPMatch EventType = "dlp.match"
	// EventAgentTamper is an anti-tamper detection on an endpoint.
	// OCSF class_uid: 2004 (Malware Activity).
	EventAgentTamper EventType = "agent.tamper"
	// EventAgentSilence is a Flow 7 prolonged offline detection.
	// OCSF class_uid: 2006 (Compliance Finding).
	EventAgentSilence EventType = "agent.silence"
	// EventPolicyChanged is a policy signer sign event.
	// OCSF class_uid: 6005 (Account Change).
	EventPolicyChanged EventType = "policy.changed"
)

// Severity is the event severity. Maps to OCSF severity_id values.
type Severity int

const (
	// SeverityInformational is the default for audit trail events.
	SeverityInformational Severity = 1
	// SeverityLow is routine security events.
	SeverityLow Severity = 2
	// SeverityMedium is security events requiring review.
	SeverityMedium Severity = 3
	// SeverityHigh is security events requiring immediate attention.
	SeverityHigh Severity = 4
	// SeverityCritical is security events requiring emergency response.
	SeverityCritical Severity = 5
)

// Event is the canonical event format passed to every exporter. Adapters
// transform this into their own wire format (CIM for Splunk, DCR schema
// for Sentinel, etc).
type Event struct {
	// ID is a unique event identifier, ideally a ULID for time ordering.
	ID string

	// Type is the event classification.
	Type EventType

	// Severity is the OCSF-aligned severity tier.
	Severity Severity

	// OccurredAt is when the event happened in the Personel system.
	OccurredAt time.Time

	// TenantID is the Personel tenant that produced the event.
	TenantID string

	// Actor is the user/service account responsible, if applicable.
	// Empty for system-generated events.
	Actor string

	// Target is the thing the event is about (endpoint ID, DSR ID,
	// policy ID, etc). Free-form string.
	Target string

	// Summary is a short English description for human consumption.
	Summary string

	// Details is the structured event payload. Keys are flattened into
	// the final wire format depending on adapter.
	Details map[string]any

	// TraceID is the distributed tracing context, if available.
	TraceID string

	// AuditHash is the hash-chain entry hash from audit_log if this
	// event corresponds to an audit row. Empty otherwise.
	// Included so SIEM analysts can cross-reference back to the
	// tamper-evident Postgres chain + WORM sink.
	AuditHash string
}

// Exporter is the interface every SIEM adapter must implement. Mirrors
// the HRIS Connector pattern.
//
// All methods MUST be safe for concurrent use. The exporter bus calls
// Publish from multiple goroutines.
type Exporter interface {
	// Name returns the adapter identifier, e.g. "splunk", "sentinel".
	// Matches the siem_exporter DB column (Phase 2.7 migration TBD).
	Name() string

	// Publish sends an event to the upstream SIEM. MUST be non-blocking
	// beyond the HTTP request itself; callers apply their own timeouts.
	//
	// Returns an error for logging/metrics purposes. The bus does NOT
	// retry failed publishes — it drops them and increments the drop
	// counter. SIEMs are lossy by design; the authoritative audit trail
	// is the Postgres chain + WORM sink (ADR 0014).
	Publish(ctx context.Context, event Event) error

	// PublishBatch is the batched equivalent. Adapters that support
	// batching (Splunk HEC, Sentinel DCR) implement this; others fall
	// back to a loop over Publish.
	PublishBatch(ctx context.Context, events []Event) error

	// TestConnection verifies credentials and upstream reachability.
	// Called at startup and by the nightly health check.
	TestConnection(ctx context.Context) error

	// Capabilities returns feature support flags.
	Capabilities() Capabilities
}

// Capabilities describes what an adapter supports.
type Capabilities struct {
	// SupportsBatch is true if PublishBatch is more efficient than
	// a loop of Publish calls.
	SupportsBatch bool

	// MaxBatchSize is the largest batch the adapter accepts in a
	// single call. Splunk HEC: ~1MB (~1000 events), Sentinel DCR: 1MB.
	MaxBatchSize int

	// SupportsAck is true if the adapter receives delivery confirmation
	// from the upstream. Splunk HEC: yes (ack endpoint), Sentinel DCR: no.
	SupportsAck bool
}

// Config is the common configuration every exporter accepts.
type Config struct {
	// Name must match Exporter.Name() and the DB column value.
	Name string

	// Endpoint is the upstream ingestion endpoint (full URL).
	Endpoint string

	// Timeout is the per-request deadline. Default 5 seconds.
	Timeout time.Duration

	// CredentialsSecretRef is a Vault path for the adapter to resolve
	// at runtime. Never a literal credential.
	CredentialsSecretRef string

	// TenantID scopes this exporter instance. One Config per tenant
	// per SIEM.
	TenantID string

	// IncludeEventTypes is an allowlist of event types to forward.
	// Empty means forward all.
	IncludeEventTypes []EventType

	// ExcludeEventTypes is a denylist applied after the allowlist.
	// Useful for suppressing noisy event types per tenant.
	ExcludeEventTypes []EventType
}
