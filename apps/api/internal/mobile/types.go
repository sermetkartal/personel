// Package mobile implements the Mobile BFF (Backend for Frontend)
// endpoints consumed by the React Native admin app (apps/mobile-admin).
//
// Design decision: these endpoints live inside the main admin API rather
// than a separate mobile-bff service. Rationale:
//
//  1. The mobile app needs exactly 5 aggregating endpoints + push token
//     registration. A dedicated service adds operational overhead
//     (deployment, CI, Keycloak client, audit routing) without benefit
//     at this scale.
//  2. RBAC, OIDC, audit, metrics, and error handling are already in
//     place in the admin API.
//  3. The mobile-admin app already uses a separate Keycloak client ID
//     so access is scoped without needing network-layer separation.
//  4. If Phase 3 introduces a public-internet edge (unlikely for on-prem),
//     the mobile endpoints can be extracted into a BFF process then.
//
// ADR 0019 Push Privacy: notifications MUST NOT carry PII. Payloads are
// structurally constrained to {type, count, deep_link}. The
// POST /v1/mobile/push-tokens endpoint never logs the raw token.
package mobile

import "time"

// SummaryResponse is the Home screen aggregate for the mobile admin app.
// One fetch per refresh (not 5 separate ones). Cached 60 seconds per user.
type SummaryResponse struct {
	// PendingLiveViewCount is the number of live view requests awaiting
	// HR approval that the caller is permitted to see.
	PendingLiveViewCount int `json:"pending_live_view_count"`

	// PendingDSRCount is the number of DSRs in open|at_risk|overdue state.
	PendingDSRCount int `json:"pending_dsr_count"`

	// SilenceAlertsLast24h is the count of Flow 7 endpoint silence alerts
	// in the last 24 hours.
	SilenceAlertsLast24h int `json:"silence_alerts_last_24h"`

	// RecentAuditEntries is the last 5 mutating admin actions for the
	// quick-review feed. Scoped to actions the caller has permission to see.
	RecentAuditEntries []AuditEntryLite `json:"recent_audit_entries"`

	// DLPState is a cached module state snapshot so the mobile home screen
	// can show the DLP badge without a second round trip.
	DLPState string `json:"dlp_state"`
}

// AuditEntryLite is the minimal audit entry shape exposed to mobile.
// Full audit detail (hash chain, raw details) requires a web console.
type AuditEntryLite struct {
	ID        string    `json:"id"`
	At        time.Time `json:"at"`
	ActorRole string    `json:"actor_role"` // role label, NOT user ID (privacy + simplicity)
	Action    string    `json:"action"`
	TargetHint string   `json:"target_hint"` // e.g. "endpoint:abc123", "dsr:xyz"
}

// PushTokenRequest is the body for POST /v1/mobile/push-tokens.
type PushTokenRequest struct {
	// Token is the FCM or APNs device token. Sensitive — never logged,
	// never echoed back in responses.
	Token string `json:"token"`

	// Platform is "ios" or "android".
	Platform string `json:"platform"`

	// DeviceID is a stable per-device identifier from expo-device.
	// Used to deduplicate when the same user reinstalls the app.
	DeviceID string `json:"device_id"`
}

// PushTokenResponse confirms registration. Never includes the token.
type PushTokenResponse struct {
	RegisteredAt time.Time `json:"registered_at"`
	TokenHash    string    `json:"token_hash"` // sha256 prefix 16 for observability; NOT the raw token
}

// LiveViewPendingItem is a lightweight pending live view request for the
// mobile approval queue.
type LiveViewPendingItem struct {
	SessionID      string    `json:"session_id"`
	RequestedAt    time.Time `json:"requested_at"`
	RequesterRole  string    `json:"requester_role"` // role label, not name
	EndpointLabel  string    `json:"endpoint_label"` // "Endpoint XYZ" or employee display name if HR
	ReasonCode     string    `json:"reason_code"`
	TTLSeconds     int       `json:"ttl_seconds"`
	DeepLink       string    `json:"deep_link"` // personel://live-view/{session_id}
}

// DSRQueueItem is a lightweight DSR row for the mobile DSR queue.
type DSRQueueItem struct {
	ID              string    `json:"id"`
	CreatedAt       time.Time `json:"created_at"`
	RequestType     string    `json:"request_type"`
	State           string    `json:"state"`
	DaysRemaining   int       `json:"days_remaining"`
	EmployeeLabel   string    `json:"employee_label"` // display name scoped to caller's visibility
	DeepLink        string    `json:"deep_link"`
}

// SilenceAlertItem is a Flow 7 endpoint silence alert.
type SilenceAlertItem struct {
	EndpointID     string    `json:"endpoint_id"`
	EndpointLabel  string    `json:"endpoint_label"`
	LastSeenAt     time.Time `json:"last_seen_at"`
	State          string    `json:"state"` // NORMAL|SUSPICIOUS|DISABLED
	SilenceMinutes int       `json:"silence_minutes"`
	DeepLink       string    `json:"deep_link"`
}
