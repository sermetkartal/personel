// Package httpserver — chi middleware stack.
// Order: RequestID → Recover → Logger → Metrics → CORS → RateLimit → Auth → AuditContext
package httpserver

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/oklog/ulid/v2"
	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/config"
	"github.com/personel/api/internal/httpx"
)

// RequestIDFromContext is a local re-export of httpx.RequestIDFromContext so
// middleware callers in this package don't need to import httpx directly.
func RequestIDFromContext(ctx context.Context) string {
	return httpx.RequestIDFromContext(ctx)
}

// RequestIDMiddleware sets a ULID-based request ID on every request.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = ulid.Make().String()
		}
		ctx := httpx.WithRequestID(r.Context(), id)
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RecoverMiddleware catches panics and returns 500.
func RecoverMiddleware(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					log.Error("panic recovered",
						slog.String("request_id", RequestIDFromContext(r.Context())),
						slog.Any("panic", rec),
						slog.String("stack", string(debug.Stack())),
					)
					httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Server Error", "err.internal")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// LoggerMiddleware logs every request with structured fields.
func LoggerMiddleware(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			log.Info("http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", ww.Status()),
				slog.Int("bytes", ww.BytesWritten()),
				slog.Duration("duration", time.Since(start)),
				slog.String("request_id", RequestIDFromContext(r.Context())),
				slog.String("remote_addr", realIP(r)),
			)
		})
	}
}

// MetricsMiddleware records Prometheus metrics per endpoint.
func MetricsMiddleware(requests *prometheus.CounterVec, latency *prometheus.HistogramVec) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)

			route := chi_route(r)
			requests.With(prometheus.Labels{
				"method": r.Method,
				"route":  route,
				"status": fmt.Sprintf("%d", ww.Status()),
			}).Inc()
			latency.With(prometheus.Labels{
				"method": r.Method,
				"route":  route,
			}).Observe(time.Since(start).Seconds())
		})
	}
}

// chi_route extracts the matched route pattern from chi context.
func chi_route(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx == nil {
		return r.URL.Path
	}
	if rctx.RoutePattern() != "" {
		return rctx.RoutePattern()
	}
	return r.URL.Path
}

// TracingMiddleware starts an OTel span per request.
func TracingMiddleware(next http.Handler) http.Handler {
	tracer := otel.Tracer("personel-admin-api")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.Start(r.Context(), r.Method+" "+r.URL.Path,
			// Use semconv for span attributes
		)
		span.SetAttributes(
			semconv.HTTPMethodKey.String(r.Method),
			semconv.HTTPURLKey.String(r.URL.String()),
			attribute.String("request_id", RequestIDFromContext(ctx)),
		)
		defer span.End()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// CORSMiddleware allows origins listed in cfg.
func CORSMiddleware(cfg *config.HTTPConfig) func(http.Handler) http.Handler {
	origins := make(map[string]struct{}, len(cfg.CORSOrigins))
	for _, o := range cfg.CORSOrigins {
		origins[o] = struct{}{}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if _, ok := origins[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,PUT,DELETE,OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type,X-Request-Id,traceparent,tracestate,baggage")
				w.Header().Set("Vary", "Origin")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// tokenBucket is a simple per-IP token bucket.
type tokenBucket struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64
	burst   int
}

type bucket struct {
	tokens    float64
	lastFill  time.Time
}

func newTokenBucket(ratePerMin, burst int) *tokenBucket {
	return &tokenBucket{
		buckets: make(map[string]*bucket),
		rate:    float64(ratePerMin) / 60.0, // tokens per second
		burst:   burst,
	}
}

func (tb *tokenBucket) allow(key string) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	b, ok := tb.buckets[key]
	if !ok {
		tb.buckets[key] = &bucket{tokens: float64(tb.burst) - 1, lastFill: time.Now()}
		return true
	}
	now := time.Now()
	elapsed := now.Sub(b.lastFill).Seconds()
	b.tokens = min(float64(tb.burst), b.tokens+elapsed*tb.rate)
	b.lastFill = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// RateLimitMiddleware enforces per-IP rate limits.
func RateLimitMiddleware(ratePerMin, burst int) func(http.Handler) http.Handler {
	tb := newTokenBucket(ratePerMin, burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := realIP(r)
			if !tb.allow(ip) {
				w.Header().Set("Retry-After", "10")
				httpx.WriteError(w, r, http.StatusTooManyRequests, httpx.ProblemTypeRateLimit, "Rate Limit Exceeded", "err.rate_limit")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AuthMiddleware verifies the Bearer JWT and populates the principal in ctx.
func AuthMiddleware(verifier *auth.Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, err := verifier.VerifyRequest(r)
			if err != nil {
				httpx.WriteError(w, r, http.StatusUnauthorized, httpx.ProblemTypeAuth, "Authentication Required", "err.unauthenticated")
				return
			}
			ctx := auth.WithPrincipal(r.Context(), p)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AuditContextMiddleware injects the recorder into the request context.
// Must be placed AFTER AuthMiddleware.
func AuditContextMiddleware(rec *audit.Recorder) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := audit.WithRecorder(r.Context(), rec)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// realIP extracts the real client IP, checking X-Forwarded-For first.
func realIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		ip := strings.TrimSpace(parts[0])
		if net.ParseIP(ip) != nil {
			return ip
		}
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}
