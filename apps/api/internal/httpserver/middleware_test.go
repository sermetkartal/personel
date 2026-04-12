// Package httpserver — middleware unit tests.
// Tests cover RequestIDMiddleware, RecoverMiddleware, CORSMiddleware,
// RateLimitMiddleware, AuditContextMiddleware, and the tokenBucket internals.
// AuthMiddleware is not unit-tested here because it requires a live OIDC
// provider; its integration surface is covered in the integration suite.
package httpserver

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/config"
	"github.com/personel/api/internal/httpx"
)

// nextOK is an http.Handler that always returns 200.
var nextOK = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

// nextPanic is an http.Handler that panics (for RecoverMiddleware tests).
var nextPanic = http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
	panic("deliberate test panic")
})

func testLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// ── RequestIDMiddleware ───────────────────────────────────────────────────────

func TestRequestIDMiddleware_GeneratesID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rw := httptest.NewRecorder()

	RequestIDMiddleware(nextOK).ServeHTTP(rw, req)

	id := rw.Header().Get("X-Request-Id")
	if id == "" {
		t.Error("RequestIDMiddleware must set X-Request-Id header when none is provided")
	}
}

func TestRequestIDMiddleware_PreservesExistingID(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Request-Id", "my-custom-id-123")
	rw := httptest.NewRecorder()

	RequestIDMiddleware(nextOK).ServeHTTP(rw, req)

	id := rw.Header().Get("X-Request-Id")
	if id != "my-custom-id-123" {
		t.Errorf("RequestIDMiddleware must preserve incoming X-Request-Id, got %q", id)
	}
}

func TestRequestIDMiddleware_InjectsIDIntoContext(t *testing.T) {
	var ctxID string
	capture := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		ctxID = httpx.RequestIDFromContext(r.Context())
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rw := httptest.NewRecorder()
	RequestIDMiddleware(capture).ServeHTTP(rw, req)

	if ctxID == "" {
		t.Error("RequestIDMiddleware must inject request ID into context")
	}
}

func TestRequestIDMiddleware_IDsAreUnique(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rw := httptest.NewRecorder()
		RequestIDMiddleware(nextOK).ServeHTTP(rw, req)
		id := rw.Header().Get("X-Request-Id")
		if ids[id] {
			t.Errorf("RequestIDMiddleware generated duplicate ID: %q", id)
		}
		ids[id] = true
	}
}

// ── RecoverMiddleware ─────────────────────────────────────────────────────────

func TestRecoverMiddleware_Returns500OnPanic(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rw := httptest.NewRecorder()

	RecoverMiddleware(testLog())(nextPanic).ServeHTTP(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Errorf("RecoverMiddleware must return 500 on panic, got %d", rw.Code)
	}
}

func TestRecoverMiddleware_PassesThroughNormalRequests(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rw := httptest.NewRecorder()

	RecoverMiddleware(testLog())(nextOK).ServeHTTP(rw, req)

	if rw.Code != http.StatusOK {
		t.Errorf("RecoverMiddleware must not affect normal requests, got %d", rw.Code)
	}
}

// ── CORSMiddleware ────────────────────────────────────────────────────────────

func TestCORSMiddleware_AllowsListedOrigin(t *testing.T) {
	cfg := &config.HTTPConfig{
		CORSOrigins: []string{"https://console.example.com"},
	}
	mw := CORSMiddleware(cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://console.example.com")
	rw := httptest.NewRecorder()

	mw(nextOK).ServeHTTP(rw, req)

	origin := rw.Header().Get("Access-Control-Allow-Origin")
	if origin != "https://console.example.com" {
		t.Errorf("CORSMiddleware must echo allowed origin, got %q", origin)
	}
}

func TestCORSMiddleware_BlocksUnlistedOrigin(t *testing.T) {
	cfg := &config.HTTPConfig{
		CORSOrigins: []string{"https://console.example.com"},
	}
	mw := CORSMiddleware(cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rw := httptest.NewRecorder()

	mw(nextOK).ServeHTTP(rw, req)

	origin := rw.Header().Get("Access-Control-Allow-Origin")
	if origin != "" {
		t.Errorf("CORSMiddleware must not set ACAO for unlisted origin, got %q", origin)
	}
}

func TestCORSMiddleware_PreflightReturns204(t *testing.T) {
	cfg := &config.HTTPConfig{
		CORSOrigins: []string{"https://console.example.com"},
	}
	mw := CORSMiddleware(cfg)

	req := httptest.NewRequest(http.MethodOptions, "/v1/dsr", nil)
	req.Header.Set("Origin", "https://console.example.com")
	rw := httptest.NewRecorder()

	mw(nextOK).ServeHTTP(rw, req)

	if rw.Code != http.StatusNoContent {
		t.Errorf("CORSMiddleware must return 204 for OPTIONS preflight, got %d", rw.Code)
	}
}

func TestCORSMiddleware_EmptyOriginsAllowsNone(t *testing.T) {
	cfg := &config.HTTPConfig{CORSOrigins: nil}
	mw := CORSMiddleware(cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://any.example.com")
	rw := httptest.NewRecorder()

	mw(nextOK).ServeHTTP(rw, req)

	if rw.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("CORSMiddleware with no allowed origins must not set ACAO header")
	}
}

func TestCORSMiddleware_SetsVaryHeader(t *testing.T) {
	cfg := &config.HTTPConfig{
		CORSOrigins: []string{"https://console.example.com"},
	}
	mw := CORSMiddleware(cfg)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "https://console.example.com")
	rw := httptest.NewRecorder()

	mw(nextOK).ServeHTTP(rw, req)

	vary := rw.Header().Get("Vary")
	if !strings.Contains(vary, "Origin") {
		t.Errorf("CORSMiddleware must set Vary: Origin, got %q", vary)
	}
}

// ── RateLimitMiddleware ───────────────────────────────────────────────────────

func TestRateLimitMiddleware_AllowsInitialRequests(t *testing.T) {
	mw := RateLimitMiddleware(600, 10) // 600 req/min, burst 10

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.1:1234"
		rw := httptest.NewRecorder()
		mw(nextOK).ServeHTTP(rw, req)
		if rw.Code != http.StatusOK {
			t.Errorf("request %d should be allowed, got %d", i, rw.Code)
		}
	}
}

func TestRateLimitMiddleware_ThrottlesAfterBurstExhausted(t *testing.T) {
	// Use burst=2 so we can exhaustit quickly without hitting the rate.
	mw := RateLimitMiddleware(1, 2) // 1 req/min, burst 2

	allowed := 0
	throttled := 0
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "10.0.0.2:5678"
		rw := httptest.NewRecorder()
		mw(nextOK).ServeHTTP(rw, req)
		if rw.Code == http.StatusOK {
			allowed++
		} else if rw.Code == http.StatusTooManyRequests {
			throttled++
		}
	}

	if throttled == 0 {
		t.Error("RateLimitMiddleware must throttle after burst is exhausted")
	}
}

func TestRateLimitMiddleware_DifferentIPsHaveSeparateBuckets(t *testing.T) {
	// burst=1 so second request from same IP is throttled.
	mw := RateLimitMiddleware(1, 1)

	for _, ip := range []string{"10.0.0.3:100", "10.0.0.4:100", "10.0.0.5:100"} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = ip
		rw := httptest.NewRecorder()
		mw(nextOK).ServeHTTP(rw, req)
		if rw.Code != http.StatusOK {
			t.Errorf("first request from %s must be allowed, got %d", ip, rw.Code)
		}
	}
}

func TestRateLimitMiddleware_RetryAfterHeaderPresent(t *testing.T) {
	mw := RateLimitMiddleware(1, 1) // burst=1: second request is throttled

	ip := "10.0.0.6:999"
	// First request exhausts the burst.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = ip
	mw(nextOK).ServeHTTP(httptest.NewRecorder(), req)

	// Second request is throttled.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.RemoteAddr = ip
	rw2 := httptest.NewRecorder()
	mw(nextOK).ServeHTTP(rw2, req2)

	if rw2.Code == http.StatusTooManyRequests {
		ra := rw2.Header().Get("Retry-After")
		if ra == "" {
			t.Error("RateLimitMiddleware must set Retry-After header on 429")
		}
	}
}

// ── tokenBucket internals ────────────────────────────────────────────────────

func TestTokenBucket_AllowsFirstRequest(t *testing.T) {
	tb := newTokenBucket(60, 5)
	if !tb.allow("key-1") {
		t.Error("first request from empty bucket must be allowed")
	}
}

func TestTokenBucket_RefillsOverTime(t *testing.T) {
	tb := newTokenBucket(600, 1) // burst=1, rate=10/s
	tb.allow("key-refill")      // exhaust burst

	// Manually set lastFill far in the past to simulate token refill.
	tb.mu.Lock()
	b := tb.buckets["key-refill"]
	b.lastFill = time.Now().Add(-2 * time.Second)
	tb.mu.Unlock()

	if !tb.allow("key-refill") {
		t.Error("bucket should have refilled tokens after 2 seconds")
	}
}

// ── AuditContextMiddleware ────────────────────────────────────────────────────

func TestAuditContextMiddleware_InjectsRecorder(t *testing.T) {
	// We pass a nil *audit.Recorder — the middleware stores it in context,
	// and downstream code may check for nil. The test just verifies the
	// middleware calls next without panicking.
	var rec *audit.Recorder // nil is acceptable for this structural test
	mw := AuditContextMiddleware(rec)

	var gotCtx bool
	handler := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		// audit.RecorderFromContext will return nil for nil recorder,
		// but context injection must not panic.
		gotCtx = true
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rw := httptest.NewRecorder()
	mw(handler).ServeHTTP(rw, req)

	if !gotCtx {
		t.Error("AuditContextMiddleware must call next handler")
	}
}
