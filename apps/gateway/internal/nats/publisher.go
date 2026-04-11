package nats

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	natslib "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/personel/gateway/internal/config"
	"github.com/personel/gateway/internal/observability"
)

// Publisher publishes proto-serialized event payloads to NATS JetStream with
// synchronous publish-ACK confirmation. Each Publish call blocks until NATS
// confirms durability (or the configured timeout expires).
type Publisher struct {
	js      jetstream.JetStream
	nc      *natslib.Conn
	cfg     config.NATSConfig
	metrics *observability.Metrics
	logger  *slog.Logger
}

// NewPublisher creates a Publisher, establishing a NATS connection and
// bootstrapping all required JetStream streams.
func NewPublisher(ctx context.Context, cfg config.NATSConfig, metrics *observability.Metrics, logger *slog.Logger) (*Publisher, error) {
	opts := []natslib.Option{
		natslib.Name("personel-gateway"),
		natslib.MaxReconnects(cfg.MaxReconnect),
		natslib.ReconnectWait(2 * time.Second),
		natslib.DisconnectErrHandler(func(_ *natslib.Conn, err error) {
			if err != nil {
				logger.Warn("nats: disconnected", slog.String("error", err.Error()))
			}
		}),
		natslib.ReconnectHandler(func(nc *natslib.Conn) {
			logger.Info("nats: reconnected", slog.String("url", nc.ConnectedUrl()))
		}),
		natslib.ClosedHandler(func(_ *natslib.Conn) {
			logger.Info("nats: connection closed permanently")
		}),
	}
	if cfg.CredsFile != "" {
		opts = append(opts, natslib.UserCredentials(cfg.CredsFile))
	}

	urls := natslib.DefaultURL
	if len(cfg.URLs) > 0 {
		urls = cfg.URLs[0]
		for i := 1; i < len(cfg.URLs); i++ {
			urls += "," + cfg.URLs[i]
		}
	}

	nc, err := natslib.Connect(urls, opts...)
	if err != nil {
		return nil, fmt.Errorf("nats: connect to %q: %w", urls, err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("nats: jetstream context: %w", err)
	}

	if err := BootstrapStreams(ctx, js, logger); err != nil {
		nc.Close()
		return nil, err
	}

	return &Publisher{js: js, nc: nc, cfg: cfg, metrics: metrics, logger: logger}, nil
}

// Publish publishes a single serialized proto payload to the given subject
// and waits for JetStream to acknowledge persistence. On success it returns
// the stream sequence number. On timeout or error it returns an error that
// the caller should use to NACK/retry or reject the batch.
func (p *Publisher) Publish(ctx context.Context, subject string, payload []byte) (uint64, error) {
	start := time.Now()
	stream := subjectToStream(subject)

	timeout := p.cfg.PublishTimeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	pubCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ack, err := p.js.Publish(pubCtx, subject, payload)
	elapsed := time.Since(start).Seconds()

	if err != nil {
		p.metrics.NATSPublishDuration.With(prometheus.Labels{
			"stream": stream,
			"status": "error",
		}).Observe(elapsed)
		return 0, fmt.Errorf("nats: publish to %q: %w", subject, err)
	}

	p.metrics.NATSPublishDuration.With(prometheus.Labels{
		"stream": stream,
		"status": "ok",
	}).Observe(elapsed)

	return ack.Sequence, nil
}

// Close drains in-flight publishes and closes the NATS connection cleanly.
func (p *Publisher) Close() {
	if err := p.nc.Drain(); err != nil {
		p.logger.Warn("nats: drain error on close", slog.String("error", err.Error()))
	}
}

// subjectToStream extracts the stream name from a subject string for metrics labels.
// e.g., "events.raw.tenant.process.start" → "events_raw"
func subjectToStream(subject string) string {
	for _, sc := range RequiredStreams() {
		for _, s := range sc.Subjects {
			prefix := s
			if len(prefix) > 1 && prefix[len(prefix)-1] == '>' {
				prefix = prefix[:len(prefix)-2] // strip ".>"
			}
			if len(subject) >= len(prefix) && subject[:len(prefix)] == prefix {
				return sc.Name
			}
		}
	}
	return "unknown"
}
