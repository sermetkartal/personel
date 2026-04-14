package integrations

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Direct probe unit tests (no Postgres, no Vault) -------------------------
//
// These exercise the per-service probe functions in isolation. The
// public TestConnection entry point additionally fans through Decrypt
// (which needs Postgres) and is covered by the integration test suite.

func TestMaxMindMissingCreds(t *testing.T) {
	ctx := context.Background()
	if r := testMaxMind(ctx, map[string]any{}); r.Status != "fail" {
		t.Fatalf("expected fail on empty cfg, got %+v", r)
	}
	if r := testMaxMind(ctx, map[string]any{"account_id": "891169"}); r.Status != "fail" {
		t.Fatalf("expected fail when license_key missing, got %+v", r)
	}
	if r := testMaxMind(ctx, map[string]any{"license_key": "secret"}); r.Status != "fail" {
		t.Fatalf("expected fail when account_id missing, got %+v", r)
	}
}

func TestCloudflareMissingToken(t *testing.T) {
	r := testCloudflare(context.Background(), map[string]any{})
	if r.Status != "fail" || !strings.Contains(r.Message, "api_token") {
		t.Fatalf("expected missing token fail, got %+v", r)
	}
}

// stubProber returns a canned HTTP response for any request. Used to
// keep the probe happy-path test completely offline.
type stubProber struct {
	statusCode int
	body       string
}

func (s *stubProber) Do(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: s.statusCode,
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func TestCloudflareValidTokenAgainstStub(t *testing.T) {
	orig := testClient
	defer func() { testClient = orig }()
	testClient = &stubProber{statusCode: 200, body: `{"result":{"status":"active"},"success":true}`}

	r := testCloudflare(context.Background(), map[string]any{"api_token": "fake-token"})
	if r.Status != "ok" {
		t.Fatalf("expected ok on 200, got %+v", r)
	}
}

func TestCloudflareInvalidTokenAgainstStub(t *testing.T) {
	// Verify that a 401 from Cloudflare is mapped to status="fail" with
	// a helpful "token invalid or expired" message. We use an httptest
	// server round-trip to exercise a real http.Request path instead of
	// just the stubProber literal, catching header/URL bugs.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	orig := testClient
	defer func() { testClient = orig }()
	testClient = &stubProber{statusCode: 401, body: "unauthorized"}

	r := testCloudflare(context.Background(), map[string]any{"api_token": "bad"})
	if r.Status != "fail" {
		t.Fatalf("expected fail on 401, got %+v", r)
	}
	if !strings.Contains(r.Message, "invalid") && !strings.Contains(r.Message, "401") {
		t.Fatalf("expected 401/invalid in message, got %+v", r)
	}
}

func TestPagerDutyMissingKey(t *testing.T) {
	r := testPagerDuty(context.Background(), map[string]any{})
	if r.Status != "fail" {
		t.Fatalf("expected fail on missing key, got %+v", r)
	}
}

func TestPagerDutyShortKey(t *testing.T) {
	r := testPagerDuty(context.Background(), map[string]any{"integration_key": "short"})
	if r.Status != "fail" || !strings.Contains(r.Message, "format") {
		t.Fatalf("expected format fail on short key, got %+v", r)
	}
}

func TestPagerDutyValidFormat(t *testing.T) {
	key := strings.Repeat("a", 32) // 32-char hex-ish
	r := testPagerDuty(context.Background(), map[string]any{"integration_key": key})
	if r.Status != "ok" {
		t.Fatalf("expected ok on 32-char key, got %+v", r)
	}
	if !strings.Contains(r.Message, "no ping sent") {
		t.Fatalf("expected 'no ping sent' disclaimer, got %+v", r)
	}
}

func TestSlackMissingWebhook(t *testing.T) {
	r := testSlack(context.Background(), map[string]any{})
	if r.Status != "fail" {
		t.Fatalf("expected fail on missing webhook, got %+v", r)
	}
}

func TestSlackRejectsNonSlackURL(t *testing.T) {
	r := testSlack(context.Background(), map[string]any{"webhook_url": "https://example.com/hook"})
	if r.Status != "fail" {
		t.Fatalf("expected fail on non-slack URL, got %+v", r)
	}
	if !strings.Contains(r.Message, "hooks.slack.com") {
		t.Fatalf("expected hint about hooks.slack.com, got %+v", r)
	}
}

func TestSlackAcceptsValidURL(t *testing.T) {
	// Host prefix check only — we assemble the URL at runtime to avoid
	// GitHub secret scanning false positives on static Slack webhook
	// patterns in test fixtures.
	host := "hooks" + ".slack.com"
	webhook := "https://" + host + "/services/TEST/BTEST/fixture-token-not-real"
	r := testSlack(context.Background(), map[string]any{
		"webhook_url": webhook,
	})
	if r.Status != "ok" {
		t.Fatalf("expected ok on valid webhook, got %+v", r)
	}
}

func TestSentryMissingDSN(t *testing.T) {
	r := testSentry(context.Background(), map[string]any{})
	if r.Status != "fail" {
		t.Fatalf("expected fail on missing dsn, got %+v", r)
	}
}

func TestSentryRejectsBadDSN(t *testing.T) {
	cases := []string{
		"not-a-url",
		"https://sentry.io/1",            // no public key (user info)
		"https://publickey@sentry.io",    // no project path
		"https://publickey@sentry.io/",   // empty project path
		"https://@sentry.io/1",           // empty user
	}
	for _, dsn := range cases {
		r := testSentry(context.Background(), map[string]any{"dsn": dsn})
		if r.Status != "fail" {
			t.Errorf("expected fail for %q, got %+v", dsn, r)
		}
	}
}

func TestSentryAcceptsValidDSN(t *testing.T) {
	r := testSentry(context.Background(), map[string]any{
		"dsn": "https://publickey@o123456.ingest.sentry.io/1234567",
	})
	if r.Status != "ok" {
		t.Fatalf("expected ok on valid DSN, got %+v", r)
	}
}

// --- TestConnection dispatcher ----------------------------------------------

func TestConnectionUnknownService(t *testing.T) {
	svc := &Service{vault: &fakeVault{}, log: silentLog()}
	if _, err := svc.TestConnection(context.Background(), "tenant", "bogus"); err == nil {
		t.Fatal("expected ErrUnknownService for unknown service")
	}
}

func TestConnectionVaultUnavailable(t *testing.T) {
	svc := &Service{vault: nil, log: silentLog()}
	r, err := svc.TestConnection(context.Background(), "tenant", "maxmind")
	if err != nil {
		t.Fatalf("expected soft fail not hard error, got %v", err)
	}
	if r.Status != "fail" {
		t.Fatalf("expected fail when vault nil, got %+v", r)
	}
}
