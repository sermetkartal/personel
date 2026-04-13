// Package nats wraps the NATS JetStream client for the gateway and enricher.
package nats

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	natslib "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// StreamConfig defines a JetStream stream to be bootstrapped idempotently.
type StreamConfig struct {
	Name        string
	Subjects    []string
	Retention   jetstream.RetentionPolicy
	Storage     jetstream.StorageType
	Replicas    int
	MaxAge      time.Duration
	MaxMsgSize  int32
	MaxBytes    int64
	Description string
}

// RequiredStreams returns the canonical set of JetStream streams for Personel.
// All streams are created or updated idempotently at startup.
func RequiredStreams() []StreamConfig {
	return []StreamConfig{
		{
			Name:        "events_raw",
			Subjects:    []string{"events.raw.>"},
			Retention:   jetstream.LimitsPolicy,
			Storage:     jetstream.FileStorage,
			Replicas:    1,
			MaxAge:      72 * time.Hour,
			Description: "Raw EventBatch payloads from agent streams before enrichment.",
		},
		{
			Name:        "events_sensitive",
			Subjects:    []string{"events.sensitive.>"},
			Retention:   jetstream.LimitsPolicy,
			Storage:     jetstream.FileStorage,
			Replicas:    1,
			MaxAge:      72 * time.Hour,
			Description: "Events flagged as KVKK m.6 sensitive, routed to shortened-TTL storage.",
		},
		{
			Name:        "live_view_control",
			Subjects:    []string{"live_view.control.>"},
			Retention:   jetstream.WorkQueuePolicy,
			Storage:     jetstream.FileStorage,
			Replicas:    1,
			MaxAge:      1 * time.Hour,
			Description: "LiveViewStart/Stop control messages from Admin API to gateway.",
		},
		{
			Name:        "agent_health",
			Subjects:    []string{"agent.health.>"},
			Retention:   jetstream.LimitsPolicy,
			Storage:     jetstream.FileStorage,
			Replicas:    1,
			MaxAge:      48 * time.Hour,
			Description: "Agent heartbeat and health events including silence alerts.",
		},
		{
			Name:        "pki_events",
			Subjects:    []string{"pki.v1.>"},
			Retention:   jetstream.WorkQueuePolicy,
			Storage:     jetstream.FileStorage,
			Replicas:    1,
			MaxAge:      24 * time.Hour,
			Description: "PKI revocation events; consumed by gateway to refresh deny list.",
		},
		{
			// Faz 7 #74 — dead letter queue. Messages that fail
			// enrichment after MaxRetries get parked here for
			// operator inspection via /v1/pipeline/dlq. Limited
			// retention (7d) and a 10 GiB ceiling so a
			// pathological bug can't exhaust disk.
			Name:        "events_dlq",
			Subjects:    []string{"events.dlq.>"},
			Retention:   jetstream.LimitsPolicy,
			Storage:     jetstream.FileStorage,
			Replicas:    1,
			MaxAge:      7 * 24 * time.Hour,
			MaxBytes:    10 * 1024 * 1024 * 1024, // 10 GiB ceiling
			Description: "Dead-lettered event batches (Faz 7 #74). Inspect via /v1/pipeline/dlq.",
		},
	}
}

// BootstrapStreams creates or updates all required JetStream streams.
// It is idempotent: if a stream already exists with matching config it is
// a no-op; if it exists with different config it is updated.
func BootstrapStreams(ctx context.Context, js jetstream.JetStream, logger *slog.Logger) error {
	for _, sc := range RequiredStreams() {
		cfg := jetstream.StreamConfig{
			Name:        sc.Name,
			Subjects:    sc.Subjects,
			Retention:   sc.Retention,
			Storage:     sc.Storage,
			Replicas:    sc.Replicas,
			MaxAge:      sc.MaxAge,
			Description: sc.Description,
		}
		if sc.MaxMsgSize > 0 {
			cfg.MaxMsgSize = sc.MaxMsgSize
		}
		if sc.MaxBytes > 0 {
			cfg.MaxBytes = sc.MaxBytes
		}

		_, err := js.CreateOrUpdateStream(ctx, cfg)
		if err != nil {
			return fmt.Errorf("nats: bootstrap stream %q: %w", sc.Name, err)
		}
		logger.InfoContext(ctx, "nats: stream bootstrapped", slog.String("stream", sc.Name))
	}
	return nil
}

// Subject constants for use across the codebase.
const (
	// SubjectEventsRaw is the subject template for raw event batches.
	// Format: events.raw.<tenant_id>.<event_type>
	SubjectEventsRaw = "events.raw"

	// SubjectEventsSensitive is used when the batch contains sensitive-flagged events.
	SubjectEventsSensitive = "events.sensitive"

	// SubjectAgentHealth is for heartbeat and silence events.
	SubjectAgentHealth = "agent.health"

	// SubjectPKIRevoke is published by Admin API when a cert is revoked.
	SubjectPKIRevoke = "pki.v1.revoke"

	// SubjectLiveViewControl carries LiveViewStart commands from Admin API.
	SubjectLiveViewControl = "live_view.control"
)

// EventSubject returns the NATS subject for an event of a given tenant and type.
// Example: events.raw.550e8400-e29b-41d4-a716-446655440000.process.start
func EventSubject(base, tenantID, eventType string) string {
	return fmt.Sprintf("%s.%s.%s", base, tenantID, eventType)
}

// Connect establishes a NATS connection with reconnect configured from the
// provided options. Returns the conn and a JetStream context.
func Connect(opts ...natslib.Option) (*natslib.Conn, jetstream.JetStream, error) {
	nc, err := natslib.Connect(natslib.DefaultURL, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("nats: connect: %w", err)
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, nil, fmt.Errorf("nats: jetstream context: %w", err)
	}
	return nc, js, nil
}
