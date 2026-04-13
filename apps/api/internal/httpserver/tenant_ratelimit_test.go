// Package httpserver — tests for TenantLimiter and TenantRateLimitMiddleware.
// Uses an injected clock so bucket refill / sweep can be exercised
// deterministically without time.Sleep.
package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/personel/api/internal/auth"
)

// fakeClock provides a manually-advanced time source.
type fakeClock struct{ now time.Time }

func (f *fakeClock) Now() time.Time          { return f.now }
func (f *fakeClock) Advance(d time.Duration) { f.now = f.now.Add(d) }

// ── TenantLimiter.Allow ──────────────────────────────────────────────────────

func TestTenantLimiter_BurstThenReject(t *testing.T) {
	lim := NewTenantLimiter(60, 5, testLog()) // 1 tok/s, burst 5
	clock := &fakeClock{now: time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)}
	lim.SetNowFn(clock.Now)

	// First request allocates the bucket pre-filled to burst=5 and then
	// consumes one token, leaving 4. Four more calls should all pass.
	for i := 0; i < 5; i++ {
		if !lim.Allow("tenant-1") {
			t.Fatalf("request %d of burst must be allowed", i+1)
		}
	}
	// The sixth request within the same instant must be rejected.
	if lim.Allow("tenant-1") {
		t.Error("burst+1 request must be rejected")
	}
}

func TestTenantLimiter_Refill(t *testing.T) {
	lim := NewTenantLimiter(60, 2, testLog()) // 1 tok/s, burst 2
	clock := &fakeClock{now: time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)}
	lim.SetNowFn(clock.Now)

	// Exhaust the bucket.
	if !lim.Allow("tenant-1") {
		t.Fatal("first request must pass")
	}
	if !lim.Allow("tenant-1") {
		t.Fatal("second request must pass")
	}
	if lim.Allow("tenant-1") {
		t.Fatal("third request must fail")
	}

	// Advance 1.5s — should regain at least 1 full token.
	clock.Advance(1500 * time.Millisecond)
	if !lim.Allow("tenant-1") {
		t.Error("after refill, request must pass")
	}
}

func TestTenantLimiter_IsolatesTenants(t *testing.T) {
	lim := NewTenantLimiter(60, 2, testLog())
	clock := &fakeClock{now: time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)}
	lim.SetNowFn(clock.Now)

	// Drain tenant-A.
	lim.Allow("tenant-A")
	lim.Allow("tenant-A")
	if lim.Allow("tenant-A") {
		t.Fatal("tenant-A should be exhausted")
	}

	// tenant-B must still have its own full burst.
	if !lim.Allow("tenant-B") {
		t.Error("tenant-B must be unaffected by tenant-A throttling")
	}
	if !lim.Allow("tenant-B") {
		t.Error("tenant-B second request must pass")
	}
}

func TestTenantLimiter_EmptyTenantIDPassesThrough(t *testing.T) {
	lim := NewTenantLimiter(1, 1, testLog()) // impossibly tight
	for i := 0; i < 100; i++ {
		if !lim.Allow("") {
			t.Fatal("empty tenant ID must always pass")
		}
	}
}

func TestTenantLimiter_ZeroRateDisables(t *testing.T) {
	lim := NewTenantLimiter(0, 0, testLog())
	for i := 0; i < 1000; i++ {
		if !lim.Allow("tenant-1") {
			t.Fatal("zero-rate limiter must always allow")
		}
	}
}

// ── sweep eviction ───────────────────────────────────────────────────────────

func TestTenantLimiter_SweepEvictsIdleBuckets(t *testing.T) {
	lim := NewTenantLimiter(60, 10, testLog())
	clock := &fakeClock{now: time.Date(2026, 4, 13, 12, 0, 0, 0, time.UTC)}
	lim.SetNowFn(clock.Now)

	lim.Allow("tenant-1")
	lim.Allow("tenant-2")
	if got := lim.BucketCount(); got != 2 {
		t.Fatalf("BucketCount=%d, want 2", got)
	}

	// Advance well past idleTTL (1h) and trigger the sweep via a
	// request for a third tenant.
	clock.Advance(2 * time.Hour)
	lim.Allow("tenant-3")

	if got := lim.BucketCount(); got != 1 {
		t.Errorf("after sweep BucketCount=%d, want 1 (only tenant-3)", got)
	}
}

// ── Middleware behaviour ─────────────────────────────────────────────────────

func TestTenantRateLimitMiddleware_429OnExhaust(t *testing.T) {
	lim := NewTenantLimiter(60, 2, testLog())
	mw := TenantRateLimitMiddleware(lim)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	p := &auth.Principal{UserID: "u", TenantID: "tenant-1", Roles: []auth.Role{auth.RoleAdmin}}

	// Two requests in the burst should pass.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/v1/anything", nil).
			WithContext(auth.WithPrincipal(httptest.NewRequest(http.MethodGet, "/", nil).Context(), p))
		rw := httptest.NewRecorder()
		handler.ServeHTTP(rw, req)
		if rw.Code != http.StatusOK {
			t.Errorf("burst[%d]: code=%d, want 200", i, rw.Code)
		}
	}

	// Third request must 429 with Retry-After set.
	req := httptest.NewRequest(http.MethodGet, "/v1/anything", nil).
		WithContext(auth.WithPrincipal(httptest.NewRequest(http.MethodGet, "/", nil).Context(), p))
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)
	if rw.Code != http.StatusTooManyRequests {
		t.Errorf("code=%d, want 429", rw.Code)
	}
	if rw.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header must be set on 429")
	}
}

func TestTenantRateLimitMiddleware_MissingPrincipalPassesThrough(t *testing.T) {
	lim := NewTenantLimiter(60, 1, testLog())
	mw := TenantRateLimitMiddleware(lim)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// No principal in context.
	req := httptest.NewRequest(http.MethodGet, "/v1/anything", nil)
	rw := httptest.NewRecorder()
	handler.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Errorf("missing principal must pass through: code=%d", rw.Code)
	}
}

func TestTenantRateLimitMiddleware_IsolatesTenants(t *testing.T) {
	lim := NewTenantLimiter(60, 1, testLog())
	mw := TenantRateLimitMiddleware(lim)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	pA := &auth.Principal{UserID: "u", TenantID: "tenant-A"}
	pB := &auth.Principal{UserID: "u", TenantID: "tenant-B"}

	// tenant-A uses its single-token burst.
	reqA := httptest.NewRequest(http.MethodGet, "/v1/x", nil).
		WithContext(auth.WithPrincipal(httptest.NewRequest(http.MethodGet, "/", nil).Context(), pA))
	rwA := httptest.NewRecorder()
	handler.ServeHTTP(rwA, reqA)
	if rwA.Code != http.StatusOK {
		t.Fatalf("tenant-A first request: code=%d", rwA.Code)
	}

	// tenant-A's next request must 429...
	rwA2 := httptest.NewRecorder()
	reqA2 := httptest.NewRequest(http.MethodGet, "/v1/x", nil).
		WithContext(auth.WithPrincipal(httptest.NewRequest(http.MethodGet, "/", nil).Context(), pA))
	handler.ServeHTTP(rwA2, reqA2)
	if rwA2.Code != http.StatusTooManyRequests {
		t.Errorf("tenant-A second request: code=%d, want 429", rwA2.Code)
	}

	// ...but tenant-B must still be fine.
	rwB := httptest.NewRecorder()
	reqB := httptest.NewRequest(http.MethodGet, "/v1/x", nil).
		WithContext(auth.WithPrincipal(httptest.NewRequest(http.MethodGet, "/", nil).Context(), pB))
	handler.ServeHTTP(rwB, reqB)
	if rwB.Code != http.StatusOK {
		t.Errorf("tenant-B must be unaffected: code=%d", rwB.Code)
	}
}
