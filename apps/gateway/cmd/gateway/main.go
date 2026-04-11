// Command gateway is the Personel Ingest Gateway binary.
//
// It:
//  1. Loads configuration from YAML + environment variables.
//  2. Initialises observability (OTel tracing + Prometheus + slog).
//  3. Connects to Vault (AppRole auth), Postgres, and NATS JetStream.
//  4. Bootstraps JetStream streams and MinIO lifecycle.
//  5. Starts the heartbeat monitor.
//  6. Starts the live view router NATS subscription.
//  7. Starts the gRPC server and blocks until SIGTERM/SIGINT.
//  8. Runs graceful shutdown: stop accepting streams → drain → flush NATS.
package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/personel/gateway/internal/config"
	"github.com/personel/gateway/internal/grpcserver"
	"github.com/personel/gateway/internal/heartbeat"
	"github.com/personel/gateway/internal/liveview"
	natspkg "github.com/personel/gateway/internal/nats"
	"github.com/personel/gateway/internal/observability"
	"github.com/personel/gateway/internal/postgres"
	"github.com/personel/gateway/internal/vault"
)

func main() {
	cfgFile := flag.String("config", "configs/gateway.yaml", "path to gateway config file")
	flag.Parse()

	// Bootstrap a provisional logger before config is loaded.
	logger := observability.InitLogger(os.Stderr, slog.LevelInfo)

	cfg, err := config.LoadGateway(*cfgFile)
	if err != nil {
		logger.Error("failed to load config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Re-init logger with configured level.
	logger = observability.InitLogger(os.Stderr, observability.ParseLogLevel(cfg.Observ.LogLevel))
	slog.SetDefault(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ----- Observability -----
	tp, err := observability.InitTracing(ctx, cfg.Observ)
	if err != nil {
		logger.Error("tracing init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() {
		shutCtx, sc := context.WithTimeout(context.Background(), 10*time.Second)
		defer sc()
		_ = tp.Shutdown(shutCtx)
	}()

	metrics := observability.NewMetrics(nil)

	// Prometheus metrics HTTP endpoint.
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		srv := &http.Server{
			Addr:         cfg.Observ.PrometheusAddr,
			Handler:      mux,
			ReadTimeout:  5 * time.Second,
			WriteTimeout: 10 * time.Second,
		}
		logger.Info("metrics: listening", slog.String("addr", cfg.Observ.PrometheusAddr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("metrics server error", slog.String("error", err.Error()))
		}
	}()

	// ----- Vault -----
	vc, err := vault.New(ctx, cfg.Vault)
	if err != nil {
		logger.Error("vault init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if err := vc.RefreshDenyList(ctx); err != nil {
		logger.Warn("vault deny list refresh failed (non-fatal)", slog.String("error", err.Error()))
	}

	// ----- Postgres -----
	db, err := postgres.New(ctx, cfg.Postgres)
	if err != nil {
		logger.Error("postgres init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer db.Close()

	// ----- NATS JetStream -----
	natsOpts := []nats.Option{
		nats.Name("personel-gateway"),
		nats.MaxReconnects(cfg.NATS.MaxReconnect),
		nats.ReconnectWait(2 * time.Second),
	}
	if cfg.NATS.CredsFile != "" {
		natsOpts = append(natsOpts, nats.UserCredentials(cfg.NATS.CredsFile))
	}
	publisher, err := natspkg.NewPublisher(ctx, cfg.NATS, metrics, logger)
	if err != nil {
		logger.Error("NATS publisher init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer publisher.Close()

	// Get a raw JetStream context for the heartbeat publisher and live view router.
	urls := ""
	for i, u := range cfg.NATS.URLs {
		if i > 0 {
			urls += ","
		}
		urls += u
	}
	nc, err := nats.Connect(urls, natsOpts...)
	if err != nil {
		logger.Error("NATS connect failed (secondary)", slog.String("error", err.Error()))
		os.Exit(1)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		logger.Error("NATS JetStream context failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// ----- Heartbeat monitor (Flow 7) -----
	hbPublisher := heartbeat.NewPublisher(js, logger)
	monitor := heartbeat.NewMonitor(cfg.Heartbeat, metrics, logger, hbPublisher)
	go monitor.Start(ctx)

	// ----- Live view router -----
	lvRouter := liveview.NewRouter(logger)
	if err := lvRouter.SubscribeNATS(ctx, js); err != nil {
		logger.Error("live view router NATS subscription failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Subscribe to pki.v1.revoke to refresh the deny list.
	go subscribePKIRevoke(ctx, js, vc, logger)

	// ----- gRPC server -----
	srv, err := grpcserver.New(grpcserver.Deps{
		DB:        db,
		Publisher: publisher,
		Vault:     vc,
		Monitor:   monitor,
		Router:    lvRouter,
		Metrics:   metrics,
		Logger:    logger,
		Cfg:       cfg,
	})
	if err != nil {
		logger.Error("gRPC server init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// ----- Signal handling -----
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		<-sigCh
		logger.Info("gateway: shutdown signal received")
		cancel()
	}()

	// Serve blocks until ctx is cancelled.
	if err := srv.Serve(ctx, cfg.GRPC.ListenAddr); err != nil {
		logger.Error("gateway: serve error", slog.String("error", err.Error()))
		os.Exit(1)
	}

	graceful := cfg.GRPC.GracefulStop
	if graceful == 0 {
		graceful = 30 * time.Second
	}
	logger.Info("gateway: graceful stop", slog.Duration("timeout", graceful))
	srv.GracefulStop(graceful)
	logger.Info("gateway: stopped cleanly")
}

// subscribePKIRevoke listens for cert revocation events on the NATS
// pki.v1.revoke subject and updates the in-memory deny list.
func subscribePKIRevoke(ctx context.Context, js jetstream.JetStream, vc *vault.Client, logger *slog.Logger) {
	cons, err := js.CreateOrUpdateConsumer(ctx, "pki_events", jetstream.ConsumerConfig{
		Durable:       "gateway-pki-revoke",
		FilterSubject: "pki.v1.revoke",
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverNewPolicy,
	})
	if err != nil {
		logger.Warn("pki revoke subscribe failed", slog.String("error", err.Error()))
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		batch, err := cons.FetchNoWait(16)
		if err != nil {
			continue
		}
		for msg := range batch.Messages() {
			serial := string(msg.Data())
			vc.AddToDenyList(serial)
			logger.Info("pki: serial added to deny list", slog.String("serial", serial))
			_ = msg.Ack()
		}
	}
}
