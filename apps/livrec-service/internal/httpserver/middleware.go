// Package httpserver — auth and audit middleware for livrec-service.
//
// Auth: validates the internal service-to-service bearer token for ingest
// paths, and a Keycloak-issued OIDC JWT for playback/export paths.
// Role enforcement (DPO-only for export) is done here at the middleware layer.
//
// Context keys: tenantID, userID are injected for downstream handlers.
package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type contextKey int

const (
	ctxKeyTenantID contextKey = iota
	ctxKeyUserID
	ctxKeyRole
)

// TenantIDFromContext retrieves the tenant ID injected by auth middleware.
func TenantIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyTenantID).(string)
	return v
}

// UserIDFromContext retrieves the user ID injected by auth middleware.
func UserIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyUserID).(string)
	return v
}

// RoleFromContext retrieves the user role injected by auth middleware.
func RoleFromContext(ctx context.Context) string {
	v, _ := ctx.Value(ctxKeyRole).(string)
	return v
}

// IngestAuthMiddleware validates the internal bearer token for chunk upload.
// The ingest path is called by the LiveKit egress shim, not by browsers.
func IngestAuthMiddleware(internalToken string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, "Bearer ") {
				WriteError(w, http.StatusUnauthorized, "bearer token required")
				return
			}
			token := strings.TrimPrefix(auth, "Bearer ")
			if token != internalToken {
				WriteError(w, http.StatusUnauthorized, "invalid token")
				return
			}

			// Extract tenant from X-Tenant-ID header (set by the egress shim).
			tenantID := r.Header.Get("X-Tenant-ID")
			if tenantID == "" {
				WriteError(w, http.StatusBadRequest, "X-Tenant-ID required")
				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyTenantID, tenantID)
			ctx = context.WithValue(ctx, ctxKeyUserID, "livrec-egress-shim")
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OIDCAuthMiddleware validates Keycloak-issued JWTs for browser-facing paths.
// STUB: in Phase 3 this will perform real JWT validation against the Keycloak
// JWKS endpoint. For now it extracts claims from trusted headers set by the
// Envoy/nginx reverse proxy (which performs the real OIDC validation upstream).
func OIDCAuthMiddleware(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// X-User-ID, X-Tenant-ID, X-User-Role are set by the trusted reverse
			// proxy after validating the Keycloak JWT. livrec trusts these headers
			// only on requests that arrive from the internal network.
			userID := r.Header.Get("X-User-ID")
			tenantID := r.Header.Get("X-Tenant-ID")
			role := r.Header.Get("X-User-Role")

			if userID == "" || tenantID == "" {
				WriteError(w, http.StatusUnauthorized, "missing identity headers")
				return
			}

			ctx := context.WithValue(r.Context(), ctxKeyUserID, userID)
			ctx = context.WithValue(ctx, ctxKeyTenantID, tenantID)
			ctx = context.WithValue(ctx, ctxKeyRole, role)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// DPOOnlyMiddleware rejects requests from non-DPO roles.
// Must be chained after OIDCAuthMiddleware.
func DPOOnlyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := RoleFromContext(r.Context())
		if role != "dpo" {
			WriteError(w, http.StatusForbidden, "DPO role required for this operation")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequestLogger logs each request with method, path, status, and latency.
func RequestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)
			log.Info("http",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rw.status),
				slog.Duration("latency", time.Since(start)),
			)
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}
