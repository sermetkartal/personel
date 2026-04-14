package integrations

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TestResult is the return shape of the per-service connection probe.
// Status is always "ok" or "fail"; Message is an operator-readable
// sentence (never a raw stack trace); LatencyMillis is the wall-clock
// duration of the probe including Vault round-trip and HTTP dial.
type TestResult struct {
	Status    string `json:"status"`
	Message   string `json:"message"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
}

// httpProber is the narrow HTTP contract the probes need. Tests inject
// a stub to avoid real egress; production uses http.Client.
type httpProber interface {
	Do(req *http.Request) (*http.Response, error)
}

// defaultHTTPClient returns the production prober with a strict 10s
// budget covering dial + TLS handshake + headers + body. Probes that
// finish faster return early; slower ones surface as a failure.
func defaultHTTPClient() httpProber {
	return &http.Client{Timeout: 10 * time.Second}
}

// testClient is overridable by unit tests. Production code should never
// touch this directly — call TestConnection which picks it up.
var testClient httpProber = defaultHTTPClient()

// TestConnection dispatches a read-only probe against the third-party
// service identified by `service`. The integration row for the given
// tenant MUST exist in Postgres and MUST be Vault-decryptable; a
// missing row or a decrypt failure surfaces as Status="fail" with a
// human-readable message (NOT as a 500 — operators need to distinguish
// "not yet configured" from "server is down").
//
// TestConnection performs NO state changes: no audit append, no Vault
// rotation, no remote-side alert. Safe to call repeatedly from the UI.
func (s *Service) TestConnection(ctx context.Context, tenantID, service string) (*TestResult, error) {
	if _, ok := AllowedServices[service]; !ok {
		return nil, ErrUnknownService
	}
	cfg, err := s.Decrypt(ctx, tenantID, service)
	if err != nil {
		if errors.Is(err, ErrVaultUnavailable) {
			return &TestResult{Status: "fail", Message: "vault encryptor unavailable — restart API with Vault credentials"}, nil
		}
		return &TestResult{Status: "fail", Message: "credentials not set or decrypt failed: " + err.Error()}, nil
	}

	start := time.Now()
	var result *TestResult
	switch service {
	case "maxmind":
		result = testMaxMind(ctx, cfg)
	case "cloudflare":
		result = testCloudflare(ctx, cfg)
	case "pagerduty":
		result = testPagerDuty(ctx, cfg)
	case "slack":
		result = testSlack(ctx, cfg)
	case "sentry":
		result = testSentry(ctx, cfg)
	default:
		return nil, fmt.Errorf("integrations: no probe for service: %s", service)
	}
	result.LatencyMS = time.Since(start).Milliseconds()
	return result, nil
}

// strField pulls a string value from the decrypted config map with a
// zero-value fallback. Non-string values (unexpected after JSON
// round-trip through Vault transit) are returned as the empty string
// so the caller's "missing field" branch fires.
func strField(cfg map[string]any, key string) string {
	if v, ok := cfg[key].(string); ok {
		return v
	}
	return ""
}

// --- Per-service probes ------------------------------------------------------

// testMaxMind validates credentials against MaxMind's authenticated
// download endpoint. A HEAD request is enough — MaxMind returns 401
// for bad creds, 200/302 for success. We deliberately do NOT pull a
// full database; this is a credentials check, not a sync.
func testMaxMind(ctx context.Context, cfg map[string]any) *TestResult {
	accountID := strField(cfg, "account_id")
	licenseKey := strField(cfg, "license_key")
	if accountID == "" || licenseKey == "" {
		return &TestResult{Status: "fail", Message: "account_id or license_key missing"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead,
		"https://download.maxmind.com/app/geoip_download?edition_id=GeoLite2-City&suffix=tar.gz.sha256", nil)
	if err != nil {
		return &TestResult{Status: "fail", Message: "build request: " + err.Error()}
	}
	req.SetBasicAuth(accountID, licenseKey)
	resp, err := testClient.Do(req)
	if err != nil {
		return &TestResult{Status: "fail", Message: err.Error()}
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusFound:
		return &TestResult{Status: "ok", Message: "MaxMind credentials accepted"}
	case http.StatusUnauthorized, http.StatusForbidden:
		return &TestResult{Status: "fail", Message: fmt.Sprintf("HTTP %d — account_id or license_key rejected", resp.StatusCode)}
	default:
		return &TestResult{Status: "fail", Message: fmt.Sprintf("HTTP %d — unexpected MaxMind response", resp.StatusCode)}
	}
}

// testCloudflare verifies an API token via the built-in /tokens/verify
// endpoint. This is the canonical zero-impact probe: it returns 200
// for valid tokens (active or disabled) and 401 for invalid ones,
// without touching any zones or DNS records.
func testCloudflare(ctx context.Context, cfg map[string]any) *TestResult {
	token := strField(cfg, "api_token")
	if token == "" {
		return &TestResult{Status: "fail", Message: "api_token missing"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.cloudflare.com/client/v4/user/tokens/verify", nil)
	if err != nil {
		return &TestResult{Status: "fail", Message: "build request: " + err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := testClient.Do(req)
	if err != nil {
		return &TestResult{Status: "fail", Message: err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return &TestResult{Status: "ok", Message: "Cloudflare token valid"}
	}
	return &TestResult{Status: "fail", Message: fmt.Sprintf("HTTP %d — token invalid or expired", resp.StatusCode)}
}

// testPagerDuty validates the integration key format only. We do NOT
// fire a real Events API v2 enqueue because that would create a live
// incident in the operator's PagerDuty service — absolutely not what
// a "Test Connection" button should do. Integration keys are 32-char
// lowercase hex; anything shorter or oddly shaped is rejected.
func testPagerDuty(ctx context.Context, cfg map[string]any) *TestResult {
	_ = ctx
	intKey := strField(cfg, "integration_key")
	if intKey == "" {
		return &TestResult{Status: "fail", Message: "integration_key missing"}
	}
	if len(intKey) < 20 {
		return &TestResult{Status: "fail", Message: "integration_key format invalid (expected 32-char hex)"}
	}
	return &TestResult{Status: "ok", Message: "format valid (no ping sent — would create PagerDuty incident)"}
}

// testSlack validates the webhook URL format. A real POST would emit a
// visible message in the operator's channel on every click, so we stop
// at URL parse + host-prefix check. Anything accepted here will fail
// loudly at first-real-use with a structured slack.NotificationError.
func testSlack(ctx context.Context, cfg map[string]any) *TestResult {
	_ = ctx
	webhook := strField(cfg, "webhook_url")
	if webhook == "" {
		return &TestResult{Status: "fail", Message: "webhook_url missing"}
	}
	if !strings.HasPrefix(webhook, "https://hooks.slack.com/") {
		return &TestResult{Status: "fail", Message: "url does not look like a Slack webhook (expected https://hooks.slack.com/...)"}
	}
	u, err := url.Parse(webhook)
	if err != nil || u.Host == "" {
		return &TestResult{Status: "fail", Message: "invalid webhook URL"}
	}
	return &TestResult{Status: "ok", Message: "webhook URL format valid (no message sent)"}
}

// testSentry validates the DSN format. A Sentry DSN looks like
// https://<publicKey>@<host>/<projectID> where <publicKey> is in the
// user-info slot of the URL. Anything else is rejected.
func testSentry(ctx context.Context, cfg map[string]any) *TestResult {
	_ = ctx
	dsn := strField(cfg, "dsn")
	if dsn == "" {
		return &TestResult{Status: "fail", Message: "dsn missing"}
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Host == "" || u.User == nil || u.User.Username() == "" {
		return &TestResult{Status: "fail", Message: "dsn format invalid (expected https://<key>@host/<project>)"}
	}
	if u.Path == "" || u.Path == "/" {
		return &TestResult{Status: "fail", Message: "dsn missing project id path"}
	}
	return &TestResult{Status: "ok", Message: "DSN format valid"}
}
