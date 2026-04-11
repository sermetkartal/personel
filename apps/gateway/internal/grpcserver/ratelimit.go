package grpcserver

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/personel/gateway/internal/config"
	"github.com/personel/gateway/internal/observability"
)

// RateLimiter maintains per-endpoint and per-tenant token bucket limiters.
// It lazily creates limiters on first access and evicts them after inactivity
// via a simple GC sweep (acceptable for Phase 1 scale with ≤ 500 endpoints).
type RateLimiter struct {
	cfg     config.RateLimitConfig
	metrics *observability.Metrics

	mu        sync.Mutex
	endpoints map[string]*rate.Limiter // keyed by endpoint_id
	tenants   map[string]*rate.Limiter // keyed by tenant_id
}

// NewRateLimiter creates a RateLimiter from config.
func NewRateLimiter(cfg config.RateLimitConfig, metrics *observability.Metrics) *RateLimiter {
	return &RateLimiter{
		cfg:       cfg,
		metrics:   metrics,
		endpoints: make(map[string]*rate.Limiter),
		tenants:   make(map[string]*rate.Limiter),
	}
}

// AllowBatch checks whether the tenant and endpoint have sufficient token
// bucket capacity to accept a batch of n events. Returns false if either
// limiter is exhausted, and increments the relevant drop counter.
//
// This is called before each EventBatch is processed in stream.go.
func (r *RateLimiter) AllowBatch(ctx context.Context, tenantID, endpointID string, n int) bool {
	endpointLimiter := r.getOrCreateEndpoint(endpointID)
	tenantLimiter := r.getOrCreateTenant(tenantID)

	// Try endpoint limiter first.
	if !endpointLimiter.AllowN(time.Now(), n) {
		r.metrics.RateLimitDrops.WithLabelValues(tenantID, "endpoint").Inc()
		return false
	}
	// Then tenant-level.
	if !tenantLimiter.AllowN(time.Now(), n) {
		r.metrics.RateLimitDrops.WithLabelValues(tenantID, "tenant").Inc()
		return false
	}
	return true
}

func (r *RateLimiter) getOrCreateEndpoint(endpointID string) *rate.Limiter {
	r.mu.Lock()
	defer r.mu.Unlock()
	if l, ok := r.endpoints[endpointID]; ok {
		return l
	}
	l := rate.NewLimiter(
		rate.Limit(r.cfg.PerEndpointEventsPerSec),
		r.cfg.PerEndpointBurst,
	)
	r.endpoints[endpointID] = l
	return l
}

func (r *RateLimiter) getOrCreateTenant(tenantID string) *rate.Limiter {
	r.mu.Lock()
	defer r.mu.Unlock()
	if l, ok := r.tenants[tenantID]; ok {
		return l
	}
	l := rate.NewLimiter(
		rate.Limit(r.cfg.PerTenantEventsPerSec),
		r.cfg.PerTenantBurst,
	)
	r.tenants[tenantID] = l
	return l
}

// RemoveEndpoint removes the limiter for an endpoint when its stream closes.
// This prevents unbounded growth of the limiter maps.
func (r *RateLimiter) RemoveEndpoint(endpointID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.endpoints, endpointID)
}
