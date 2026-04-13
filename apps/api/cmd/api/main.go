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
	"github.com/personel/api/internal/httpserver"
	"github.com/personel/api/internal/incident"
	"github.com/personel/api/internal/legalhold"
	"github.com/personel/api/internal/liveview"
	minioclient "github.com/personel/api/internal/minio"
	"github.com/personel/api/internal/mobile"
	natsclient "github.com/personel/api/internal/nats"
	"github.com/personel/api/internal/observability"
	"github.com/personel/api/internal/policy"
	"github.com/personel/api/internal/postgres"
	"github.com/personel/api/internal/reports"
	"github.com/personel/api/internal/screenshots"
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

	tenantSvc := tenant.NewService(pool, recorder, log)
	userSvc := user.NewService(pool, recorder, log)
	endpointSvc := endpoint.NewService(pool, vc, recorder, log, cfg.Server.PublicURL, cfg.Server.GatewayURL)

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
	accessReviewSvc := accessreview.NewService(recorder, evidenceRecorder, log)
	incidentSvc := incident.NewService(recorder, evidenceRecorder, log)
	bcpSvc := bcp.NewService(recorder, evidenceRecorder, log)

	// --- 10. Mobile BFF service (Phase 2.9) ---
	mobileSvc := mobile.NewService(pool, recorder, log, dsrSvc, lvSvc, silenceSvc, dlpStateSvc)

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
		Policy:       policySvc,
		DSR:          dsrSvc,
		LegalHold:    lhSvc,
		Destruction:  destSvc,
		LiveView:     lvSvc,
		Reports:      reportsSvc,
		Screenshots:  screenshotsSvc,
		Transparency: transSvc,
		Silence:      silenceSvc,
		DLPState:     dlpStateSvc,
		Mobile:       mobileSvc,
		Evidence:     evidenceStore,
		EvidencePack: evidencePackBuilder,
		Backup:       backupSvc,
		AccessReview: accessReviewSvc,
		Incident:     incidentSvc,
		BCP:          bcpSvc,
		DBPool:       pool,
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
