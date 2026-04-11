// Package hris implements the HRIS (Human Resources Information System)
// connector framework per ADR 0018. Phase 2.5 scaffold.
//
// The framework follows these design principles:
//
//  1. Compile-time registry (not runtime plugin loading). Adapter packages
//     must be imported explicitly from the api cmd/ binary. This trades
//     flexibility for security: a compromised HRIS adapter cannot inject
//     new code paths at runtime.
//
//  2. Connectors are pure data sources. They read employee records from
//     upstream HRIS systems and return canonical `Employee` structs. All
//     side effects (DB writes, audit logging, user table updates) happen
//     in the sync orchestrator, NOT in connectors.
//
//  3. HRIS is the source of truth for employment status. Personel owns
//     platform-specific data (DSR history, live view approvals, policy
//     assignments); HRIS owns identity + org chart + employment lifecycle.
//
//  4. Deletion semantics: when HRIS marks an employee as terminated, the
//     sync orchestrator marks them `inactive` and triggers the KVKK
//     retention countdown. The employee record is NOT hard-deleted —
//     that would break audit trail integrity (ADR 0013).
//
// Phase 2.5 Phase 2.5 ships two real adapters:
//   - BambooHR (international, OAuth2, REST webhooks)
//   - Logo Tiger (Turkish market differentiator, session ticket auth)
//
// Future adapters (Phase 2.6+): Workday, Personio, BordroPlus, SAP
// SuccessFactors, Mikro, Netsis.
package hris

import (
	"context"
	"time"
)

// Employee is the canonical representation of an employee as returned by
// any HRIS connector. The sync orchestrator maps this to the `users` table
// columns added in migration 0023 (Phase 2.0/1).
//
// Required fields: ExternalID, Email, Username.
// All other fields are best-effort — connectors should populate what they
// can from the upstream HRIS schema.
type Employee struct {
	// ExternalID is the HRIS-assigned stable identifier. MUST be stable
	// across syncs for the sync orchestrator to deduplicate correctly.
	// Stored in users.hris_id.
	ExternalID string

	// Source identifies which HRIS this record came from. Matches the
	// hris_source CHECK constraint in migration 0023.
	Source string

	// Email is the corporate email address. Used as the primary join key
	// back to the existing users table when an employee is imported for
	// the first time and no external ID is yet linked.
	Email string

	// Username is the login name (typically the corporate SSO username).
	Username string

	// FullName is the display name for the admin console.
	FullName string

	// Department is the current org unit. Stored in users.department.
	Department string

	// JobTitle is the current role title. Stored in users.job_title.
	JobTitle string

	// ManagerExternalID is the direct manager's HRIS ID. The sync
	// orchestrator resolves this to a UUID in users.manager_user_id
	// after all employees in the batch have been upserted.
	ManagerExternalID string

	// HiredAt is the employment start date. Stored in users.hired_at.
	HiredAt *time.Time

	// TerminatedAt is the employment end date, if known. Non-nil
	// triggers the KVKK retention countdown on the user record.
	TerminatedAt *time.Time

	// IsActive is true if the employee is currently employed.
	IsActive bool

	// Locale is the UI language preference (tr/en). Default "tr"
	// per locked decision #1.
	Locale string

	// CustomFields is a free-form JSON blob for HRIS-specific attributes
	// that don't fit the canonical schema. Stored in users.custom_fields.
	CustomFields map[string]any
}

// ChangeKind identifies the type of change a connector reports. Used by
// webhook-driven connectors to push incremental updates.
type ChangeKind string

const (
	// ChangeCreated indicates a new employee was added in the HRIS.
	ChangeCreated ChangeKind = "created"
	// ChangeUpdated indicates an existing employee's record was modified.
	ChangeUpdated ChangeKind = "updated"
	// ChangeTerminated indicates the employment ended.
	ChangeTerminated ChangeKind = "terminated"
	// ChangeDeleted indicates the HRIS record was hard-deleted (rare; most
	// HRIS systems soft-delete). The sync orchestrator treats this the
	// same as Terminated to preserve audit trail.
	ChangeDeleted ChangeKind = "deleted"
)

// Change is a single incremental update reported via WatchChanges.
type Change struct {
	Kind     ChangeKind
	Employee Employee
	At       time.Time
}

// Connector is the interface every HRIS adapter must implement. Adapter
// implementations live in sibling packages under apps/api/internal/hris/adapters.
//
// All methods MUST be safe to call concurrently. The sync orchestrator
// may issue ListEmployees while WatchChanges is running.
//
// All methods MUST respect the context deadline. HRIS calls can be slow;
// the orchestrator wraps each call with a per-connector timeout.
//
// Errors are categorized via the ErrorKind wrapper in errors.go. Transient
// errors trigger exponential backoff; permanent errors page the DPO.
type Connector interface {
	// Name returns the connector identifier, e.g. "bamboohr", "logo_tiger".
	// Must match the hris_source CHECK constraint values in migration 0023.
	Name() string

	// TestConnection verifies that credentials and network connectivity
	// are valid. Called during initial setup and by the nightly health
	// check. Returns nil on success or an ErrorKind-wrapped error.
	TestConnection(ctx context.Context) error

	// ListEmployees returns all employees currently active in the HRIS.
	// Used for the full periodic sync (hourly default). Connectors MAY
	// implement this as a paginated call internally; callers see a single
	// flat slice.
	//
	// The returned slice MUST NOT contain duplicates (by ExternalID).
	ListEmployees(ctx context.Context) ([]Employee, error)

	// GetEmployee returns a single employee by external ID. Used for
	// targeted refreshes after a webhook push.
	GetEmployee(ctx context.Context, externalID string) (*Employee, error)

	// WatchChanges subscribes to incremental updates. Connectors that
	// support webhooks return a channel that receives Change events as
	// they arrive. Connectors that only support polling return a closed
	// channel; the orchestrator falls back to periodic ListEmployees.
	//
	// The returned channel is closed when ctx is cancelled or when the
	// upstream subscription terminates.
	WatchChanges(ctx context.Context) (<-chan Change, error)

	// Capabilities returns metadata about what the connector supports.
	// Used by the sync orchestrator to pick the right strategy.
	Capabilities() Capabilities
}

// Capabilities describes optional features a connector may or may not support.
type Capabilities struct {
	// SupportsWebhooks is true if the connector can push incremental
	// updates via WatchChanges (BambooHR yes, Logo Tiger no).
	SupportsWebhooks bool

	// SupportsManagerChain is true if the connector exposes the manager
	// relationship for org chart construction.
	SupportsManagerChain bool

	// SupportsCustomFields is true if the connector passes through
	// HRIS-specific fields into Employee.CustomFields.
	SupportsCustomFields bool

	// MaxPageSize is the maximum number of employees the connector can
	// return in a single ListEmployees call. 0 means unlimited.
	MaxPageSize int

	// PollInterval is the recommended polling cadence when webhooks are
	// unavailable. 0 means use the global default (1 hour).
	PollInterval time.Duration
}

// Config is the common configuration every connector accepts. Adapters
// may embed this and add their own fields.
type Config struct {
	// Name must match Connector.Name() and the hris_source CHECK constraint.
	Name string

	// BaseURL is the upstream HRIS endpoint (e.g. https://api.bamboohr.com).
	// Empty for connectors that use fixed endpoints (rare).
	BaseURL string

	// TenantID scopes the sync to a single Personel tenant. All upserted
	// users inherit this tenant_id.
	TenantID string

	// Timeout is the per-request deadline applied to every upstream call.
	// Default 30 seconds.
	Timeout time.Duration

	// CredentialsSecretRef is a Vault path the adapter resolves at runtime
	// to fetch its credentials. NEVER a literal credential — always an
	// indirection so Vault owns rotation.
	CredentialsSecretRef string
}
