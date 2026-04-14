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

// --- Backup (Phase 3.0 — A1.2 / CC9.1) ---
const (
	// ActionBackupRun is emitted by the out-of-API backup runner after a
	// successful dump. Input to the backup.Service.RecordRun evidence
	// collector.
	ActionBackupRun Action = "backup.run"
)

// --- Access review / Incident / BCP (Phase 3.0 — CC6.3 / CC7.3 / CC9.1) ---
const (
	// ActionAccessReviewCompleted is emitted when a DPO or manager
	// submits the outcome of a quarterly or semi-annual access review.
	ActionAccessReviewCompleted Action = "access_review.completed"

	// ActionIncidentClosed is emitted when a security incident is
	// closed with a post-incident review. Input to the incident.Service
	// evidence collector for CC7.3.
	ActionIncidentClosed Action = "incident.closed"

	// ActionBCPDrillCompleted is emitted after a BCP / DR drill
	// (tabletop or live) is complete. Input to the bcp.Service
	// evidence collector for CC9.1.
	ActionBCPDrillCompleted Action = "bcp_drill.completed"
)

// --- Tickets (Faz 17 item #184) ---
const (
	ActionTicketCreated     Action = "ticket.created"
	ActionTicketUpdated     Action = "ticket.updated"
	ActionTicketStateChange Action = "ticket.state_changed"
)

// --- Status page (Faz 17 item #185) ---
const (
	ActionStatusIncidentCreated     Action = "status_incident.created"
	ActionStatusIncidentUpdated     Action = "status_incident.updated"
	ActionStatusIncidentResolved    Action = "status_incident.resolved"
	ActionMaintenanceWindowCreated  Action = "maintenance_window.created"
	ActionMaintenanceWindowStarted  Action = "maintenance_window.started"
	ActionMaintenanceWindowFinished Action = "maintenance_window.finished"
)

// --- License (Faz 17 item #179/180) ---
const (
	ActionLicenseRefreshed      Action = "license.refreshed"
	ActionLicenseTamperDetected Action = "license.tamper_detected"
)

// --- Endpoint fleet ---
const (
	ActionEndpointEnrolled       Action = "endpoint.enrolled"
	ActionEndpointRevoked        Action = "endpoint.revoked"
	ActionEndpointDeleted        Action = "endpoint.deleted"
	ActionEndpointTokenRefreshed Action = "endpoint.token_refreshed"
	// Remote command actions (Faz 6 #64 #65).
	// Wipe issues a crypto-erase command (KVKK m.7). Deactivate stops
	// all collectors but preserves local state. Bulk covers 1..N
	// endpoints in one API call. Ack is written when the gateway
	// reports back agent acknowledgement of the command.
	ActionEndpointWipe        Action = "endpoint.wipe_issued"
	ActionEndpointDeactivate  Action = "endpoint.deactivate_issued"
	ActionEndpointCommandBulk Action = "endpoint.bulk_operation"
	ActionEndpointCommandAck  Action = "endpoint.command_acknowledged"
)

// --- Screenshot / screenclip ---
const (
	ActionScreenshotViewed   Action = "screenshot.viewed"
	ActionScreenshotExported Action = "screenshot.exported"
	ActionScreenclipViewed   Action = "screenclip.viewed"
)

// --- Tenant preferences ---
const (
	// Per-tenant screenshot capture preset update (minimal/low/medium/
	// high/max). Written by the /v1/tenants/me/screenshot-preset PATCH
	// handler; audit details include before+after preset values.
	ActionTenantScreenshotPreset Action = "tenant.screenshot_preset.update"
)

// --- Event detail views ---
const (
	ActionFileEventViewed    Action = "file_event.viewed"
	ActionNetworkEventViewed Action = "network_event.viewed"
)

// --- Live view ---
const (
	ActionLiveViewRequested         Action = "live_view.requested"
	ActionLiveViewApproved          Action = "live_view.approved"
	ActionLiveViewDenied            Action = "live_view.denied"
	ActionLiveViewStarted           Action = "live_view.started"
	ActionLiveViewStopped           Action = "live_view.stopped"
	// ActionLiveViewTerminatedByHR is retained only as a compile-time
	// constant so existing migrations/tests don't break; the real
	// authority for live view termination is IT (_byIT / _byAdmin).
	// HR has no termination authority in the IT-owned hierarchy.
	ActionLiveViewTerminatedByHR    Action = "live_view.terminated_by_hr"
	ActionLiveViewTerminatedByITMgr Action = "live_view.terminated_by_it_manager"
	ActionLiveViewTerminatedByAdmin Action = "live_view.terminated_by_admin"
	ActionLiveViewTerminatedByDPO   Action = "live_view.terminated_by_dpo"
	ActionLiveViewExpired           Action = "live_view.expired"
	ActionLiveViewFailed            Action = "live_view.failed"
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

// --- Feature flags (Faz 16 #173) ---
const (
	ActionFeatureFlagSet     Action = "feature_flag.set"
	ActionFeatureFlagDeleted Action = "feature_flag.deleted"
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

// --- Pipeline (Faz 7 #75) ---
const (
	// ActionPipelineReplay is emitted by POST /v1/pipeline/replay
	// BEFORE any DLQ re-publish or CH reconstruction. Every replay
	// is audited — dry_run or real — so operators cannot silently
	// re-inject events after a bug fix.
	ActionPipelineReplay Action = "pipeline.replay"
)

// --- API keys (Faz 6 #72 — service-to-service credential) ---
const (
	// ActionAPIKeyCreated is emitted by the apikey service when a new
	// service API key is issued. Details include the name, scopes,
	// and whether the key is tenant-scoped or cross-tenant.
	ActionAPIKeyCreated Action = "apikey.issued"

	// ActionAPIKeyRevoked is emitted when an API key is revoked. The
	// target is "apikey:{id}". No plaintext or hash is included.
	ActionAPIKeyRevoked Action = "apikey.revoked"
)

// --- Audit stream (Faz 6 #66) ---
const (
	// ActionAuditStreamSubscribed is recorded when a principal opens
	// a WebSocket to GET /v1/audit/stream. Captures filter, tenant,
	// and whether the subscriber elevated to all_tenants (DPO only).
	ActionAuditStreamSubscribed Action = "audit.stream.subscribed"

	// ActionAuditStreamUnsubscribed is recorded on WebSocket close.
	// Details.dropped surfaces any entries the fanout had to discard
	// because the subscriber's consumer was behind; non-zero values
	// indicate a slow client worth investigating.
	ActionAuditStreamUnsubscribed Action = "audit.stream.unsubscribed"
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
	ActionBackupRun,
	ActionAccessReviewCompleted,
	ActionIncidentClosed,
	ActionBCPDrillCompleted,
	ActionTicketCreated,
	ActionTicketUpdated,
	ActionTicketStateChange,
	ActionStatusIncidentCreated,
	ActionStatusIncidentUpdated,
	ActionStatusIncidentResolved,
	ActionMaintenanceWindowCreated,
	ActionMaintenanceWindowStarted,
	ActionMaintenanceWindowFinished,
	ActionLicenseRefreshed,
	ActionLicenseTamperDetected,
	ActionEndpointEnrolled,
	ActionEndpointRevoked,
	ActionEndpointDeleted,
	ActionEndpointTokenRefreshed,
	ActionEndpointWipe,
	ActionEndpointDeactivate,
	ActionEndpointCommandBulk,
	ActionEndpointCommandAck,
	ActionScreenshotViewed,
	ActionScreenshotExported,
	ActionScreenclipViewed,
	ActionTenantScreenshotPreset,
	ActionFileEventViewed,
	ActionNetworkEventViewed,
	ActionLiveViewRequested,
	ActionLiveViewApproved,
	ActionLiveViewDenied,
	ActionLiveViewStarted,
	ActionLiveViewStopped,
	ActionLiveViewTerminatedByHR,
	ActionLiveViewTerminatedByITMgr,
	ActionLiveViewTerminatedByAdmin,
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
	ActionFeatureFlagSet,
	ActionFeatureFlagDeleted,
	ActionAuditChainVerified,
	ActionAuditChainBroken,
	ActionTransparencyHistoryVisibilityChanged,
	ActionFirstLoginAcknowledged,
	ActionTransparencyDSRDetailViewed,
	ActionDLPPEDEKBootstrapped,
	ActionDLPPEDEKBootstrapBatch,
	ActionAgentSilenceAcknowledged,
	ActionPipelineReplay,
	ActionAuditStreamSubscribed,
	ActionAuditStreamUnsubscribed,
	ActionAPIKeyCreated,
	ActionAPIKeyRevoked,
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
