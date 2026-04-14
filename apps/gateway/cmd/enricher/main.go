// Command enricher is the Personel event enrichment and sink binary.
//
// It runs as a separate process from the gateway. Responsibilities:
//  1. Consumes EventBatch messages from NATS JetStream (events.raw.>).
//  2. Enriches each event with tenant/endpoint metadata from Postgres.
//  3. Applies the SensitivityGuard policy (window title regex, host globs).
//  4. Routes events to the appropriate ClickHouse table (normal or sensitive).
//  5. Blob-reference events (screenshot, keystroke) carry their MinIO paths
//     from the agent; the enricher records them in ClickHouse without fetching
//     the actual blob content (that is the DLP service's responsibility).
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

	"github.com/personel/gateway/internal/clickhouse"
	"github.com/personel/gateway/internal/config"
	"github.com/personel/gateway/internal/enricher"
	minioclient "github.com/personel/gateway/internal/minio"
	"github.com/personel/gateway/internal/observability"
	"github.com/personel/gateway/internal/postgres"
)

func main() {
	cfgFile := flag.String("config", "configs/enricher.yaml", "path to enricher config file")
	flag.Parse()

	logger := observability.InitLogger(os.Stderr, slog.LevelInfo)

	cfg, err := config.LoadEnricher(*cfgFile)
	if err != nil {
		logger.Error("enricher: load config failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	logger = observability.InitLogger(os.Stderr, observability.ParseLogLevel(cfg.Observ.LogLevel))
	slog.SetDefault(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// ----- Observability -----
	tp, err := observability.InitTracing(ctx, cfg.Observ)
	if err != nil {
		logger.Error("enricher: tracing init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer func() {
		sc, scc := context.WithTimeout(context.Background(), 10*time.Second)
		defer scc()
		_ = tp.Shutdown(sc)
	}()

	metrics := observability.NewMetrics(nil)

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
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("enricher metrics server error", slog.String("error", err.Error()))
		}
	}()

	// ----- Postgres -----
	db, err := postgres.New(ctx, cfg.Postgres)
	if err != nil {
		logger.Error("enricher: postgres init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer db.Close()

	// ----- ClickHouse -----
	ch, err := clickhouse.New(ctx, cfg.ClickHouse, logger)
	if err != nil {
		logger.Error("enricher: clickhouse init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer ch.Close()

	batcher := clickhouse.NewBatcher(ch.Conn(), cfg.Batch, metrics, logger)
	go batcher.Run(ctx)

	// ----- MinIO -----
	mc, err := minioclient.New(ctx, cfg.MinIO, logger)
	if err != nil {
		logger.Error("enricher: minio init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	if err := mc.BootstrapLifecycle(ctx); err != nil {
		logger.Warn("enricher: minio lifecycle bootstrap failed (non-fatal)",
			slog.String("error", err.Error()),
		)
	}

	// ----- NATS JetStream -----
	natsOpts := []nats.Option{
		nats.Name("personel-enricher"),
		nats.MaxReconnects(cfg.NATS.MaxReconnect),
		nats.ReconnectWait(2 * time.Second),
	}
	if cfg.NATS.CredsFile != "" {
		natsOpts = append(natsOpts, nats.UserCredentials(cfg.NATS.CredsFile))
	}
	urls := ""
	for i, u := range cfg.NATS.URLs {
		if i > 0 {
			urls += ","
		}
		urls += u
	}
	nc, err := nats.Connect(urls, natsOpts...)
	if err != nil {
		logger.Error("enricher: NATS connect failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		logger.Error("enricher: JetStream context failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// ----- Enricher pipeline -----
	enrich := enricher.NewEnricher(db)

	// Optional MaxMind GeoLite2 server-side GeoIP enrichment.
	// Disabled when cfg.GeoIP.MMDBPath is empty. When the path is set
	// but the open fails, we log a warning and continue — a
	// misconfigured GeoIP file should not take the pipeline down.
	if cfg.GeoIP.MMDBPath != "" {
		geo, gerr := enricher.NewGeoLookup(cfg.GeoIP.MMDBPath)
		if gerr != nil {
			logger.Warn("enricher: geoip lookup open failed (continuing without geo)",
				slog.String("path", cfg.GeoIP.MMDBPath),
				slog.String("error", gerr.Error()),
			)
		} else if geo != nil {
			enrich.SetGeoLookup(geo)
			defer func() { _ = geo.Close() }()
			logger.Info("enricher: geoip lookup initialised",
				slog.String("path", geo.Path()),
			)
		}
	} else {
		logger.Info("enricher: geoip lookup disabled (no mmdb_path configured)")
	}

	router := enricher.NewRouter()
	consumer := enricher.NewConsumer(js, batcher, enrich, router, metrics, logger)

	// Faz 7 item #78: deduplication cache (100k entries, 5m TTL).
	// Absorbs at-least-once redelivery before hitting ClickHouse.
	deduper := enricher.NewDeduper(100_000, 5*time.Minute, nil)
	defer deduper.Close()
	consumer.SetDeduper(deduper)

	// Faz 7 item #80: data-quality monitoring.
	dqm := enricher.NewDQM(nil)
	consumer.SetDQM(dqm)

	// Faz 7 items #73 + #74: schema-versioned decoder + dead letter queue.
	// The decoder dispatches on the NATS "schema-version" header so v1
	// and v2 wire schemas can coexist during a fleet migration. The DLQ
	// absorbs any message that fails decode / enrich / sink write after
	// MaxRetries retries, preventing infinite redelivery loops.
	consumer.SetDecoder(enricher.NewDefaultDecoder())
	dlqPublisher := enricher.NewDLQPublisher(js, enricher.DLQPublisherConfig{MaxRetries: 3}, logger)
	consumer.SetDLQ(dlqPublisher)

	// ----- Signal handling -----
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigCh
		logger.Info("enricher: shutdown signal received")
		cancel()
	}()

	consumerCfg := enricher.DefaultConsumerConfig()
	logger.Info("enricher: starting consumer loop")
	if err := consumer.Run(ctx, consumerCfg); err != nil && ctx.Err() == nil {
		logger.Error("enricher: consumer exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// Final flush.
	flushCtx, fc := context.WithTimeout(context.Background(), 30*time.Second)
	defer fc()
	if err := batcher.Flush(flushCtx); err != nil {
		logger.Warn("enricher: final flush failed", slog.String("error", err.Error()))
	}
	logger.Info("enricher: stopped cleanly")
}
