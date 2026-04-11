// Package fixtures provides deterministic, reusable test data for the Personel
// QA framework.
//
// All IDs, addresses, and payloads in this package are fixed constants — they
// never depend on randomness so that test assertions remain stable across runs
// and environments. Tests that need randomised data should use the simulator's
// EventGenerator with an explicit seed.
//
// Usage:
//
//	import "github.com/personel/qa/internal/fixtures"
//
//	func TestSomething(t *testing.T) {
//	    ep := fixtures.Endpoint1
//	    ...
//	}
package fixtures

import "time"

// ---------------------------------------------------------------------------
// Tenant identifiers
// ---------------------------------------------------------------------------

const (
	// TenantA is the primary test tenant used across most e2e tests.
	TenantA = "00000000-0000-0000-0000-000000000001"

	// TenantB is used for multi-tenant isolation tests.
	TenantB = "00000000-0000-0000-0000-000000000002"

	// TenantDPO is used for legal-hold and DSR tests where the DPO role matters.
	TenantDPO = "00000000-0000-0000-0000-000000000003"
)

// ---------------------------------------------------------------------------
// Endpoint identifiers
// ---------------------------------------------------------------------------

const (
	// Endpoint1–5 are stable endpoint IDs belonging to TenantA.
	Endpoint1 = "ep-00000000-0000-0000-0000-000000000001"
	Endpoint2 = "ep-00000000-0000-0000-0000-000000000002"
	Endpoint3 = "ep-00000000-0000-0000-0000-000000000003"
	Endpoint4 = "ep-00000000-0000-0000-0000-000000000004"
	Endpoint5 = "ep-00000000-0000-0000-0000-000000000005"

	// EndpointLegalHold is the endpoint used in legal-hold e2e tests.
	EndpointLegalHold = "ep-00000000-0000-0000-0001-000000000001"

	// EndpointRedTeam is the endpoint used in the keystroke admin-blindness red
	// team. It has a PE-DEK row pre-seeded in the test database.
	EndpointRedTeam = "ep-00000000-0000-0000-ffff-000000000001"
)

// ---------------------------------------------------------------------------
// User identifiers (used in RBAC and live-view tests)
// ---------------------------------------------------------------------------

const (
	UserAdmin    = "user-admin-0000-0000-0000-000000000001"
	UserManager  = "user-mgr-00000-0000-0000-000000000002"
	UserHR       = "user-hr-000000-0000-0000-000000000003"
	UserDPO      = "user-dpo-00000-0000-0000-000000000004"
	UserEmployee = "user-emp-00000-0000-0000-000000000005"

	// UserLiveViewRequester and UserLiveViewApprover are distinct users so that
	// dual-control assertions always pass.
	UserLiveViewRequester = "user-lv-req-0000-0000-0000-000000000006"
	UserLiveViewApprover  = "user-lv-apr-0000-0000-0000-000000000007"
)

// ---------------------------------------------------------------------------
// Key version constants
// ---------------------------------------------------------------------------

const (
	// CurrentPEDEKVersion is the PE-DEK version that the gateway accepts without
	// issuing a RotateCert. Agents presenting a lower version get RotateCert(rekey).
	CurrentPEDEKVersion uint32 = 1

	// CurrentTMKVersion is the TMK version currently active in Vault.
	CurrentTMKVersion uint32 = 1

	// StaleTMKVersion is used in key-rotation tests to trigger RotateCert.
	StaleTMKVersion uint32 = 0
)

// ---------------------------------------------------------------------------
// Time anchors (deterministic, not time.Now())
// ---------------------------------------------------------------------------

var (
	// EpochPhase1 is the synthetic "start of Phase 1 pilot" timestamp used in
	// date-range assertions so tests don't drift as calendar time advances.
	EpochPhase1 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// LegalHoldFrom is the start of the legal-hold date range in legalhold_test.go.
	LegalHoldFrom = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// LegalHoldTo is the end of the legal-hold date range.
	LegalHoldTo = time.Date(2026, 4, 10, 23, 59, 59, 0, time.UTC)

	// DSRCreatedAt is used in DSR SLA timer tests.
	DSRCreatedAt = time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
)

// ---------------------------------------------------------------------------
// Synthetic TCKN (Turkish national ID) values for red-team tests
// ---------------------------------------------------------------------------

const (
	// SyntheticTCKN is the test Turkish national ID number injected into
	// keystroke content during the admin-blindness red team.
	// It is deliberately not a real TCKN — it starts with 1 (valid format) but
	// is chosen to be globally unique as a test marker.
	SyntheticTCKN = "12345678901"

	// SyntheticTCKN2 is a second TCKN used to test that multi-TCKN payloads are
	// also rejected.
	SyntheticTCKN2 = "98765432109"
)

// ---------------------------------------------------------------------------
// Event payloads
// ---------------------------------------------------------------------------

// WindowTitlePayload returns a deterministic JSON payload for a
// window.title_changed event.
func WindowTitlePayload(windowTitle string) []byte {
	return []byte(`{"window_title":"` + jsonEscape(windowTitle) + `","pid":1234,"process_name":"chrome.exe"}`)
}

// ProcessStartPayload returns a deterministic JSON payload for a process.start
// event.
func ProcessStartPayload(exeName string) []byte {
	return []byte(`{"exe":"` + jsonEscape(exeName) + `","pid":9999,"parent_pid":1,"cmdline":"` + jsonEscape(exeName) + ` --started-by=fixture","user":"TESTUSER"}`)
}

// NetworkFlowPayload returns a deterministic network.flow_summary payload.
func NetworkFlowPayload(destIP string, port uint16) []byte {
	return []byte(`{"dest_ip":"` + destIP + `","dest_port":` + uintStr(uint64(port)) + `,"proto":"TCP","bytes_sent":1024,"bytes_recv":4096,"duration_ms":250}`)
}

// SensitiveWindowTitlePayload returns a window title that matches the Turkish
// sensitivity regex patterns used in TestSensitiveBucketRouting:
//
//	window_title_sensitive_regex: [".*saglik.*", ".*sendika.*", ".*din.*"]
func SensitiveWindowTitlePayload() []byte {
	return WindowTitlePayload("saglik.gov.tr — Randevu Sistemi")
}

// ---------------------------------------------------------------------------
// HTTP header sets (for use in API tests)
// ---------------------------------------------------------------------------

// AdminHeaders returns standard headers for an admin user request.
func AdminHeaders() map[string]string {
	return map[string]string{
		"Authorization": "Bearer test-admin-token-super-admin",
		"X-Test-Role":   "super_admin",
		"Content-Type":  "application/json",
	}
}

// DPOHeaders returns headers for a Data Protection Officer request.
func DPOHeaders() map[string]string {
	return map[string]string{
		"Authorization": "Bearer test-dpo-token",
		"X-Test-Role":   "dpo",
		"Content-Type":  "application/json",
	}
}

// HRHeaders returns headers for an HR user request.
func HRHeaders() map[string]string {
	return map[string]string{
		"Authorization": "Bearer test-hr-token",
		"X-Test-Role":   "hr",
		"Content-Type":  "application/json",
	}
}

// UnauthenticatedHeaders returns headers with no auth — used to verify 401/403
// responses on protected endpoints.
func UnauthenticatedHeaders() map[string]string {
	return map[string]string{
		"Content-Type": "application/json",
	}
}

// ---------------------------------------------------------------------------
// Legal hold request bodies
// ---------------------------------------------------------------------------

// LegalHoldBody returns the standard legal hold placement request body for
// TestLegalHoldPlacementAndRelease.
func LegalHoldBody() string {
	return `{
	"reason_code": "legal_proceeding",
	"ticket_id": "LEGAL-2026-001",
	"justification": "Ongoing litigation requires preservation of communications",
	"scope": {
		"endpoint_ids": ["` + EndpointLegalHold + `"],
		"date_range": {"from": "2026-01-01T00:00:00Z", "to": "2026-04-10T23:59:59Z"},
		"event_types": ["keystroke.window_stats", "window.title_changed", "screenshot.captured"]
	},
	"max_duration_days": 365
}`
}

// LegalHoldReleaseBody returns the standard release request body.
func LegalHoldReleaseBody() string {
	return `{"justification": "Litigation concluded; hold no longer needed"}`
}

// ---------------------------------------------------------------------------
// DSR request bodies
// ---------------------------------------------------------------------------

// DSRSubmissionBody returns a KVKK Article 11 data subject request body.
func DSRSubmissionBody(subjectEmail string) string {
	return `{
	"type": "access",
	"subject_email": "` + jsonEscape(subjectEmail) + `",
	"subject_name": "Test Subject",
	"identity_verified": true,
	"description": "KVKK Article 11 — right of access request"
}`
}

// ---------------------------------------------------------------------------
// Internal helpers (unexported)
// ---------------------------------------------------------------------------

// jsonEscape escapes a string for embedding in a JSON value. Only handles
// double-quotes and backslashes — sufficient for deterministic fixture values.
func jsonEscape(s string) string {
	out := make([]byte, 0, len(s)+4)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			out = append(out, '\\', '"')
		case '\\':
			out = append(out, '\\', '\\')
		default:
			out = append(out, s[i])
		}
	}
	return string(out)
}

// uintStr converts a uint64 to its decimal string representation without
// importing strconv (to keep the fixture package dependency-free).
func uintStr(n uint64) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
