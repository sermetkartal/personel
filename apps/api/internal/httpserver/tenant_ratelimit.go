// Package httpserver — tenant_ratelimit.go implements Faz 6 #71:
// per-tenant token-bucket rate limiting as a second layer BEHIND the
// existing per-IP RateLimitMiddleware.
//
// Rationale:
//
//   - Per-IP alone is not enough in a multi-tenant deployment. One rogue
//     tenant running a noisy dashboard behind a corporate NAT could bring
//     down the API for every other tenant because all their requests
//     arrive from one egress IP.
//
//   - Per-tenant alone is not enough either. A single anonymous attacker
//     with no principal could still flood the /v1/agent-enroll path (which
//     is outside the auth group and thus outside this middleware).
//
// Layered strategy: per-IP middleware is mounted globally (top of the
// stack, runs before auth); per-tenant middleware mounts AFTER
// AuthMiddleware so the principal is populated. Pre-auth traffic is
// limited per IP; post-auth traffic is limited per IP AND per tenant.
//
// Token-bucket semantics:
//
//   - Lazy bucket allocation. Tenants with no traffic consume zero memory.
//
//   - Lazy refill on each Allow() call. No background goroutine per
//     bucket — the bucket just reads (now - lastRefill) and adds tokens.
//
//   - Periodic sweep removes buckets that have been idle for >1h to keep
//     the map from growing unbounded as tenants churn.
//
//   - Injectable clock (nowFn) so tests can fast-forward time without
//     calling time.Sleep.
package httpserver

import (
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// TenantLimiter is a concurrent-safe token-bucket rate limiter keyed by
// tenant ID. Construct with NewTenantLimiter.
type TenantLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tenantBucket

	// rate is tokens-per-second (the input is per-minute but we work in
	// per-second so elapsed * rate is directly comparable to tokens).
	rate float64

	// burst is the maximum tokens a bucket can accumulate. New buckets
	// start pre-filled to burst so a fresh tenant doesn't get throttled
	// on its very first request.
	burst float64

	// nowFn is injected for tests. Production uses time.Now.
	nowFn func() time.Time

	// idleTTL is how long a bucket may remain unused before the sweeper
	// evicts it. Defaults to 1 hour.
	idleTTL time.Duration

	// lastSweep is updated on every Allow call; the sweeper runs inline
	// when it notices enough time has passed, so there is no background
	// goroutine to stop during shutdown.
	lastSweep time.Time

	log *slog.Logger
}

// tenantBucket is one per tenant. The mutex on TenantLimiter protects
// all bucket mutations so individual buckets do not need their own lock.
type tenantBucket struct {
	tokens     float64
	lastRefill time.Time
	lastUsed   time.Time
}

// NewTenantLimiter constructs a limiter with the given per-minute rate and
// burst size. A zero or negative rate disables the limiter (Allow always
// returns true) which is the sane behaviour when the operator omits the
// per-tenant block from api.yaml entirely.
func NewTenantLimiter(ratePerMin, burst int, log *slog.Logger) *TenantLimiter {
	if log == nil {
		log = slog.Default()
	}
	return &TenantLimiter{
		buckets:   make(map[string]*tenantBucket),
		rate:      float64(ratePerMin) / 60.0,
		burst:     float64(burst),
		nowFn:     time.Now,
		idleTTL:   time.Hour,
		lastSweep: time.Now(),
		log:       log,
	}
}

// SetNowFn overrides the clock for tests. MUST NOT be called while
// requests are in flight — only at test setup.
func (t *TenantLimiter) SetNowFn(f func() time.Time) {
	t.nowFn = f
	t.lastSweep = f()
}

// Allow consumes one token for the given tenant and reports whether the
// request may proceed. Empty tenantID is treated as a pass-through: post
// auth this should not happen, but making it defensive avoids an entire
// class of subtle bugs where a badly-populated Principal silently breaks
// the API.
func (t *TenantLimiter) Allow(tenantID string) bool {
	if tenantID == "" {
		return true
	}
	if t.rate <= 0 {
		return true
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	now := t.nowFn()

	// Opportunistic sweep. O(buckets) once per idleTTL window is cheap
	// relative to the per-request work we do anyway, and it means we
	// never need a dedicated goroutine or explicit shutdown.
	if now.Sub(t.lastSweep) >= t.idleTTL {
		t.sweepLocked(now)
		t.lastSweep = now
	}

	b, ok := t.buckets[tenantID]
	if !ok {
		// First request for this tenant: allocate pre-filled bucket
		// and consume one token.
		t.buckets[tenantID] = &tenantBucket{
			tokens:     t.burst - 1,
			lastRefill: now,
			lastUsed:   now,
		}
		return true
	}

	// Lazy refill: add (elapsed * rate) tokens, capped at burst.
	elapsed := now.Sub(b.lastRefill).Seconds()
	if elapsed > 0 {
		b.tokens += elapsed * t.rate
		if b.tokens > t.burst {
			b.tokens = t.burst
		}
		b.lastRefill = now
	}
	b.lastUsed = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweepLocked evicts buckets not touched within idleTTL. Called with
// t.mu already held.
func (t *TenantLimiter) sweepLocked(now time.Time) {
	removed := 0
	for k, b := range t.buckets {
		if now.Sub(b.lastUsed) > t.idleTTL {
			delete(t.buckets, k)
			removed++
		}
	}
	if removed > 0 && t.log != nil {
		t.log.Debug("tenant rate limiter sweep",
			slog.Int("evicted", removed),
			slog.Int("remaining", len(t.buckets)),
		)
	}
}

// BucketCount returns the current number of active tenant buckets.
// Exposed for tests and for a future Prometheus gauge.
func (t *TenantLimiter) BucketCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.buckets)
}

// TenantRateLimitMiddleware returns a middleware that enforces the given
// TenantLimiter. MUST be mounted AFTER AuthMiddleware so the principal is
// available in context. 429 responses include a Retry-After header
// computed from the per-second rate.
//
// Empty principals (should not happen post-auth) are passed through to
// avoid a silent denial of service when the auth middleware is
// accidentally re-ordered — the failure mode is "not enforced" rather
// than "entire API down".
func TenantRateLimitMiddleware(limiter *TenantLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := auth.PrincipalFromContext(r.Context())
			if p == nil || p.TenantID == "" {
				next.ServeHTTP(w, r)
				return
			}
			if !limiter.Allow(p.TenantID) {
				// Retry-After in seconds: at least 1, rounded up from
				// the time it takes to accrue a full token at the
				// configured rate.
				retry := 1
				if limiter.rate > 0 {
					retry = int(1.0/limiter.rate) + 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retry))
				httpx.WriteError(w, r, http.StatusTooManyRequests,
					httpx.ProblemTypeRateLimit,
					"Tenant Rate Limit Exceeded",
					"err.rate_limit_tenant")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
