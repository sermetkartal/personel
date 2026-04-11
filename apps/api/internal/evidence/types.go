// Package evidence implements the SOC 2 evidence locker for Phase 3.0.
//
// ADR 0023 mandates 12-month SOC 2 Type II observation. The evidence
// locker automates collection of audit artifacts required by each
// control in the TSC (Trust Services Criteria) so auditors can pull
// time-bounded evidence packs on demand rather than engineers scrambling
// to assemble evidence manually.
//
// Design principles:
//
//  1. Append-only: evidence items are immutable once written. The locker
//     is itself auditable — any mutation is a SOC 2 CC7.2 violation.
//  2. Control-mapped: every evidence item carries the TSC control ID it
//     supports (e.g. CC6.1 logical access, CC7.3 incident detection).
//  3. Time-bucketed: evidence is organized by collection_period (month)
//     so generating a Type II evidence pack for any 12-month window is
//     a single query + streaming export.
//  4. Signed: every evidence pack is signed by the Vault control-plane
//     key before being delivered to auditors. The signature + the
//     WORM-sunk audit chain together provide end-to-end integrity.
//  5. Read-path is DPO-only: only the DPO role can retrieve evidence
//     packs. Auditors receive packs out-of-band from the DPO.
//
// This package is the DB write path + HTTP export path. The collectors
// that actually produce evidence (access review exports, change ticket
// references, backup run records) live in their respective domain
// packages and call evidence.Record() at the right moment.
package evidence

import (
	"context"
	"encoding/json"
	"time"
)

// ControlID is a TSC control reference like "CC6.1" or "A1.2".
// Mapped to the SOC 2 Type II control matrix in docs/adr/0023-soc2-type2-controls.md.
type ControlID string

// TSC (Trust Services Criteria) control identifiers used by Personel.
// Not exhaustive — only the controls actively evidenced from the API.
const (
	// CC = Common Criteria
	CtrlCC6_1 ControlID = "CC6.1" // Logical access controls
	CtrlCC6_2 ControlID = "CC6.2" // New user registration + authorization
	CtrlCC6_3 ControlID = "CC6.3" // Access removal + periodic review
	CtrlCC6_6 ControlID = "CC6.6" // External access restrictions
	CtrlCC6_7 ControlID = "CC6.7" // Data transmission security
	CtrlCC6_8 ControlID = "CC6.8" // Malicious software prevention
	CtrlCC7_1 ControlID = "CC7.1" // Configuration management
	CtrlCC7_2 ControlID = "CC7.2" // Change authorization
	CtrlCC7_3 ControlID = "CC7.3" // Incident detection + response
	CtrlCC7_4 ControlID = "CC7.4" // Vulnerability management
	CtrlCC8_1 ControlID = "CC8.1" // Change management
	CtrlCC9_1 ControlID = "CC9.1" // Business continuity
	CtrlCC9_2 ControlID = "CC9.2" // Vendor management
	// A = Availability
	CtrlA1_1 ControlID = "A1.1" // Availability commitments
	CtrlA1_2 ControlID = "A1.2" // Backup and recovery
	CtrlA1_3 ControlID = "A1.3" // System monitoring
	// C = Confidentiality
	CtrlC1_1 ControlID = "C1.1" // Data classification
	CtrlC1_2 ControlID = "C1.2" // Data retention + disposal (KVKK m.7 overlap)
	// PI = Processing Integrity
	CtrlPI1_1 ControlID = "PI1.1" // Input validation
	CtrlPI1_5 ControlID = "PI1.5" // Output validation
	// P = Privacy
	CtrlP3_1 ControlID = "P3.1"  // Notice of privacy practices (KVKK m.10)
	CtrlP5_1 ControlID = "P5.1"  // Choice and consent (KVKK m.5)
	CtrlP6_1 ControlID = "P6.1"  // Collection limited to disclosed purposes (KVKK m.4)
	CtrlP7_1 ControlID = "P7.1"  // Use and retention (KVKK m.7)
)

// ItemKind identifies the type of evidence being stored.
type ItemKind string

const (
	// KindAccessReview is a periodic access review report signed by
	// the department manager + DPO.
	KindAccessReview ItemKind = "access_review"

	// KindChangeAuthorization is a change ticket approval record linked
	// to a production deploy.
	KindChangeAuthorization ItemKind = "change_authorization"

	// KindIncidentReport is a closed incident with timeline + RCA.
	KindIncidentReport ItemKind = "incident_report"

	// KindBackupRun is a successful backup job record with checksum.
	KindBackupRun ItemKind = "backup_run"

	// KindBackupRestoreTest is a successful restore drill with evidence.
	KindBackupRestoreTest ItemKind = "backup_restore_test"

	// KindVulnerabilityScan is a scan result with remediation status.
	KindVulnerabilityScan ItemKind = "vulnerability_scan"

	// KindVendorReview is an annual vendor security review.
	KindVendorReview ItemKind = "vendor_review"

	// KindPolicyReview is an annual policy document review + reaffirmation.
	KindPolicyReview ItemKind = "policy_review"

	// KindTrainingCompletion is a security awareness training completion
	// record per employee.
	KindTrainingCompletion ItemKind = "training_completion"

	// KindPenTestReport is a penetration test report with remediation
	// tracking.
	KindPenTestReport ItemKind = "pen_test_report"

	// KindComplianceAttestation is a formal attestation (KVKK VERBİS
	// renewal, GDPR Art. 30 register update).
	KindComplianceAttestation ItemKind = "compliance_attestation"

	// KindPrivilegedAccessSession is a closed HR-dual-control live view
	// session. Evidence recorded after termination captures requester,
	// approver, terminator, target endpoint, justification, and actual
	// duration. Supports CC6.1 (logical access controls) and CC6.3 (access
	// removal: the session terminated at or before its hard cap).
	KindPrivilegedAccessSession ItemKind = "privileged_access_session"
)

// Item is a single evidence record. Immutable once written to Postgres.
type Item struct {
	// ID is a ULID assigned at write time (sorts by time).
	ID string

	// TenantID scopes the evidence to a single on-prem deployment or
	// SaaS tenant.
	TenantID string

	// Control is the TSC control this evidence supports. An item may
	// support multiple controls — if so, the writer creates one Item
	// per control for unambiguous query paths.
	Control ControlID

	// Kind identifies the evidence category.
	Kind ItemKind

	// CollectionPeriod is the month this evidence applies to, formatted
	// YYYY-MM. Used for Type II observation window queries.
	CollectionPeriod string

	// RecordedAt is when the evidence was captured (wall clock).
	RecordedAt time.Time

	// Actor is the user or service that generated the evidence. May be
	// empty for automated system evidence.
	Actor string

	// Summary is a short human-readable description shown in auditor
	// evidence indexes. Turkish + English parallel text.
	SummaryTR string
	SummaryEN string

	// Payload is the structured evidence body. Opaque to the locker;
	// the consumer knows how to interpret it based on Kind.
	Payload json.RawMessage

	// ReferencedAuditIDs are the audit_log hash chain entries this
	// evidence item corresponds to. Provides cross-reference from the
	// evidence pack to the tamper-evident audit trail.
	ReferencedAuditIDs []int64

	// AttachmentRefs are MinIO object keys for any attached files
	// (PDF reports, zip archives, screenshots).
	AttachmentRefs []string

	// SignatureKeyVersion is the Vault key version used to sign this
	// item (Phase 3.0 will populate; Phase 2.11 scaffold leaves empty).
	SignatureKeyVersion string

	// Signature is the Ed25519 signature over a canonical encoding
	// of the item. Verified at retrieval time.
	Signature []byte
}

// Recorder is the interface domain packages call to store evidence.
type Recorder interface {
	// Record appends an evidence item to the locker. Returns the
	// assigned ULID on success.
	//
	// The recorder is responsible for: assigning the ID, computing the
	// CollectionPeriod from RecordedAt, signing the canonical encoding
	// via Vault, and persisting the item to Postgres within a single
	// transaction.
	//
	// Write failures should be treated as incidents — failing to record
	// required evidence is a SOC 2 control failure.
	Record(ctx context.Context, item Item) (string, error)
}

// PackRequest is the parameters for building a Type II evidence pack.
type PackRequest struct {
	// TenantID scopes the export.
	TenantID string

	// PeriodStart and PeriodEnd define the observation window.
	// For SOC 2 Type II this is exactly 12 months; the locker does
	// not enforce that but auditors reject packs outside the window.
	PeriodStart time.Time
	PeriodEnd   time.Time

	// Controls limits the pack to specific TSC controls. Empty means
	// all controls the locker has evidence for.
	Controls []ControlID

	// IncludeAttachments embeds referenced MinIO objects in the ZIP.
	// false = manifest-only (faster, smaller).
	IncludeAttachments bool
}

// Pack is the result of PackBuilder.Build — a reference to a MinIO
// object ready for DPO download.
type Pack struct {
	ID            string
	TenantID      string
	PeriodStart   time.Time
	PeriodEnd     time.Time
	ItemCount     int
	ControlsCovered []ControlID
	SizeBytes     int64
	MinIOKey      string
	SignatureKey  string
	Signature     []byte
	BuiltAt       time.Time
	BuiltBy       string
}
