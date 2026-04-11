// Command livrec is the livrec-service binary.
//
// Service wiring order (per ADR 0019):
//  1. Load config from env (no secrets in config files)
//  2. Initialise Vault client + start token renewal goroutine
//  3. Initialise MinIO client + ensure bucket exists
//  4. Construct crypto layer (LVMKDeriver)
//  5. Construct audit forwarder (async HTTP to Admin API)
//  6. Construct upload + playback + export handlers
//  7. Start TTL scheduler goroutine
//  8. Start chi HTTP server
//  9. Block on SIGINT/SIGTERM + graceful shutdown with 30s timeout
//
// Per ADR 0019 quality bar: no hardcoded secrets — all credentials from env.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/personel/livrec/internal/audit"
	"github.com/personel/livrec/internal/config"
	"github.com/personel/livrec/internal/crypto"
	"github.com/personel/livrec/internal/export"
	"github.com/personel/livrec/internal/httpserver"
	"github.com/personel/livrec/internal/playback"
	"github.com/personel/livrec/internal/retention"
	"github.com/personel/livrec/internal/storage"
	"github.com/personel/livrec/internal/upload"
	"github.com/personel/livrec/internal/vault"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(log)

	if err := run(log); err != nil {
		log.Error("livrec: fatal", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	// 1. Config.
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log.Info("livrec: config loaded", slog.String("listen", cfg.ListenAddr))

	// Root context — cancelled on signal.
	rootCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 2. Vault client.
	vc, err := vault.NewClient(
		rootCtx,
		cfg.VaultAddr,
		cfg.VaultRoleID,
		cfg.VaultSecretID,
		cfg.VaultCACert,
		cfg.VaultLVMKPath,
		cfg.VaultSignerPath,
		cfg.VaultRenewInterval,
		log,
	)
	if err != nil {
		return err
	}
	go vc.StartRenewal(rootCtx)
	log.Info("livrec: vault client initialised")

	// 3. MinIO client.
	minioClient, err := storage.NewClient(
		rootCtx,
		cfg.MinIOEndpoint,
		cfg.MinIOAccessKey,
		cfg.MinIOSecretKey,
		cfg.MinIOBucket,
		cfg.MinIOUseTLS,
		log,
	)
	if err != nil {
		return err
	}
	log.Info("livrec: minio client initialised", slog.String("bucket", cfg.MinIOBucket))

	// 4. Crypto layer.
	deriver := crypto.NewLVMKDeriver(vc)

	// 5. Audit forwarder.
	auditor := audit.NewRecorder(cfg.AdminAPIBaseURL, cfg.AdminAPIToken, log)

	// 6. Handlers.
	sessionStore := upload.NewSessionStore()
	chunkHandler := upload.NewChunkHandler(sessionStore, minioClient, deriver, auditor, log)

	approvalGate := playback.NewApprovalGate(cfg.AdminAPIBaseURL, cfg.AdminAPIToken, log)
	dekDelivery := playback.NewDEKDelivery(deriver, log)

	// RecordingStore stub — Phase 3 replaces with real Postgres queries.
	recStore := &stubRecordingStore{}
	streamHandler := playback.NewStreamHandler(approvalGate, dekDelivery, minioClient, recStore, auditor, log)

	// ForensicExport — RecordingMetaProvider stub.
	exportMeta := &stubExportMetaProvider{}
	forensicHandler := export.NewForensicHandler(exportMeta, minioClient, deriver, vc, auditor, log)

	// 7. TTL scheduler.
	holdChecker := retention.NewPostgresLegalHoldChecker()
	sessExpirer := &stubSessionExpirer{} // Phase 3 wires Postgres.
	ttlScheduler := retention.NewTTLScheduler(
		sessExpirer,
		holdChecker,
		minioClient,
		auditor,
		24*time.Hour,
		log,
	)
	go ttlScheduler.Run(rootCtx)
	log.Info("livrec: ttl scheduler started")

	// 8. HTTP server.
	srv := httpserver.New(cfg.ListenAddr, httpserver.Handlers{
		ChunkUpload:    chunkHandler,
		PlaybackStream: streamHandler,
		ForensicExport: forensicHandler,
	}, cfg.AdminAPIToken, log)

	// Signal handler.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start server in a goroutine.
	serverErr := make(chan error, 1)
	go func() {
		log.Info("livrec: http server starting", slog.String("addr", cfg.ListenAddr))
		if err := srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// 9. Block until signal or server error.
	select {
	case sig := <-sigCh:
		log.Info("livrec: signal received, shutting down", slog.String("signal", sig.String()))
	case err := <-serverErr:
		return err
	}

	// Graceful shutdown with 30s timeout.
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutCancel()
	cancel() // Stop background goroutines.

	if err := srv.Shutdown(shutCtx); err != nil {
		log.Error("livrec: shutdown error", slog.Any("error", err))
		return err
	}
	log.Info("livrec: stopped cleanly")
	return nil
}

// ---------------------------------------------------------------------------
// Stubs — Phase 3 replaces with real Postgres implementations.
// ---------------------------------------------------------------------------

// stubRecordingStore implements playback.RecordingStore.
type stubRecordingStore struct{}

func (s *stubRecordingStore) GetRecordingMeta(_ context.Context, _ string) (*playback.RecordingMeta, error) {
	// Phase 3: SELECT id, tenant_id, dek_wrap, lvmk_version, chunk_count
	// FROM live_view_recordings WHERE session_id = $1.
	return nil, nil
}

// stubExportMetaProvider implements export.RecordingMetaProvider.
type stubExportMetaProvider struct{}

func (s *stubExportMetaProvider) GetRecordingForExport(_ context.Context, _ string) (*export.RecordingExportMeta, error) {
	// Phase 3: full SELECT from live_view_recordings.
	return nil, nil
}

// stubSessionExpirer implements retention.SessionExpirer.
type stubSessionExpirer struct{}

func (s *stubSessionExpirer) ListExpiredSessions(_ context.Context, _ time.Time) ([]retention.ExpiredSession, error) {
	// Phase 3: SELECT ... FROM live_view_recordings
	// WHERE ttl_expires_at <= $1 AND destroyed_at IS NULL AND legal_hold_id IS NULL.
	return nil, nil
}

func (s *stubSessionExpirer) MarkDestroyed(_ context.Context, _ string, _ time.Time) error {
	// Phase 3: UPDATE live_view_recordings SET destroyed_at = $2 WHERE session_id = $1.
	return nil
}
