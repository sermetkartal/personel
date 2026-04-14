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
	"github.com/personel/api/internal/apikey"
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
	"github.com/personel/api/internal/featureflags"
	"github.com/personel/api/internal/incident"
	"github.com/personel/api/internal/integrations"
	"github.com/personel/api/internal/kvkk"
	"github.com/personel/api/internal/legalhold"
	"github.com/personel/api/internal/liveview"
	"github.com/personel/api/internal/mobile"
	"github.com/personel/api/internal/pipeline"
	"github.com/personel/api/internal/policy"
	"github.com/personel/api/internal/reports"
	"github.com/personel/api/internal/reportspg"
	"github.com/personel/api/internal/screenshots"
	"github.com/personel/api/internal/search"
	"github.com/personel/api/internal/settings"
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
	// EndpointCmd handles remote command issuance + ack (Faz 6 #64 #65).
	EndpointCmd  *endpoint.CommandService
	Policy       *policy.Service
	DSR          *dsr.Service
	// DSRFulfillment runs KVKK m.11/b access exports and m.11/f
	// crypto-erase (Faz 6 #69). Optional — if nil the /fulfill-*
	// routes are not mounted.
	DSRFulfillment *dsr.FulfillmentService
	// APIKey provides the service-to-service credential layer
	// (Faz 6 #72). Optional — if nil the /apikeys routes are not
	// mounted and the ApiKey middleware is unavailable.
	APIKey       *apikey.Service
	LegalHold    *legalhold.Service
	Destruction  *destruction.Service
	LiveView     *liveview.Service
	Reports      *reports.Service
	ReportsCH    *reports.CHHandlers
	Trends       *reports.TrendsHandler
	Search       *search.Service
	Screenshots  *screenshots.Service
	Transparency *transparency.Service
	Silence      *silence.Service
	DLPState     *dlpstate.Service
	Mobile       *mobile.Service
	Evidence      *evidence.Store
	EvidencePack  *evidence.PackBuilder
	Backup        *backup.Service
	// BackupTargets is the Wave 9 Sprint 3A settings CRUD surface for
	// per-tenant backup destinations + run history. Nil-safe: when
	// nil the /v1/settings/backup/* routes are not mounted.
	BackupTargets *backup.TargetService
	// Integrations is the Wave 9 Sprint 3A third-party credential
	// vault (MaxMind, Cloudflare, PagerDuty, Slack, Sentry). Nil-safe:
	// when nil the /v1/settings/integrations routes are not mounted.
	Integrations *integrations.Service
	// Settings is the Wave 9 Sprint 3A tenant CA mode + retention
	// policy surface. Nil-safe: when nil the /v1/settings/ca-mode
	// and /v1/settings/retention routes are not mounted.
	Settings *settings.Service
	AccessReview  *accessreview.Service
	Incident      *incident.Service
	BCP           *bcp.Service
	// KVKK is the Wave 9 Sprint 2A backend for the console KVKK
	// compliance section (VERBİS, aydınlatma, DPA, DPIA, açık rıza).
	// Nil-safe: when nil the /v1/kvkk/* routes are not mounted.
	KVKK          *kvkk.Service
	Pipeline      *pipeline.Service
	// FeatureFlags is the Faz 16 #173 feature flag evaluator + admin
	// surface. Nil-safe: when nil the /v1/system/feature-flags routes
	// are not mounted.
	FeatureFlags  *featureflags.Service
	DBPool        *pgxpool.Pool
	// AuditBroker fans audit entries to live WebSocket subscribers of
	// /v1/audit/stream (Faz 6 #66). Nil-safe: when nil the route is
	// mounted but returns 503 so the console sees a coherent degraded
	// mode instead of a 404. main.go wires the same broker into
	// Recorder.SetBroker so every successful append is fanned out.
	AuditBroker   *audit.Broker
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

	// Per-tenant rate limit (Faz 6 #71). Constructed once so the bucket
	// map persists for the lifetime of the router; mounted further down
	// inside the /v1 auth group because the principal is required.
	// Zero rate disables the layer — the limiter itself short-circuits
	// Allow in that case, so mounting is unconditional.
	tenantLimiter := NewTenantLimiter(
		svc.Cfg.RateLimit.TenantRequestsPerMinute,
		svc.Cfg.RateLimit.TenantBurst,
		svc.Log,
	)

	// Health and metrics — no auth required.
	r.Get("/healthz", healthzHandler)
	r.Get("/readyz", readyzHandler)
	r.Handle(svc.Cfg.Observ.MetricsPath, promhttp.Handler())

	// --- Public agent enrollment endpoint ---
	// POST /v1/agent-enroll is unauthenticated by design — the agent
	// has no Keycloak identity yet, only the single-use AppRole
	// credential bundled in the opaque enrollment token. The handler
	// itself enforces auth via three independent gates:
	// (1) enrollment_tokens row exists, unused, unexpired;
	// (2) Vault AppRole login succeeds with the presented role/secret;
	// (3) the presented CSR is cryptographically valid.
	// Mounted at the top level (NOT inside the /v1 auth group) so the
	// AuthMiddleware never sees it.
	r.Post("/v1/agent-enroll", endpoint.AgentEnrollHandler(svc.Endpoint))

	// --- Internal gateway → API callback (Faz 6 #64) ---
	// POST /v1/internal/commands/{commandID}/ack is called by the
	// in-cluster gateway after the agent reports acknowledgement or
	// final state for a remote command. No Keycloak JWT — the
	// InternalTokenMiddleware enforces a shared-secret header.
	if svc.EndpointCmd != nil {
		r.Route("/v1/internal", func(r chi.Router) {
			r.Use(InternalTokenMiddleware(svc.Cfg.Server.InternalToken))
			r.Post("/commands/{commandID}/ack", endpoint.InternalAckHandler(svc.EndpointCmd))
		})
	}

	// API v1 — all routes require auth.
	r.Route("/v1", func(r chi.Router) {
		r.Use(AuthMiddleware(svc.Verifier))
		// Per-tenant rate limit runs BEFORE AuditContextMiddleware so a
		// rogue tenant cannot flood the audit chain with 429 noise — the
		// reject path is served by httpx.WriteError which does not touch
		// the recorder. Mount order: Auth → TenantRateLimit → Audit.
		r.Use(TenantRateLimitMiddleware(tenantLimiter))
		r.Use(AuditContextMiddleware(svc.Recorder))

		// --- Audit log streaming (Faz 6 #66) ---
		// WebSocket at /v1/audit/stream. RBAC is enforced inside the
		// handler (admin, dpo, investigator, auditor). The handler also
		// records the subscription + disconnection in the audit log so
		// an operator cannot silently observe every tenant's events.
		// When AuditBroker is nil the handler returns 503.
		r.Get("/audit/stream", audit.StreamHandler(svc.AuditBroker))

		// --- Tenants ---
		r.Route("/tenants", func(r chi.Router) {
			// Tenant-scoped preference: screenshot capture preset.
			// GET is open to any authenticated user in the tenant
			// (read-only preference). PATCH is locked to admin +
			// it_manager via inline `.With(...)` override.
			if svc.Tenant != nil {
				r.Get("/me/screenshot-preset",
					tenant.GetScreenshotPresetHandler(svc.Tenant))
				r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleITManager)).
					Patch("/me/screenshot-preset",
						tenant.PatchScreenshotPresetHandler(svc.Tenant))
			}

			// Admin-only CRUD on the tenant collection itself.
			r.Group(func(r chi.Router) {
				r.Use(auth.RequireRole(auth.RoleAdmin))
				r.Get("/", tenant.ListHandler(svc.Tenant))
				r.Post("/", tenant.CreateHandler(svc.Tenant))
				r.Route("/{tenantID}", func(r chi.Router) {
					r.Get("/", tenant.GetHandler(svc.Tenant))
					r.Patch("/", tenant.UpdateHandler(svc.Tenant))
				})
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

			// POST /bulk — Faz 6 #65. Batch wipe / deactivate / revoke
			// across up to endpoint.BulkLimit endpoints. Admin +
			// IT Manager only (Investigator still uses per-endpoint
			// wipe for KVKK m.7 incident response).
			if svc.EndpointCmd != nil {
				r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleITManager)).
					Post("/bulk", endpoint.BulkHandler(svc.EndpointCmd))
			}

			r.Route("/{endpointID}", func(r chi.Router) {
				r.Get("/", endpoint.GetHandler(svc.Endpoint))
				r.Delete("/", endpoint.DeleteHandler(svc.Endpoint))
				r.Post("/revoke", endpoint.RevokeHandler(svc.Endpoint))
				// POST /refresh-token (Faz 6 #63) — widen RBAC to IT
				// hierarchy. The parent /endpoints group is gated to
				// admin+dpo; we override with With() here because
				// DPO has no cert-rotation authority (device lifecycle
				// is IT's purview) but it_manager/it_operator do.
				r.With(auth.RequireRole(
					auth.RoleAdmin, auth.RoleITManager, auth.RoleITOperator,
				)).Post("/refresh-token", endpoint.RefreshTokenHandler(svc.Endpoint))

				// --- Remote commands (Faz 6 #64) ---
				// wipe/deactivate/commands-history. Guarded on the
				// CommandService being wired so the package builds in
				// test fixtures that only stub the enrollment service.
				if svc.EndpointCmd != nil {
					// Wipe: crypto-erase. Admin + IT Manager +
					// Investigator (incident response KVKK m.7).
					r.With(auth.RequireRole(
						auth.RoleAdmin, auth.RoleITManager, auth.RoleInvestigator,
					)).Post("/wipe", endpoint.WipeHandler(svc.EndpointCmd))

					// Deactivate: reversible stop. Admin + IT Manager.
					r.With(auth.RequireRole(
						auth.RoleAdmin, auth.RoleITManager,
					)).Post("/deactivate", endpoint.DeactivateHandler(svc.EndpointCmd))

					// GET /commands — read-only command history for
					// the console endpoint detail page. Admin +
					// IT Manager + IT Operator.
					r.With(auth.RequireRole(
						auth.RoleAdmin, auth.RoleITManager, auth.RoleITOperator,
					)).Get("/commands", endpoint.ListCommandsHandler(svc.EndpointCmd))
				}
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
				// GET /preview — dry-run validation before sign-and-push.
				// Readable by Manager, DPO, Auditor in addition to Admin
				// (parent route uses RequireRole(Admin); we widen with With).
				r.With(auth.RequireRole(
					auth.RoleAdmin, auth.RoleManager, auth.RoleDPO, auth.RoleAuditor,
				)).Get("/preview", policy.PreviewHandler(svc.Policy))
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

					// --- Faz 6 #69: DSR fulfillment workflow ---
					// POST /fulfill-access — KVKK m.11/b. DPO + Admin.
					// POST /fulfill-erasure — KVKK m.11/f. DPO ONLY
					// (even Admin is locked out; the DPO is personally
					// accountable for the unrecoverable decision per
					// KVKK art. 7). Route is no-op when fulfillment
					// service is nil (Phase 1 dev setups).
					if svc.DSRFulfillment != nil {
						r.With(auth.RequireRole(auth.RoleDPO, auth.RoleAdmin)).
							Post("/fulfill-access", dsr.FulfillAccessHandler(svc.DSRFulfillment))
						r.With(auth.RequireRole(auth.RoleDPO)).
							Post("/fulfill-erasure", dsr.FulfillErasureHandler(svc.DSRFulfillment))
					}
				})
			})
		})

		// --- API Keys (Faz 6 #72) ---
		// Service-to-service credential issuance. DPO + Admin can
		// generate, list, and revoke. The plaintext key is returned
		// ONCE on creation and never again — losing it requires
		// revoke + re-create. Non-interactive callers present the
		// key via `Authorization: ApiKey <plaintext>` against routes
		// that mount apikey.Middleware in place of AuthMiddleware.
		if svc.APIKey != nil {
			r.Route("/apikeys", func(r chi.Router) {
				r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleDPO))
				r.Post("/", apikey.CreateHandler(svc.APIKey))
				r.Get("/", apikey.ListHandler(svc.APIKey))
				r.Delete("/{keyID}", apikey.RevokeHandler(svc.APIKey))
			})
		}

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

			// --- Roadmap item #87 — trend analysis (week-over-week + z-score).
			// RBAC inherits the /reports parent gate. Handler is nil-safe
			// (svc.Trends may be nil if the trend service wasn't wired).
			if svc.Trends != nil {
				r.Get("/trends", svc.Trends.Get)
			}

			// --- Roadmap item #68 — real CH aggregation queries
			// Parallel /ch/ subpath. Uses the same RBAC as the parent
			// /reports group PLUS HR + Investigator (they have legitimate
			// access to cross-user aggregates for KVKK DSR fulfilment and
			// investigation workflows). 503 when the CH client is nil.
			r.Route("/ch", func(r chi.Router) {
				r.Use(auth.RequireRole(
					auth.RoleAdmin, auth.RoleManager, auth.RoleHR,
					auth.RoleDPO, auth.RoleInvestigator,
				))
				if svc.ReportsCH != nil {
					r.Get("/top-apps", svc.ReportsCH.CHTopAppsHandler())
					r.Get("/idle-active", svc.ReportsCH.CHIdleActiveHandler())
					r.Get("/productivity", svc.ReportsCH.CHProductivityHandler())
					r.Get("/app-blocks", svc.ReportsCH.CHAppBlocksHandler())
				} else {
					// Mount empty 503 responders so the console sees a
					// coherent degraded mode rather than a 404.
					nilH := reports.NewCHHandlers(nil)
					r.Get("/top-apps", nilH.CHTopAppsHandler())
					r.Get("/idle-active", nilH.CHIdleActiveHandler())
					r.Get("/productivity", nilH.CHProductivityHandler())
					r.Get("/app-blocks", nilH.CHAppBlocksHandler())
				}

				// --- Roadmap items #85, #86 — scoring endpoints.
				// productivity-breakdown inherits the /ch RBAC set above
				// (admin, manager, hr, dpo, investigator).
				// risk-score is tighter: admin + dpo + investigator only,
				// since a risk-score leak on a per-user basis is materially
				// more sensitive than productivity analytics. The .With()
				// call composes with the outer .Use().
				if svc.DBPool != nil {
					r.Get("/productivity-breakdown",
						reports.ProductivityBreakdownHandler(svc.DBPool))
					r.With(auth.RequireRole(
						auth.RoleAdmin, auth.RoleDPO, auth.RoleInvestigator,
					)).Get("/risk-score", reports.RiskScoreHandler(svc.DBPool))
				}
			})
		})

		// --- Search (Faz 6 #67 — OpenSearch full-text) ---
		// /v1/search/audit — audit log full-text search. Gated to the
		// roles that have a legitimate need to investigate admin actions:
		// Admin (system owner), DPO (KVKK m.11 compliance), Investigator
		// (incident response), Auditor (read-only compliance).
		//
		// /v1/search/events — ClickHouse-mirrored fleet telemetry search.
		// Widens the RBAC to Manager + HR because they need to be able to
		// search employee application/file activity for performance
		// reviews and HR investigations. Keystroke content is redacted
		// server-side regardless of role (ADR 0013 invariant).
		if svc.Search != nil {
			r.Route("/search", func(r chi.Router) {
				r.With(auth.RequireRole(
					auth.RoleAdmin, auth.RoleDPO,
					auth.RoleInvestigator, auth.RoleAuditor,
				)).Get("/audit", search.AuditHandler(svc.Search))

				r.With(auth.RequireRole(
					auth.RoleAdmin, auth.RoleDPO,
					auth.RoleInvestigator, auth.RoleAuditor,
					auth.RoleManager, auth.RoleHR,
				)).Get("/events", search.EventsHandler(svc.Search))
			})
		}

		// --- Reports preview (Postgres-backed, Phase 1 dev/demo) ---
		// Reads from employee_daily_stats + employee_hourly_stats so the
		// console reports pages can render real numbers before the
		// ClickHouse event pipeline is live. Swap to /v1/reports/* in
		// Phase 2 when the roll-up job writes to ClickHouse.
		// DBPool is always non-nil here: cmd/api/main.go calls os.Exit(1)
		// if pool init fails, so a nil guard would silently skip route mount
		// and return 404 to the console — a misleading "no data" state.
		r.Route("/reports-preview", func(r chi.Router) {
			// KVKK m.5/m.6 proportionality: productivity/idle-active are personnel
			// performance metrics. IT operators have no legitimate need (their role is
			// device support, not HR analytics) and must not see them. HR explicitly
			// allowed; DPO/Auditor allowed for compliance oversight.
			r.Use(auth.RequireRole(
				auth.RoleAdmin, auth.RoleManager, auth.RoleHR,
				auth.RoleDPO, auth.RoleAuditor, auth.RoleITManager,
			))
			r.Get("/productivity", reportspg.ProductivityHandler(svc.DBPool))
			r.Get("/top-apps", reportspg.TopAppsHandler(svc.DBPool))
			r.Get("/idle-active", reportspg.IdleActiveHandler(svc.DBPool))
			r.Get("/app-blocks", reportspg.AppBlocksHandler(svc.DBPool))
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

			// Feature flags (Faz 16 #173). Admin + DPO only. Every flip
			// writes an audit entry; unknown keys evaluate to the
			// caller-supplied default. Gated on non-nil service so
			// environments without Postgres still mount cleanly.
			if svc.FeatureFlags != nil {
				r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleDPO)).
					Get("/feature-flags", featureflags.ListHandler(svc.FeatureFlags))
				r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleDPO)).
					Get("/feature-flags/{key}", featureflags.GetHandler(svc.FeatureFlags))
				r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleDPO)).
					Put("/feature-flags/{key}", featureflags.SetHandler(svc.FeatureFlags))
				r.With(auth.RequireRole(auth.RoleAdmin, auth.RoleDPO)).
					Delete("/feature-flags/{key}", featureflags.DeleteHandler(svc.FeatureFlags))
			}
		})

		// --- KVKK compliance (Wave 9 Sprint 2A) ---
		// VERBİS / aydınlatma metni / DPA / DPIA / açık rıza. Every
		// GET readable by admin+dpo+auditor; every mutation gated to
		// admin+dpo only (auditors are read-only). Document uploads
		// are multipart PDFs for DPA/DPIA and base64 JSON for per-user
		// consent, all with a 10 MB cap enforced in the service layer.
		if svc.KVKK != nil {
			r.Route("/kvkk", func(r chi.Router) {
				// Reads — admin, dpo, auditor.
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleDPO, auth.RoleAuditor))
					r.Get("/verbis", kvkk.GetVerbisHandler(svc.KVKK))
					r.Get("/aydinlatma", kvkk.GetAydinlatmaHandler(svc.KVKK))
					r.Get("/dpa", kvkk.GetDpaHandler(svc.KVKK))
					r.Get("/dpia", kvkk.GetDpiaHandler(svc.KVKK))
					r.Get("/consents", kvkk.ListConsentsHandler(svc.KVKK))
				})
				// Mutations — admin + dpo only.
				r.Group(func(r chi.Router) {
					r.Use(auth.RequireRole(auth.RoleAdmin, auth.RoleDPO))
					r.Patch("/verbis", kvkk.PatchVerbisHandler(svc.KVKK))
					r.Post("/aydinlatma/publish", kvkk.PublishAydinlatmaHandler(svc.KVKK))
					r.Post("/dpa/upload", kvkk.UploadDpaHandler(svc.KVKK))
					r.Post("/dpia/upload", kvkk.UploadDpiaHandler(svc.KVKK))
					r.Post("/consents", kvkk.RecordConsentHandler(svc.KVKK))
					r.Delete("/consents/{userID}/{consentType}", kvkk.RevokeConsentHandler(svc.KVKK))
				})
			})
		}

		// --- Settings (Wave 9 Sprint 3A) ---
		// Integrations (third-party credentials), CA mode, retention
		// policy, and backup targets + run history. Every read and
		// every mutation is admin-only — these are deployment-critical
		// knobs that should never be hit by HR/DPO/manager roles.
		r.Route("/settings", func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdmin))

			if svc.Integrations != nil {
				r.Get("/integrations", integrations.ListHandler(svc.Integrations))
				r.Get("/integrations/{service}", integrations.GetHandler(svc.Integrations))
				r.Put("/integrations/{service}", integrations.UpsertHandler(svc.Integrations))
				r.Delete("/integrations/{service}", integrations.DeleteHandler(svc.Integrations))
			}

			if svc.Settings != nil {
				r.Get("/ca-mode", settings.GetCaModeHandler(svc.Settings))
				r.Patch("/ca-mode", settings.UpdateCaModeHandler(svc.Settings))
				r.Get("/retention", settings.GetRetentionHandler(svc.Settings))
				r.Patch("/retention", settings.UpdateRetentionHandler(svc.Settings))
			}

			if svc.BackupTargets != nil {
				r.Get("/backup/targets", backup.ListTargetsHandler(svc.BackupTargets))
				r.Post("/backup/targets", backup.CreateTargetHandler(svc.BackupTargets))
				r.Patch("/backup/targets/{id}", backup.UpdateTargetHandler(svc.BackupTargets))
				r.Delete("/backup/targets/{id}", backup.DeleteTargetHandler(svc.BackupTargets))
				r.Post("/backup/targets/{id}/run", backup.TriggerRunHandler(svc.BackupTargets))
				r.Get("/backup/targets/{id}/runs", backup.ListRunsHandler(svc.BackupTargets))
			}
		})

		// --- Pipeline (Faz 7 #74 + #75) ---
		// GET  /v1/pipeline/dlq    — admin, dpo, investigator
		// POST /v1/pipeline/replay — admin, dpo ONLY (load + compliance)
		//
		// Every replay writes an audit entry BEFORE any NATS publish;
		// dry_run=true returns projected count without publishing. Tenant
		// isolation is enforced by the service layer: all_tenants=true
		// requires DPO role.
		if svc.Pipeline != nil {
			r.Route("/pipeline", func(r chi.Router) {
				r.With(auth.RequireRole(
					auth.RoleAdmin, auth.RoleDPO, auth.RoleInvestigator,
				)).Get("/dlq", pipeline.ListDLQHandler(svc.Pipeline))

				r.With(auth.RequireRole(
					auth.RoleAdmin, auth.RoleDPO,
				)).Post("/replay", pipeline.ReplayHandler(svc.Pipeline))
			})
		}

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
