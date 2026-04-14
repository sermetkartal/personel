// cmd/api/main.go — Admin API entrypoint.
//
// Boot order:
//  1. Load config
//  2. Init logger and tracing
//  3. Connect Vault (AppRole auth, start token renewal)
//  4. Run Postgres migrations, open pool
//  5. Connect ClickHouse (read-only)
//  6. Connect MinIO
//  7. Connect NATS
//  8. Init Keycloak OIDC verifier
//  9. Build all services
// 10. Build chi router
// 11. Start HTTP server
// 12. Graceful shutdown on SIGINT/SIGTERM
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/personel/api/internal/accessreview"
	"github.com/personel/api/internal/apikey"
	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/backup"
	"github.com/personel/api/internal/bcp"
	clickhouseclient "github.com/personel/api/internal/clickhouse"
	"github.com/personel/api/internal/config"
	"github.com/personel/api/internal/destruction"
	"github.com/personel/api/internal/dlpstate"
	"github.com/personel/api/internal/dsr"
	"github.com/personel/api/internal/endpoint"
	"github.com/personel/api/internal/evidence"
	"github.com/personel/api/internal/featureflags"
	"github.com/personel/api/internal/httpserver"
	"github.com/personel/api/internal/incident"
	"github.com/personel/api/internal/integrations"
	"github.com/personel/api/internal/kvkk"
	"github.com/personel/api/internal/legalhold"
	"github.com/personel/api/internal/liveview"
	minioclient "github.com/personel/api/internal/minio"
	"github.com/personel/api/internal/mobile"
	natsclient "github.com/personel/api/internal/nats"
	"github.com/personel/api/internal/observability"
	"github.com/personel/api/internal/pipeline"
	"github.com/personel/api/internal/policy"
	"github.com/personel/api/internal/postgres"
	"github.com/personel/api/internal/reports"
	"github.com/personel/api/internal/screenshots"
	"github.com/personel/api/internal/search"
	"github.com/personel/api/internal/settings"
	"github.com/personel/api/internal/silence"
	"github.com/personel/api/internal/tenant"
	"github.com/personel/api/internal/transparency"
	"github.com/personel/api/internal/user"
	vaultclient "github.com/personel/api/internal/vault"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- 1. Config ---
	cfgPath := os.Getenv("PERSONEL_CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "configs/api.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		slog.Error("config load failed", slog.Any("error", err))
		os.Exit(1)
	}

	// --- 2. Logger + Tracing ---
	log := observability.NewLogger(cfg.Observ.ServiceName, cfg.Observ.ServiceVersion)
	slog.SetDefault(log)

	if cfg.Observ.TracingEnabled {
		shutdown, err := observability.InitTracing(ctx, cfg.Observ.ServiceName, cfg.Observ.ServiceVersion)
		if err != nil {
			log.Error("tracing init failed", slog.Any("error", err))
			os.Exit(1)
		}
		defer shutdown(ctx)
	}

	// --- 3. Vault ---
	vc, err := vaultclient.NewClient(ctx,
		cfg.Vault.Addr,
		cfg.Vault.AppRoleID,
		cfg.Vault.AppRoleSecretID,
		cfg.Vault.TLSCACert,
		cfg.Vault.ControlPlaneSigningKey,
		cfg.Vault.TokenRenewInterval,
		log,
	)
	if err != nil {
		log.Error("vault init failed", slog.Any("error", err))
		os.Exit(1)
	}
	go vc.StartRenewal(ctx)

	// --- 4. Postgres ---
	if err := postgres.RunMigrations(cfg.Postgres.DSN, log); err != nil {
		log.Error("postgres migrations failed", slog.Any("error", err))
		os.Exit(1)
	}
	pool, err := postgres.NewPool(ctx, &cfg.Postgres, log)
	if err != nil {
		log.Error("postgres pool failed", slog.Any("error", err))
		os.Exit(1)
	}
	defer pool.Close()

	// --- 5. ClickHouse ---
	ch, err := clickhouseclient.New(clickhouseclient.Config{
		Addr:      cfg.ClickHouse.Addr,
		Database:  cfg.ClickHouse.Database,
		Username:  cfg.ClickHouse.Username,
		Password:  cfg.ClickHouse.Password,
		TLSEnable: cfg.ClickHouse.TLSEnable,
	})
	if err != nil {
		log.Error("clickhouse init failed", slog.Any("error", err))
		os.Exit(1)
	}
	defer ch.Close()

	// --- 6. MinIO ---
	mc, err := minioclient.New(
		cfg.MinIO.Endpoint,
		cfg.MinIO.AccessKeyID,
		cfg.MinIO.SecretAccessKey,
		cfg.MinIO.UseSSL,
		cfg.MinIO.BucketScreenshots,
		cfg.MinIO.BucketDSR,
		cfg.MinIO.BucketDestruction,
		log,
	)
	if err != nil {
		log.Error("minio init failed", slog.Any("error", err))
		os.Exit(1)
	}

	// --- 7. NATS ---
	natsPublisher, err := natsclient.New(cfg.NATS.URL, cfg.NATS.CredsFile, log)
	if err != nil {
		log.Error("nats init failed", slog.Any("error", err))
		os.Exit(1)
	}
	defer natsPublisher.Close()

	// --- 8. Keycloak OIDC verifier ---
	verifier, err := auth.NewVerifier(ctx, cfg.Keycloak.IssuerURL, cfg.Keycloak.ClientID, log)
	if err != nil {
		log.Error("oidc verifier init failed", slog.Any("error", err))
		os.Exit(1)
	}

	// --- 9. Build services ---
	recorder := audit.NewRecorder(pool, log)

	// Audit stream broker (Faz 6 #66). Constructed here so the single
	// instance is shared between the Recorder (publisher side) and the
	// /v1/audit/stream handler (consumer side). Every successful
	// Recorder.Append call fans out to this broker AFTER the Postgres
	// INSERT commits; the broker then dispatches to live WebSocket
	// subscribers with KVKK keystroke-content stripping applied.
	auditBroker := audit.NewBroker(log)
	recorder.SetBroker(auditBroker)

	tenantSvc := tenant.NewService(pool, recorder, log)
	userSvc := user.NewService(pool, recorder, log)
	endpointSvc := endpoint.NewService(pool, vc, recorder, log, cfg.Server.PublicURL, cfg.Server.GatewayURL)

	// --- Remote command service (Faz 6 #64 #65) ---
	// Separate from endpointSvc because its surface area (store +
	// publisher + audit recorder via interfaces) is independently
	// mockable in unit tests. Reuses the same pool and natsPublisher.
	endpointCmdStore := endpoint.NewPgxCommandStore(pool)
	endpointCmdSvc := endpoint.NewCommandService(endpointCmdStore, natsPublisher, recorder, log)

	policyStore := policy.NewStore(pool)
	policyPub := policy.NewPublisher(natsPublisher, vc, cfg.NATS.PolicySubject, log)
	policySvc := policy.NewService(policyStore, policyPub, recorder, log)

	dsrStore := dsr.NewStore(pool)
	var dsrNotifier dsr.Notifier = &noopDSRNotifier{}
	dsrSvc := dsr.NewService(dsrStore, recorder, dsrNotifier, log)

	// Start DSR SLA job.
	var tenantIDs []string
	if cfg.Tenant.DefaultTenantID != "" {
		tenantIDs = []string{cfg.Tenant.DefaultTenantID}
	}
	dsrSLAJob := dsr.NewSLAJob(dsrStore, dsrNotifier, tenantIDs, log)
	go dsrSLAJob.Run(ctx)

	lhStore := legalhold.NewStore(pool)
	lhSvc := legalhold.NewService(lhStore, recorder, log)

	lvStore := liveview.NewStore(pool)
	var lkMinter liveview.LiveKitTokenMinter = &noopLiveKitMinter{host: cfg.LiveKit.Host}
	lvSvc := liveview.NewService(lvStore, recorder, natsPublisher, vc, lkMinter, liveview.ServiceConfig{
		LiveKitHost:         cfg.LiveKit.Host,
		MaxDuration:         cfg.LiveKit.MaxSessionDuration,
		ApprovalTimeout:     cfg.LiveKit.ApprovalTimeout,
		NATSLiveViewSubject: cfg.NATS.LiveViewSubject,
	}, log)

	reportsSvc := reports.NewService(ch)

	// Roadmap item #68 — real CH aggregation handlers behind /v1/reports/ch.
	// Built on top of the same `ch` connection used by reportsSvc so no
	// additional pool/config is required; nil-safe inside handlers.
	reportsCHClient := clickhouseclient.NewClient(ch)
	reportsCHHandlers := reports.NewCHHandlers(reportsCHClient)

	// Roadmap item #87 — trend analysis service over employee_daily_stats.
	trendStore := reports.NewPGTrendStore(pool)
	trendSvc := reports.NewTrendService(trendStore)
	trendsHandler := reports.NewTrendsHandler(trendSvc)

	// Roadmap item #72 — service-to-service API key auth.
	apikeyStore := apikey.NewStore(pool)
	apikeySvc := apikey.NewService(apikeyStore, "dev", recorder, log)

	// --- Roadmap item #67 — Search (OpenSearch full-text) ---
	// Degraded-mode graceful: if the cluster is unreachable at boot,
	// NewClient returns an error and we pass nil into NewService so
	// /v1/search/* returns 503 until the cluster is back online. The
	// API boot must not block on the search tier.
	var searchClient *search.Client
	if cfg.OpenSearch.Enabled {
		sc, serr := search.NewClient(ctx, search.Config{
			Addr:     cfg.OpenSearch.Addr,
			Username: cfg.OpenSearch.Username,
			Password: cfg.OpenSearch.Password,
			Timeout:  cfg.OpenSearch.Timeout,
		}, log)
		if serr != nil {
			log.Warn("search: client init failed; /v1/search/* will return 503",
				slog.String("error", serr.Error()),
				slog.String("addr", cfg.OpenSearch.Addr),
			)
		} else {
			searchClient = sc
		}
	} else {
		log.Info("search: disabled by config; /v1/search/* will return 503")
	}
	searchSvc := search.NewService(searchClient, log)

	screenshotsSvc := screenshots.NewService(mc, recorder, cfg.MinIO.PresignTTL, log)

	transSvc := transparency.NewService(pool, lvSvc, log)

	// DLP state service — uses a stub Vault bootstrap client since the real
	// dlp-bootstrap AppRole is invoked by dlp-enable.sh with its own token.
	// TODO(devops): provision the dlp-bootstrap AppRole and pass its raw Vault
	// client here when enabling DLP in production.
	dlpStateStore := dlpstate.NewStore(pool)
	dlpBootstrapVault := dlpstate.NewVaultBootstrapClient(nil) // stub until provisioned
	dlpStateSvc := dlpstate.NewService(dlpStateStore, dlpBootstrapVault, recorder, log)

	silenceSvc := silence.NewService(ch, pool, recorder, log)

	destGen := destruction.NewGenerator(pool, ch, mc, vc, recorder, log)
	destSvc := destruction.NewService(destGen)

	// --- Audit verifier (nightly cron) + WORM sink (ADR 0014) ---
	verifierSink := &noopExternalSink{}
	wormSink, err := audit.NewWORMSink(audit.WORMSinkConfig{
		Endpoint:        cfg.MinIO.Endpoint,
		AccessKeyID:     cfg.MinIO.AuditSinkAccessKey,
		SecretAccessKey: cfg.MinIO.AuditSinkSecretKey,
		UseSSL:          cfg.MinIO.UseSSL,
	}, log)
	if err != nil {
		log.Warn("WORM sink unavailable at startup; audit verifier will run without cross-validation",
			slog.String("error", err.Error()))
		wormSink = nil
	}
	auditVerifier := audit.NewVerifier(pool, vc, verifierSink, wormSink, recorder, log)
	go runAuditVerifierJob(ctx, auditVerifier, tenantIDs, log)

	// --- Evidence locker (Phase 3.0 — ADR 0023 SOC 2 Type II) ---
	// The evidence Store requires a WORM sink; if MinIO was unavailable at
	// startup wormSink is nil and evidence.Record() will refuse writes so
	// we never produce auditor-facing evidence without an integrity anchor.
	//
	// The Signer is the same vault.Client already used for audit checkpoint
	// signing — its Sign(ctx, payload) method satisfies evidence.Signer by
	// interface shape. Sharing the control-plane signing key between audit
	// checkpoints and evidence items gives auditors a single key-rotation
	// history to audit against both artifact families.
	var evidenceStore *evidence.Store
	var evidenceRecorder *evidence.RecorderImpl
	var evidencePackBuilder *evidence.PackBuilder
	// Compile-time assertion: vault.Client must satisfy evidence.Signer
	// AND evidence.Verifier. If either side's method signature drifts,
	// this errors at build time rather than at first Record() or at
	// first auditor pack-verify call under load.
	var _ evidence.Signer = (*vaultclient.Client)(nil)
	var _ evidence.Verifier = (*vaultclient.Client)(nil)
	if wormSink != nil {
		evidenceStore = evidence.NewStore(pool, wormSink)
		evidenceRecorder = evidence.NewRecorder(evidenceStore, vc, log)
		evidencePackBuilder = evidence.NewPackBuilder(evidenceStore, vc)
		log.Info("evidence locker ready",
			slog.String("signer", "vault:control-plane"),
			slog.String("worm_bucket", audit.WORMBucket),
		)

		// Wire the evidence recorder into domain collectors. Each
		// collector attaches via a setter so the constructor signature
		// stays stable for existing tests. Add a new SetEvidenceRecorder
		// call here for every future collector (CC9.1 BCP runs, A1.2
		// backups, etc.).
		lvSvc.SetEvidenceRecorder(evidenceRecorder)     // CC6.1 privileged access
		policySvc.SetEvidenceRecorder(evidenceRecorder) // CC8.1 change authorization
		dsrSvc.SetEvidenceRecorder(evidenceRecorder)    // P7.1 DSR fulfilment
	} else {
		log.Warn("evidence locker disabled: WORM sink unavailable; domain collectors must handle nil Recorder")
	}
	_ = evidenceStore // referenced via the recorder; retained for future direct queries

	// Phase 3.0.4 collector services — constructed unconditionally.
	// When evidenceRecorder is nil they still write audit entries; the
	// SOC 2 evidence emission is the only thing skipped in scaffold mode.
	backupSvc := backup.NewService(recorder, evidenceRecorder, log)

	// Wave 9 Sprint 3A — settings surface services. vc (vault client)
	// is shared with the audit / evidence signers; it already
	// implements Encrypt / Decrypt and can be reused here for the
	// integrations and backup_targets transit keys. The settings and
	// backup target services gracefully degrade when vc is nil — GET
	// endpoints still work (rows are returned masked) but mutations
	// short-circuit with ErrVaultUnavailable.
	var integrationsVault integrations.VaultEncryptor
	var backupTargetsVault backup.VaultEncryptor
	if vc != nil {
		integrationsVault = vc
		backupTargetsVault = vc
	}
	integrationsSvc := integrations.NewService(pool, recorder, integrationsVault, log)
	settingsSvc := settings.NewService(pool, recorder, log)
	backupTargetsSvc := backup.NewTargetService(pool, recorder, backupTargetsVault, log)
	accessReviewSvc := accessreview.NewService(recorder, evidenceRecorder, log)
	incidentSvc := incident.NewService(recorder, evidenceRecorder, log)
	bcpSvc := bcp.NewService(recorder, evidenceRecorder, log)

	// --- 10. Mobile BFF service (Phase 2.9) ---
	mobileSvc := mobile.NewService(pool, recorder, log, dsrSvc, lvSvc, silenceSvc, dlpStateSvc)

	// --- 10. KVKK compliance service (Wave 9 Sprint 2A) ---
	// VERBİS / aydınlatma metni / DPA / DPIA / açık rıza backend. PDF
	// uploads are routed through the minio.Client.PutObject path — the
	// "kvkk/" key prefix lands in the DSR bucket (tamper-evident long
	// retention is shared between DSR artefacts and KVKK documents).
	kvkkSvc := kvkk.NewService(pool, recorder, kvkkDocAdapter{mc: mc}, log)

	// --- 10b. Pipeline service (Faz 7 #73 + #74 + #75) ---
	// DLQ inspection + replay. The DLQReader and EventPublisher share
	// the same NATS JetStream context already owned by natsPublisher —
	// we do NOT open a second NATS connection. The CH count source is
	// the same read-only client used by reportsCHClient; a nil client
	// degrades the /v1/pipeline/replay CH path to "unavailable" at
	// request time (handler returns 500 with a clear error) rather
	// than panicking at boot.
	pipelineNATS := natsPublisher.JS()
	pipelineReader := pipeline.NewNATSDLQReader(pipelineNATS, log)
	pipelinePub := pipeline.NewNATSPublisher(pipelineNATS, log)
	// ClickHouse count adapter is a thin wrapper so the pipeline
	// package does not depend on apps/api/internal/clickhouse.
	var pipelineCH pipeline.CHEventSource
	if reportsCHClient != nil {
		pipelineCH = pipelineCHAdapter{client: reportsCHClient}
	}
	pipelineSvc := pipeline.NewService(pipelineReader, pipelinePub, pipelineCH, recorder, log)

	// --- 10c. Feature flags service (Faz 16 #173) ---
	// Pure-Go evaluator + admin surface. Safe to construct even if the
	// feature_flags table is missing — List returns empty and every
	// IsEnabled call falls through to the caller's default. Admin
	// console mounts /v1/system/feature-flags when the Services field is
	// non-nil.
	featureFlagsSvc := featureflags.NewService(pool, recorder, log)

	// --- 11. Prometheus metrics ---
	reg := observability.NewRegistry()
	reg.MustRegister(prometheus.NewGoCollector())
	reg.MustRegister(prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}))

	// Evidence coverage collector — computes per-tenant × per-control
	// gauge values at scrape time from live Postgres. Populated with the
	// tenant list seen by the audit verifier (same source of truth for
	// "who are we running for"). Alert rule in infra/compose/prometheus
	// catches any 24h zero-coverage window.
	coverageCollector := evidence.NewCoverageCollector(pool, log)
	coverageCollector.SetTenants(tenantIDs)
	reg.MustRegister(coverageCollector)

	met := httpserver.NewMetrics(reg)

	// --- 12. Chi router + HTTP server ---
	handler := httpserver.BuildRouter(&httpserver.Services{
		Cfg:          cfg,
		Verifier:     verifier,
		Recorder:     recorder,
		Tenant:       tenantSvc,
		User:         userSvc,
		Endpoint:     endpointSvc,
		EndpointCmd:  endpointCmdSvc,
		Policy:       policySvc,
		DSR:          dsrSvc,
		LegalHold:    lhSvc,
		Destruction:  destSvc,
		LiveView:     lvSvc,
		Reports:      reportsSvc,
		ReportsCH:    reportsCHHandlers,
		Trends:       trendsHandler,
		APIKey:       apikeySvc,
		Search:       searchSvc,
		Screenshots:  screenshotsSvc,
		Transparency: transSvc,
		Silence:      silenceSvc,
		DLPState:     dlpStateSvc,
		Mobile:       mobileSvc,
		Evidence:     evidenceStore,
		EvidencePack: evidencePackBuilder,
		Backup:        backupSvc,
		BackupTargets: backupTargetsSvc,
		Integrations:  integrationsSvc,
		Settings:      settingsSvc,
		Pipeline:     pipelineSvc,
		FeatureFlags: featureFlagsSvc,
		AccessReview: accessReviewSvc,
		Incident:     incidentSvc,
		BCP:          bcpSvc,
		KVKK:         kvkkSvc,
		DBPool:       pool,
		AuditBroker:  auditBroker,
		Log:          log,
	}, met)

	srv := httpserver.New(&cfg.HTTP, handler, log)

	// --- 12. Signal handling + graceful shutdown ---
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		log.Info("shutdown signal received")
		cancel()
		shutCtx, shutCancel := context.WithTimeout(context.Background(), cfg.HTTP.ShutdownTimeout)
		defer shutCancel()
		if err := srv.Shutdown(shutCtx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("graceful shutdown failed", slog.Any("error", err))
		}
	}()

	log.Info("personel admin api starting",
		slog.String("addr", cfg.HTTP.Addr),
		slog.String("service", cfg.Observ.ServiceName),
	)

	if err := srv.Start(); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("server error", slog.Any("error", err))
		os.Exit(1)
	}
}

// runAuditVerifierJob runs the nightly audit verifier at 02:30 local time.
func runAuditVerifierJob(ctx context.Context, v *audit.Verifier, tenantIDs []string, log *slog.Logger) {
	for {
		now := time.Now()
		// Schedule next 02:30.
		next := time.Date(now.Year(), now.Month(), now.Day(), 2, 30, 0, 0, now.Location())
		if next.Before(now) {
			next = next.Add(24 * time.Hour)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(next)):
			for _, tid := range tenantIDs {
				if err := v.RunForTenant(ctx, tid); err != nil {
					log.Error("audit verifier failed",
						slog.String("tenant_id", tid),
						slog.Any("error", err),
					)
				}
			}
		}
	}
}

// noopDSRNotifier is a no-op notifier for use until a real one is wired.
type noopDSRNotifier struct{}

func (n *noopDSRNotifier) NotifyDPO(_ context.Context, _, _, _ string) error         { return nil }
func (n *noopDSRNotifier) NotifyEmployee(_ context.Context, _, _, _, _ string) error { return nil }
func (n *noopDSRNotifier) EscalateToDPOSecondary(_ context.Context, _, _ string) error { return nil }

// noopLiveKitMinter is a stub until the LiveKit SDK is wired.
type noopLiveKitMinter struct{ host string }

func (m *noopLiveKitMinter) MintAdminToken(room string, ttl time.Duration) (string, error) {
	return "admin-token-stub-" + room, nil
}
func (m *noopLiveKitMinter) MintAgentToken(room, sessionID string, ttl time.Duration) (string, error) {
	return "agent-token-stub-" + sessionID, nil
}
func (m *noopLiveKitMinter) CreateRoom(_ context.Context, room string) error { return nil }

// noopExternalSink is a no-op external sink for the audit verifier.
type noopExternalSink struct{}

func (s *noopExternalSink) Write(_ context.Context, _ time.Time, _ string, _ []byte) error {
	return nil
}

// pipelineCHAdapter wraps the read-only ClickHouse client used by the
// reports handlers into the narrow CHEventSource interface consumed by
// the pipeline replay service. Keeping the adapter here (rather than
// inside the pipeline package) lets the pipeline package stay free of
// the clickhouseclient import.
type pipelineCHAdapter struct {
	client *clickhouseclient.Client
}

// Count exists as a Phase 1 projection surface for /v1/pipeline/replay.
// The read-only Client does not yet expose a generic Count method;
// return 0 here so the replay endpoint reports "projected=0" instead
// of blowing up. Real counting will be wired alongside Phase 2 CH
// replay reconstruction.
func (a pipelineCHAdapter) Count(_ context.Context, _ pipeline.CHReplayFilter) (int, error) {
	return 0, nil
}

// kvkkDocAdapter satisfies kvkk.DocumentStore by delegating to the
// existing minio.Client.PutObject path. Keeping it here avoids a
// cross-package dependency from kvkk into minio — the kvkk package
// only knows the narrow DocumentStore interface, and this adapter is
// the single wiring point.
//
// Compile-time assertion: if minio.Client.PutObject ever changes
// signature the build breaks here rather than at first upload.
type kvkkDocAdapter struct {
	mc *minioclient.Client
}

var _ kvkk.DocumentStore = kvkkDocAdapter{}

func (a kvkkDocAdapter) PutDocument(ctx context.Context, key string, data []byte, contentType string) error {
	return a.mc.PutObject(ctx, key, data, contentType)
}
