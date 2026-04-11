// Package httpserver — chi router wiring for livrec-service.
//
// Routes:
//   POST   /v1/record/chunk                  — ingest (egress shim; internal bearer auth)
//   GET    /v1/record/{session_id}/stream    — SSE playback (OIDC auth; dual-control gate)
//   POST   /v1/record/{session_id}/export    — forensic export (OIDC auth; DPO-only)
//   GET    /healthz                          — liveness
//   GET    /readyz                           — readiness
//   GET    /metrics                          — Prometheus
package httpserver

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Handlers groups all route handlers. Wired by main.go.
type Handlers struct {
	ChunkUpload    http.Handler
	PlaybackStream http.Handler
	ForensicExport http.Handler
}

// Server wraps http.Server with chi routing.
type Server struct {
	httpServer *http.Server
}

// New constructs a Server with all routes wired.
func New(addr string, h Handlers, internalToken string, log *slog.Logger) *Server {
	r := chi.NewRouter()

	// Global middleware.
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(RequestLogger(log))

	// Health endpoints — no auth.
	r.Get("/healthz", healthzHandler)
	r.Get("/readyz", readyzHandler)
	r.Get("/metrics", promhttp.Handler().ServeHTTP)

	// Ingest — internal bearer token auth.
	r.Group(func(r chi.Router) {
		r.Use(IngestAuthMiddleware(internalToken))
		r.Post("/v1/record/chunk", h.ChunkUpload.ServeHTTP)
	})

	// Browser-facing — OIDC auth via trusted proxy headers.
	r.Group(func(r chi.Router) {
		r.Use(oidcAuthMiddlewareSimple)
		r.Get("/v1/record/{session_id}/stream", h.PlaybackStream.ServeHTTP)
		r.With(DPOOnlyMiddleware).Post("/v1/record/{session_id}/export", h.ForensicExport.ServeHTTP)
	})

	return &Server{
		httpServer: &http.Server{
			Addr:         addr,
			Handler:      r,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 0, // disabled for SSE (streaming responses)
			IdleTimeout:  60 * time.Second,
		},
	}
}

// Start begins listening. Blocks until the server is shut down.
func (s *Server) Start() error {
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func readyzHandler(w http.ResponseWriter, _ *http.Request) {
	// TODO(phase3): check MinIO + Vault reachability.
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready"))
}

// oidcAuthMiddlewareSimple is the package-level adaptor for the OIDC middleware
// that does not require a slog.Logger reference (avoids import cycle in this file).
// The real request logger is wired in main.go after the server is constructed.
func oidcAuthMiddlewareSimple(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
