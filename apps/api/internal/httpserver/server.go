// Package httpserver wires the chi router, all middleware, all route groups,
// and the HTTP server lifecycle.
package httpserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/personel/api/internal/accessreview"
	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/backup"
	"github.com/personel/api/internal/bcp"
	"github.com/personel/api/internal/config"
	"github.com/personel/api/internal/destruction"
	"github.com/personel/api/internal/dlpstate"
	"github.com/personel/api/internal/dsr"
	"github.com/personel/api/internal/endpoint"
	"github.com/personel/api/internal/evidence"
	"github.com/personel/api/internal/incident"
	"github.com/personel/api/internal/legalhold"
	"github.com/personel/api/internal/liveview"
	"github.com/personel/api/internal/mobile"
	"github.com/personel/api/internal/policy"
	"github.com/personel/api/internal/reports"
	"github.com/personel/api/internal/screenshots"
	"github.com/personel/api/internal/silence"
	"github.com/personel/api/internal/tenant"
	"github.com/personel/api/internal/transparency"
	"github.com/personel/api/internal/user"
)

// Services is a dependency bag injected at server construction.
type Services struct {
	Cfg          *config.Config
	Verifier     *auth.Verifier
	Recorder     *audit.Recorder
	Tenant       *tenant.Service
	User         *user.Service
	Endpoint     *endpoint.Service
	Policy       *policy.Service
	DSR          *dsr.Service
	LegalHold    *legalhold.Service
	Destruction  *destruction.Service
	LiveView     *liveview.Service
	Reports      *reports.Service
	Screenshots  *screenshots.Service
	Transparency *transparency.Service
	Silence      *silence.Service
	DLPState     *dlpstate.Service
	Mobile       *mobile.Service
	Evidence      *evidence.Store
	EvidencePack  *evidence.PackBuilder
	Backup        *backup.Service
	AccessReview  *accessreview.Service
	Incident      *incident.Service
	BCP           *bcp.Service
	DBPool        *pgxpool.Pool
	Log           *slog.Logger
}

// Metrics holds the Prometheus collectors registered at server construction.
type Metrics struct {
	Requests *prometheus.CounterVec
	Latency  *prometheus.HistogramVec
}

// NewMetrics registers and returns Prometheus metrics.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		Requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "personel_api_requests_total",
			Help: "Total HTTP requests by method, route, and status.",
		}, []string{"method", "route", "status"}),
		Latency: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "personel_api_request_duration_seconds",
			Help:    "HTTP request latency.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5},
		}, []string{"method", "route"}),
	}
	reg.MustRegister(m.Requests, m.Latency)
	return m
}

// BuildRouter constructs the fully-wired chi router.
func BuildRouter(svc *Services, met *Metrics) http.Handler {
	r := chi.NewRouter()

	// Global middleware (order matters).
	r.Use(RequestIDMiddleware)
	r.Use(RecoverMiddleware(svc.Log))
	r.Use(middleware.RealIP)
	r.Use(LoggerMiddleware(svc.Log))
	r.Use(TracingMiddleware)
	r.Use(MetricsMiddleware(met.Requests, met.Latency))
	r.Use(CORSMiddleware(&svc.Cfg.HTTP))
	r.Use(RateLimitMiddleware(svc.Cfg.RateLimit.RequestsPerMinute, svc.Cfg.RateLimit.BurstSize))

	// Health and metrics — no auth required.
	r.Get("/healthz", healthzHandler)
	r.Get("/readyz", readyzHandler)
	r.Handle(svc.Cfg.Observ.MetricsPath, promhttp.Handler())

	// API v1 — all routes require auth.
	r.Route("/v1", func(r chi.Router) {
		r.Use(AuthMiddleware(svc.Verifier))
		r.Use(AuditContextMiddleware(svc.Recorder))

		// --- Tenants ---
		r.Route("/tenants", func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdmin))
			r.Get("/", tenant.ListHandler(svc.Tenant))
			r.Post("/", tenant.CreateHandler(svc.Tenant))
			r.Route("/{tenantID}", func(r chi.Router) {
				r.Get("/", tenant.GetHandler(svc.Tenant))
				r.Patch("/", tenant.UpdateHandler(svc.Tenant))
			})
		})

		// --- Users ---
		r.Route("/users", func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleDPO))
			r.Get("/", user.ListHandler(svc.User))
			r.Post("/", user.CreateHandler(svc.User))
			r.Route("/{userID}", func(r chi.Router) {
				r.Get("/", user.GetHandler(svc.User))
				r.Patch("/", user.UpdateHandler(svc.User))
				r.Delete("/", user.DeleteHandler(svc.User))
				r.Patch("/role", user.ChangeRoleHandler(svc.User))
				r.Post("/disable", user.DisableHandler(svc.User))
			})
		})

		// --- Employee monitoring overview (Phase 2 preview) ---
		// GET /v1/employees/overview — one row per employee with today's
		// active/idle minutes, top apps, productivity score. Consumed
		// by /tr/employees console page.
		if svc.DBPool != nil {
			r.Route("/employees", func(r chi.Router) {
				r.Use(auth.RequireRole(
					auth.RoleAdmin, auth.RoleManager, auth.RoleHR,
					auth.RoleDPO, auth.RoleInvestigator, auth.RoleAuditor,
					auth.RoleITManager, auth.RoleITOperator,
				))
				r.Get("/overview", user.EmployeesOverviewHandler(svc.DBPool))
				r.Get("/{userID}/detail", user.EmployeeDetailHandler(svc.DBPool))
			})
		}

		// --- Endpoints (agent fleet) ---
		r.Route("/endpoints", func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleDPO))
			r.Get("/", endpoint.ListHandler(svc.Endpoint))
			r.Post("/enroll", endpoint.EnrollHandler(svc.Endpoint))
			r.Route("/{endpointID}", func(r chi.Router) {
				r.Get("/", endpoint.GetHandler(svc.Endpoint))
				r.Delete("/", endpoint.DeleteHandler(svc.Endpoint))
				r.Post("/revoke", endpoint.RevokeHandler(svc.Endpoint))
			})
		})

		// --- Policies ---
		r.Route("/policies", func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdmin))
			r.Get("/", policy.ListHandler(svc.Policy))
			r.Post("/", policy.CreateHandler(svc.Policy))
			r.Route("/{policyID}", func(r chi.Router) {
				r.Get("/", policy.GetHandler(svc.Policy))
				r.Patch("/", policy.UpdateHandler(svc.Policy))
				r.Delete("/", policy.DeleteHandler(svc.Policy))
				r.Post("/push", policy.PushHandler(svc.Policy))
			})
		})

		// --- DSR (KVKK m.11) ---
		r.Route("/dsr", func(r chi.Router) {
			r.Post("/", dsr.SubmitHandler(svc.DSR))       // Employee or DPO
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireRole(auth.RoleDPO))
				r.Get("/", dsr.ListHandler(svc.DSR))
				r.Route("/{dsrID}", func(r chi.Router) {
					r.Get("/", dsr.GetHandler(svc.DSR))
					r.Post("/assign", dsr.AssignHandler(svc.DSR))
					r.Post("/respond", dsr.RespondHandler(svc.DSR))
					r.Post("/extend", dsr.ExtendHandler(svc.DSR))
					r.Post("/reject", dsr.RejectHandler(svc.DSR))
				})
			})
		})

		// --- Legal Hold (DPO only) ---
		r.Route("/legal-holds", func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleDPO))
			r.Post("/", legalhold.PlaceHandler(svc.LegalHold))
			r.Get("/", legalhold.ListHandler(svc.LegalHold))
			r.Route("/{holdID}", func(r chi.Router) {
				r.Get("/", legalhold.GetHandler(svc.LegalHold))
				r.Post("/release", legalhold.ReleaseHandler(svc.LegalHold))
			})
		})

		// --- Evidence Packs (DPO-only SOC 2 Type II export) ---
		// Phase 3.0 — ADR 0023. Streams a signed ZIP containing the
		// manifest + per-item JSON + per-item Ed25519 signature. The
		// canonical bytes are pulled from audit-worm independently.
		if svc.EvidencePack != nil {
			r.Route("/dpo/evidence-packs", func(r chi.Router) {
				r.Use(auth.RequireRole(auth.RoleDPO))
				r.Get("/", evidence.GetPackHandler(svc.EvidencePack))
			})
		}

		// --- Destruction Reports (DPO-only download) ---
		r.Route("/destruction-reports", func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleDPO, auth.RoleAdmin))
			r.Get("/", destruction.ListHandler(svc.Destruction))
			r.Route("/{reportID}", func(r chi.Router) {
				r.Get("/", destruction.GetHandler(svc.Destruction))
				r.Get("/download", destruction.DownloadHandler(svc.Destruction))
			})
			// Manual generation trigger (auto-runs on 1 Jan and 1 Jul)
			r.Post("/generate", destruction.GenerateHandler(svc.Destruction))
		})

		// --- Live View ---
		// Authority model: IT Operator requests → IT Manager (or Admin)
		// approves. HR has NO live-view authority. DPO has a compliance
		// override for termination only (KVKK scope violations).
		r.Route("/live-view", func(r chi.Router) {
			r.Route("/requests", func(r chi.Router) {
				// Request: IT Operator / IT Manager / Admin
				r.Post("/", liveview.RequestHandler(svc.LiveView))
				// List pending: IT Manager / Admin (to approve)
				r.Get("/", liveview.ListRequestsHandler(svc.LiveView))
				r.Route("/{requestID}", func(r chi.Router) {
					r.Get("/", liveview.GetRequestHandler(svc.LiveView))
					r.Post("/approve", liveview.ApproveHandler(svc.LiveView))
					r.Post("/reject", liveview.RejectHandler(svc.LiveView))
				})
			})
			r.Route("/sessions", func(r chi.Router) {
				r.Get("/", liveview.ListSessionsHandler(svc.LiveView))
				r.Route("/{sessionID}", func(r chi.Router) {
					r.Get("/", liveview.GetSessionHandler(svc.LiveView))
					r.Post("/end", liveview.EndSessionHandler(svc.LiveView))
					r.Post("/terminate", liveview.TerminateHandler(svc.LiveView))
				})
			})
		})

		// --- Reports (ClickHouse) ---
		r.Route("/reports", func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleManager, auth.RoleDPO, auth.RoleAuditor))
			r.Get("/productivity", reports.ProductivityHandler(svc.Reports))
			r.Get("/top-apps", reports.TopAppsHandler(svc.Reports))
			r.Get("/idle-active", reports.IdleActiveHandler(svc.Reports))
			r.Get("/endpoint-activity", reports.EndpointActivityHandler(svc.Reports))
			r.Get("/app-blocks", reports.AppBlocksHandler(svc.Reports))
		})

		// --- Screenshots (Investigator / DPO only) ---
		r.Route("/screenshots", func(r chi.Router) {
			r.Use(auth.RequireCan(auth.OpRead, auth.ResourceScreenshot))
			r.Get("/", screenshots.ListHandler(svc.Screenshots))
			r.Route("/{screenshotID}", func(r chi.Router) {
				r.Get("/url", screenshots.PresignedURLHandler(svc.Screenshots))
			})
		})

		// --- Silence / agent heartbeat gaps ---
		r.Route("/silence", func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleManager, auth.RoleDPO))
			r.Get("/", silence.ListHandler(svc.Silence))
			r.Route("/{endpointID}", func(r chi.Router) {
				r.Get("/timeline", silence.TimelineHandler(svc.Silence))
				r.Post("/acknowledge", silence.AcknowledgeHandler(svc.Silence))
			})
		})

		// --- Employee Transparency Portal backend ---
		r.Route("/me", func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleEmployee, auth.RoleAdmin, auth.RoleDPO))
			r.Get("/", transparency.MyDataHandler(svc.Transparency))
			r.Get("/live-view-history", transparency.MyLiveViewHistoryHandler(svc.Transparency))
			r.Post("/dsr", transparency.SubmitDSRHandler(svc.DSR))
			r.Get("/dsr", transparency.MyDSRHandler(svc.DSR))
			// GET /v1/me/dsr/{id} — employee DSR detail (scoped to own DSRs)
			r.Get("/dsr/{id}", transparency.MyDSRDetailHandler(svc.DSR))
			// POST /v1/me/acknowledge-notification — KVKK m.10 first-login acknowledgement
			r.Post("/acknowledge-notification", transparency.AcknowledgeNotificationHandler(svc.Transparency))
		})

		// --- System endpoints ---
		r.Route("/system", func(r chi.Router) {
			// GET /v1/system/dlp-state — readable by all roles (portal banner, console badge)
			r.With(auth.RequireRole(
				auth.RoleAdmin, auth.RoleManager, auth.RoleHR, auth.RoleDPO,
				auth.RoleInvestigator, auth.RoleAuditor, auth.RoleEmployee, auth.RoleDLPAdmin,
			)).Get("/dlp-state", dlpstate.GetDLPStateHandler(svc.DLPState))

			// POST /v1/system/dlp-bootstrap-keys — dlp-admin only (invoked by dlp-enable.sh)
			r.With(auth.RequireRole(auth.RoleDLPAdmin)).
				Post("/dlp-bootstrap-keys", dlpstate.BootstrapPEDEKsHandler(svc.DLPState))

			// POST /v1/system/backup-runs — admin-only ingest path for
			// the out-of-API backup runner. Records an A1.2 evidence
			// item (KindBackupRun) from the submitted run report.
			if svc.Backup != nil {
				r.With(auth.RequireRole(auth.RoleAdmin)).
					Post("/backup-runs", backup.RecordRunHandler(svc.Backup))
			}

			// POST /v1/system/access-reviews — DPO/Admin submits the
			// outcome of a quarterly/semi-annual access review. CC6.3.
			if svc.AccessReview != nil {
				r.With(auth.RequireRole(auth.RoleDPO, auth.RoleAdmin)).
					Post("/access-reviews", accessreview.RecordReviewHandler(svc.AccessReview))
			}

			// POST /v1/system/incident-closures — DPO/Admin submits a
			// closed incident with post-incident review. CC7.3.
			if svc.Incident != nil {
				r.With(auth.RequireRole(auth.RoleDPO, auth.RoleAdmin)).
					Post("/incident-closures", incident.RecordClosureHandler(svc.Incident))
			}

			// POST /v1/system/bcp-drills — Admin submits the result of
			// a BCP / DR live drill or tabletop exercise. CC9.1.
			if svc.BCP != nil {
				r.With(auth.RequireRole(auth.RoleAdmin)).
					Post("/bcp-drills", bcp.RecordDrillHandler(svc.BCP))
			}

			// GET /v1/system/evidence-coverage?period=YYYY-MM — SOC 2
			// Type II coverage matrix for the current tenant. Lists the
			// item count for every expected TSC control plus an explicit
			// GapControls array of zero-item controls. DPO / Auditor only.
			// Phase 3.0 — ADR 0023.
			if svc.Evidence != nil {
				r.With(auth.RequireRole(auth.RoleDPO, auth.RoleAuditor)).
					Get("/evidence-coverage", evidence.GetCoverageHandler(svc.Evidence))
			}
		})

		// --- Mobile BFF endpoints (Phase 2.9) ---
		// Consumed exclusively by apps/mobile-admin (React Native). Gated
		// on roles that make sense in an on-call admin context.
		r.Route("/mobile", func(r chi.Router) {
			r.Use(auth.RequireRole(
				auth.RoleAdmin, auth.RoleManager, auth.RoleHR, auth.RoleDPO,
				auth.RoleInvestigator, auth.RoleAuditor,
			))
			r.Get("/summary", mobile.GetSummaryHandler(svc.Mobile))
			r.Post("/push-tokens", mobile.RegisterPushTokenHandler(svc.Mobile))
			r.Get("/live-view/pending", mobile.ListPendingLiveViewHandler(svc.Mobile))
			r.Get("/dsr/queue", mobile.ListDSRQueueHandler(svc.Mobile))
			r.Get("/silence", mobile.ListSilenceHandler(svc.Mobile))

			// POST /v1/system/dlp-transition — atomic state transition; writes
			// audit entry, updates dlp_state, derives banner state (ADR 0013).
			// Called by dlp-enable.sh and dlp-disable.sh after out-of-API side
			// effects (Vault Secret ID, container start/stop) have completed.
			r.With(auth.RequireRole(auth.RoleDLPAdmin)).
				Post("/dlp-transition", dlpstate.TransitionHandler(svc.DLPState))

			// GET /v1/system/module-state — forward-compat generalization
			// of dlp-state. Phase 1 returns DLP real state + Phase 2 module
			// placeholders (OCR, ML, live view recording, HRIS). Portal and
			// console should prefer this endpoint over /dlp-state for new
			// code. Readable by all authenticated roles.
			r.With(auth.RequireRole(
				auth.RoleAdmin, auth.RoleManager, auth.RoleHR, auth.RoleDPO,
				auth.RoleInvestigator, auth.RoleAuditor, auth.RoleEmployee, auth.RoleDLPAdmin,
			)).Get("/module-state", dlpstate.GetModuleStateHandler(svc.DLPState))
		})
	})

	return r
}

// Server wraps net/http.Server with graceful shutdown.
type Server struct {
	srv *http.Server
	log *slog.Logger
}

// New creates the HTTP server.
func New(cfg *config.HTTPConfig, handler http.Handler, log *slog.Logger) *Server {
	return &Server{
		srv: &http.Server{
			Addr:         cfg.Addr,
			Handler:      handler,
			ReadTimeout:  cfg.ReadTimeout,
			WriteTimeout: cfg.WriteTimeout,
			IdleTimeout:  cfg.IdleTimeout,
		},
		log: log,
	}
}

// Start listens and serves. Blocks until the server stops.
func (s *Server) Start() error {
	s.log.Info("http server starting", slog.String("addr", s.srv.Addr))
	if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http server: %w", err)
	}
	return nil
}

// Shutdown gracefully drains connections.
func (s *Server) Shutdown(ctx context.Context) error {
	s.log.Info("http server shutting down")
	return s.srv.Shutdown(ctx)
}

func healthzHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func readyzHandler(w http.ResponseWriter, _ *http.Request) {
	// TODO: check DB ping, NATS ping
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ready"}`))
}
