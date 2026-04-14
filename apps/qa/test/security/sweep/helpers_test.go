//go:build security

// Package sweep — Faz 14 #152 security payload sweep suite.
//
// Separate subpackage so the payload sweep tests can use the
// `security` build tag without colliding with the existing
// keystroke_admin_blindness_test.go helpers in the parent
// package (which uses a different helper signature and is
// untagged). Both suites run under `-tags=security`; this
// split is only about Go symbol scoping.
package sweep

import (
	"os"
	"testing"
)

// getAPIURL returns the admin API base URL. Defaults to the vm3
// pilot if not set via env.
func getAPIURL(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("PERSONEL_API_URL"); v != "" {
		return v
	}
	return "http://192.168.5.44:8000"
}

// getConsoleURL returns the console base URL.
func getConsoleURL(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("PERSONEL_CONSOLE_URL"); v != "" {
		return v
	}
	return "http://192.168.5.44:3000"
}

// getGatewayAddr returns the gateway host:port.
func getGatewayAddr(t *testing.T) string {
	t.Helper()
	if v := os.Getenv("PERSONEL_GATEWAY_ADDR"); v != "" {
		return v
	}
	return "192.168.5.44:9443"
}

// getAdminToken returns a pre-fetched admin bearer token from
// the environment. Uses a distinct name from the existing
// harness-based `getAdminToken` in keystroke_admin_blindness_test.go.
// The payload sweep tests are stack-agnostic — ops pre-fetches a
// token via Keycloak password grant and exports PERSONEL_ADMIN_TOKEN.
func getAdminToken(t *testing.T) string {
	t.Helper()
	return os.Getenv("PERSONEL_ADMIN_TOKEN")
}
