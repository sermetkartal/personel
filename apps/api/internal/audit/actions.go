// Package audit defines the exhaustive enumeration of all audit action strings.
//
// MANDATORY: Every Admin API handler that mutates state, reads sensitive data,
// or publishes commands MUST call audit.Recorder.Append with one of these
// constants BEFORE performing the side effect. A CI test replays all
// registered handlers against a stub recorder and fails if any handler does
// not call Append.
//
// Adding a new action requires:
//  1. Adding a constant here.
//  2. Adding the action to AllActions below (compile-time exhaustiveness check).
//  3. Using it in the handler before any DB/NATS write.
package audit

// Action is the typed string for an audit event action.
// Values map directly to the runbook §7 table.
type Action string

// --- Identity & Session ---
const (
	ActionAdminLoginSuccess   Action = "admin.login.success"
	ActionAdminLoginFailed    Action = "admin.login.failed"
	ActionAdminLoginLocked    Action = "admin.login.locked"
	ActionAdminLogout         Action = "admin.logout"
	ActionAdminSessionRefreshed Action = "admin.session.refreshed"
	ActionAdminPasswordChanged  Action = "admin.password.changed"
	ActionAdminMFAEnrolled      Action = "admin.mfa.enrolled"
	ActionAdminMFADisabled      Action = "admin.mfa.disabled"
)

// --- User management ---
const (
	ActionUserCreated     Action = "user.created"
	ActionUserRoleChanged Action = "user.role_changed"
	ActionUserDisabled    Action = "user.disabled"
	ActionUserDeleted     Action = "user.deleted"
)

// --- Employee ---
const (
	ActionEmployeeCreated Action = "employee.created"
	ActionEmployeeUpdated Action = "employee.updated"
	ActionEmployeeDeleted Action = "employee.deleted"
)

// --- Policy ---
const (
	ActionPolicyCreated Action = "policy.created"
	ActionPolicyUpdated Action = "policy.updated"
	ActionPolicyDeleted Action = "policy.deleted"
	ActionPolicyPushed  Action = "policy.pushed"
)

// --- Endpoint fleet ---
const (
	ActionEndpointEnrolled Action = "endpoint.enrolled"
	ActionEndpointRevoked  Action = "endpoint.revoked"
	ActionEndpointDeleted  Action = "endpoint.deleted"
)

// --- Screenshot / screenclip ---
const (
	ActionScreenshotViewed   Action = "screenshot.viewed"
	ActionScreenshotExported Action = "screenshot.exported"
	ActionScreenclipViewed   Action = "screenclip.viewed"
)

// --- Event detail views ---
const (
	ActionFileEventViewed    Action = "file_event.viewed"
	ActionNetworkEventViewed Action = "network_event.viewed"
)

// --- Live view ---
const (
	ActionLiveViewRequested       Action = "live_view.requested"
	ActionLiveViewApproved        Action = "live_view.approved"
	ActionLiveViewDenied          Action = "live_view.denied"
	ActionLiveViewStarted         Action = "live_view.started"
	ActionLiveViewStopped         Action = "live_view.stopped"
	ActionLiveViewTerminatedByHR  Action = "live_view.terminated_by_hr"
	ActionLiveViewTerminatedByDPO Action = "live_view.terminated_by_dpo"
	ActionLiveViewExpired         Action = "live_view.expired"
	ActionLiveViewFailed          Action = "live_view.failed"
)

// --- DLP ---
const (
	ActionDLPRuleDrafted   Action = "dlp.rule.drafted"
	ActionDLPRuleApproved  Action = "dlp.rule.approved"
	ActionDLPRuleSigned    Action = "dlp.rule.signed"
	ActionDLPRuleActivated Action = "dlp.rule.activated"
	ActionDLPMatchViewed   Action = "dlp.match.viewed"
	// ADR 0013 state transition audit events.
	ActionDLPEnabled       Action = "dlp.enabled"
	ActionDLPDisabled      Action = "dlp.disabled"
	ActionDLPEnableFailed  Action = "dlp.enable_failed"
)

// --- DSR (KVKK m.11) ---
const (
	ActionDSRSubmitted  Action = "dsr.submitted"
	ActionDSRAssigned   Action = "dsr.assigned"
	ActionDSRExtended   Action = "dsr.extended"
	ActionDSRResponded  Action = "dsr.responded"
	ActionDSRRejected   Action = "dsr.rejected"
	ActionDSRErased     Action = "dsr.erased"
	ActionDSRExported   Action = "dsr.exported"
)

// --- Legal Hold ---
const (
	ActionLegalHoldPlaced   Action = "legal_hold.placed"
	ActionLegalHoldReleased Action = "legal_hold.released"
)

// --- Retention ---
const (
	ActionRetentionPolicyChanged  Action = "retention.policy.changed"
	ActionRetentionPurgeExecuted  Action = "retention.purge.executed"
	ActionRetentionDestructionReportGenerated Action = "retention.destruction_report_generated"
)

// --- Export ---
const (
	ActionExportRequested  Action = "export.requested"
	ActionExportGenerated  Action = "export.generated"
	ActionExportDownloaded Action = "export.downloaded"
)

// --- Break-glass ---
const (
	ActionBreakGlassDLPOpened   Action = "admin.breakglass.dlp_host.opened"
	ActionBreakGlassDLPClosed   Action = "admin.breakglass.dlp_host.closed"
	ActionBreakGlassVaultOpened Action = "admin.breakglass.vault.opened"
)

// --- Vault / PKI ---
const (
	ActionVaultPolicyChanged     Action = "vault.policy.changed"
	ActionPKICertIssued          Action = "pki.cert.issued"
	ActionPKICertRevoked         Action = "pki.cert.revoked"
)

// --- Release ---
const (
	ActionReleaseSigningKeyRotated Action = "release.signing_key_rotated"
	ActionReleasePublished         Action = "release.published"
	ActionReleaseCanaryAdvanced    Action = "release.canary_advanced"
	ActionReleaseRolledBack        Action = "release.rolled_back"
)

// --- Audit chain ---
const (
	ActionAuditChainVerified Action = "audit.chain_verified"
	ActionAuditChainBroken   Action = "audit.chain_broken"
)

// --- Transparency ---
const (
	ActionTransparencyHistoryVisibilityChanged Action = "transparency.history_visibility_changed"
	ActionFirstLoginAcknowledged               Action = "transparency.first_login_acknowledged"
	ActionTransparencyDSRDetailViewed          Action = "transparency.dsr_detail_viewed"
)

// --- DLP PE-DEK bootstrap (ADR 0013 A2) ---
const (
	ActionDLPPEDEKBootstrapped    Action = "dlp.pe_dek_bootstrapped"
	ActionDLPPEDEKBootstrapBatch  Action = "dlp.pe_dek_bootstrap_batch"
)

// --- Silence / agent health ---
const (
	ActionAgentSilenceAcknowledged Action = "agent.silence.acknowledged"
)

// AllActions is the canonical list. The test in audit_test.go iterates this
// slice and verifies every action appears in at least one registered handler.
// Add new actions here in the same commit as the constant above.
var AllActions = []Action{
	ActionAdminLoginSuccess,
	ActionAdminLoginFailed,
	ActionAdminLoginLocked,
	ActionAdminLogout,
	ActionAdminSessionRefreshed,
	ActionAdminPasswordChanged,
	ActionAdminMFAEnrolled,
	ActionAdminMFADisabled,
	ActionUserCreated,
	ActionUserRoleChanged,
	ActionUserDisabled,
	ActionUserDeleted,
	ActionEmployeeCreated,
	ActionEmployeeUpdated,
	ActionEmployeeDeleted,
	ActionPolicyCreated,
	ActionPolicyUpdated,
	ActionPolicyDeleted,
	ActionPolicyPushed,
	ActionEndpointEnrolled,
	ActionEndpointRevoked,
	ActionEndpointDeleted,
	ActionScreenshotViewed,
	ActionScreenshotExported,
	ActionScreenclipViewed,
	ActionFileEventViewed,
	ActionNetworkEventViewed,
	ActionLiveViewRequested,
	ActionLiveViewApproved,
	ActionLiveViewDenied,
	ActionLiveViewStarted,
	ActionLiveViewStopped,
	ActionLiveViewTerminatedByHR,
	ActionLiveViewTerminatedByDPO,
	ActionLiveViewExpired,
	ActionLiveViewFailed,
	ActionDLPRuleDrafted,
	ActionDLPRuleApproved,
	ActionDLPRuleSigned,
	ActionDLPRuleActivated,
	ActionDLPMatchViewed,
	ActionDLPEnabled,
	ActionDLPDisabled,
	ActionDLPEnableFailed,
	ActionDSRSubmitted,
	ActionDSRAssigned,
	ActionDSRExtended,
	ActionDSRResponded,
	ActionDSRRejected,
	ActionDSRErased,
	ActionDSRExported,
	ActionLegalHoldPlaced,
	ActionLegalHoldReleased,
	ActionRetentionPolicyChanged,
	ActionRetentionPurgeExecuted,
	ActionRetentionDestructionReportGenerated,
	ActionExportRequested,
	ActionExportGenerated,
	ActionExportDownloaded,
	ActionBreakGlassDLPOpened,
	ActionBreakGlassDLPClosed,
	ActionBreakGlassVaultOpened,
	ActionVaultPolicyChanged,
	ActionPKICertIssued,
	ActionPKICertRevoked,
	ActionReleaseSigningKeyRotated,
	ActionReleasePublished,
	ActionReleaseCanaryAdvanced,
	ActionReleaseRolledBack,
	ActionAuditChainVerified,
	ActionAuditChainBroken,
	ActionTransparencyHistoryVisibilityChanged,
	ActionFirstLoginAcknowledged,
	ActionTransparencyDSRDetailViewed,
	ActionDLPPEDEKBootstrapped,
	ActionDLPPEDEKBootstrapBatch,
	ActionAgentSilenceAcknowledged,
}

// ValidAction returns true if a is a known audit action.
func ValidAction(a Action) bool {
	for _, known := range AllActions {
		if known == a {
			return true
		}
	}
	return false
}
