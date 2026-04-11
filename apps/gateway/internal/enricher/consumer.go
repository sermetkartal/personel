// Package enricher implements the NATS JetStream consumer that reads raw event
// batches, enriches them with tenant/endpoint metadata, applies sensitivity
// classification, and routes them to ClickHouse or MinIO.
package enricher

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"

	"github.com/personel/gateway/internal/clickhouse"
	"github.com/personel/gateway/internal/observability"
	personelv1 "github.com/personel/proto/personel/v1"
)

// Consumer pulls EventBatch messages from NATS JetStream, enriches each batch,
// and writes to ClickHouse. It uses explicit manual ACK (at-least-once delivery).
type Consumer struct {
	js       jetstream.JetStream
	batcher  *clickhouse.Batcher
	enricher *Enricher
	router   *Router
	metrics  *observability.Metrics
	logger   *slog.Logger
}

// ConsumerConfig configures the NATS consumer.
type ConsumerConfig struct {
	// Durable is the JetStream durable consumer name.
	Durable string
	// FilterSubject selects which subjects this consumer sees.
	FilterSubject string
	// Concurrency is the number of parallel batch processors.
	Concurrency int
	// FetchBatchSize is the maximum messages to fetch in one call.
	FetchBatchSize int
	// FetchMaxWait is the timeout for a fetch call when no messages are available.
	FetchMaxWait time.Duration
}

// DefaultConsumerConfig returns sensible defaults for the enricher consumer.
func DefaultConsumerConfig() ConsumerConfig {
	return ConsumerConfig{
		Durable:        "enricher-v1",
		FilterSubject:  "events.raw.>",
		Concurrency:    4,
		FetchBatchSize: 100,
		FetchMaxWait:   500 * time.Millisecond,
	}
}

// NewConsumer creates a Consumer. The js must be a ready-to-use JetStream context.
func NewConsumer(
	js jetstream.JetStream,
	batcher *clickhouse.Batcher,
	enricher *Enricher,
	router *Router,
	metrics *observability.Metrics,
	logger *slog.Logger,
) *Consumer {
	return &Consumer{
		js:       js,
		batcher:  batcher,
		enricher: enricher,
		router:   router,
		metrics:  metrics,
		logger:   logger,
	}
}

// Run starts the consumer loop. Blocks until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context, cfg ConsumerConfig) error {
	if cfg.Concurrency == 0 {
		cfg.Concurrency = 4
	}
	if cfg.FetchBatchSize == 0 {
		cfg.FetchBatchSize = 100
	}
	if cfg.FetchMaxWait == 0 {
		cfg.FetchMaxWait = 500 * time.Millisecond
	}

	cons, err := c.js.CreateOrUpdateConsumer(ctx, "events_raw", jetstream.ConsumerConfig{
		Durable:        cfg.Durable,
		FilterSubject:  cfg.FilterSubject,
		AckPolicy:      jetstream.AckExplicitPolicy,
		DeliverPolicy:  jetstream.DeliverAllPolicy,
		MaxAckPending:  cfg.Concurrency * cfg.FetchBatchSize,
		AckWait:        60 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("enricher consumer: create consumer: %w", err)
	}

	c.logger.InfoContext(ctx, "enricher consumer: started",
		slog.String("durable", cfg.Durable),
		slog.Int("concurrency", cfg.Concurrency),
	)

	// Semaphore to limit concurrent processors.
	sem := make(chan struct{}, cfg.Concurrency)

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		msgs, err := cons.Fetch(cfg.FetchBatchSize, jetstream.FetchMaxWait(cfg.FetchMaxWait))
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			c.logger.WarnContext(ctx, "enricher consumer: fetch error",
				slog.String("error", err.Error()),
			)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		for msg := range msgs.Messages() {
			msg := msg // capture for goroutine
			sem <- struct{}{}
			go func() {
				defer func() { <-sem }()
				c.processMessage(ctx, msg)
			}()
		}
		if msgs.Error() != nil {
			c.logger.WarnContext(ctx, "enricher consumer: batch fetch error",
				slog.String("error", msgs.Error().Error()),
			)
		}

		// Update lag metric.
		if info, err := cons.Info(ctx); err == nil {
			c.metrics.EnricherLag.Set(float64(info.NumPending))
		}
	}
}

// processMessage deserialises an EventBatch, enriches each event, and writes
// to the appropriate sink. On success it ACKs the message; on error it NAKs
// with a delay (JetStream redelivery).
func (c *Consumer) processMessage(ctx context.Context, msg jetstream.Msg) {
	var batch personelv1.EventBatch
	if err := proto.Unmarshal(msg.Data(), &batch); err != nil {
		c.logger.ErrorContext(ctx, "enricher: unmarshal EventBatch failed",
			slog.String("error", err.Error()),
		)
		// Poison message — term to avoid infinite redelivery.
		_ = msg.Term()
		return
	}

	if err := c.processBatch(ctx, &batch); err != nil {
		c.logger.ErrorContext(ctx, "enricher: process batch failed",
			slog.String("error", err.Error()),
		)
		_ = msg.NakWithDelay(5 * time.Second)
		return
	}

	_ = msg.Ack()
}

// processBatch enriches and routes all events in a batch.
func (c *Consumer) processBatch(ctx context.Context, batch *personelv1.EventBatch) error {
	for _, ev := range batch.GetEvents() {
		enriched, err := c.enricher.Enrich(ctx, ev)
		if err != nil {
			return fmt.Errorf("enrich event %q: %w", ev.GetMeta().GetEventId(), err)
		}

		// Apply sensitivity guard.
		ApplySensitivity(enriched, c.enricher.policy(ctx, enriched.TenantID))

		// Route to correct sink.
		destination := c.router.Route(enriched)

		switch destination.Sink {
		case SinkClickHouse:
			if err := c.batcher.Add(ctx, eventToRow(enriched, batch.GetBatchId())); err != nil {
				return fmt.Errorf("clickhouse add: %w", err)
			}
		case SinkClickHouseHeartbeat:
			row := heartbeatToRow(ev, enriched)
			if err := c.batcher.AddHeartbeat(ctx, row); err != nil {
				return fmt.Errorf("clickhouse heartbeat add: %w", err)
			}
		case SinkDrop:
			c.logger.DebugContext(ctx, "enricher: event dropped (purge retention)",
				slog.String("event_type", ev.GetMeta().GetEventType()),
			)
		}
	}
	return nil
}

// eventToRow converts an EnrichedEvent to a ClickHouse EventRow.
func eventToRow(e *EnrichedEvent, batchID uint64) clickhouse.EventRow {
	meta := e.Event.GetMeta()
	var occAt time.Time
	if meta.GetOccurredAt() != nil {
		occAt = meta.GetOccurredAt().AsTime()
	}
	var recAt time.Time
	if meta.GetReceivedAt() != nil {
		recAt = meta.GetReceivedAt().AsTime()
	}
	return clickhouse.EventRow{
		TenantID:          e.TenantID,
		EndpointID:        e.EndpointID,
		OccurredAt:        occAt,
		EventID:           byteSliceToHex(meta.GetEventId().GetValue()),
		EventType:         meta.GetEventType(),
		SchemaVersion:     uint8(meta.GetSchemaVersion()),
		UserSID:           meta.GetUserSid().GetValue(),
		AgentVersionMaj:   uint8(meta.GetAgentVersion().GetMajor()),
		AgentVersionMin:   uint8(meta.GetAgentVersion().GetMinor()),
		AgentVersionPatch: uint8(meta.GetAgentVersion().GetPatch()),
		Seq:               meta.GetSeq(),
		PIIClass:          meta.GetPii().String(),
		RetentionClass:    meta.GetRetention().String(),
		ReceivedAt:        recAt,
		Payload:           clickhouse.PayloadToJSON(e.PayloadJSON),
		Sensitive:         e.Sensitive,
		LegalHold:         false,
		BatchID:           batchID,
	}
}

// heartbeatToRow converts an AgentHealthHeartbeat event to a HeartbeatRow.
func heartbeatToRow(ev *personelv1.Event, enriched *EnrichedEvent) clickhouse.HeartbeatRow {
	hb, ok := ev.Payload.(*personelv1.Event_AgentHealthHeartbeat)
	if !ok {
		return clickhouse.HeartbeatRow{}
	}
	meta := ev.GetMeta()
	var occAt time.Time
	if meta.GetOccurredAt() != nil {
		occAt = meta.GetOccurredAt().AsTime()
	}
	var recAt time.Time
	if meta.GetReceivedAt() != nil {
		recAt = meta.GetReceivedAt().AsTime()
	}
	return clickhouse.HeartbeatRow{
		TenantID:        enriched.TenantID,
		EndpointID:      enriched.EndpointID,
		OccurredAt:      occAt,
		ReceivedAt:      recAt,
		CPUPercent:      float32(hb.AgentHealthHeartbeat.GetCpuPercent()),
		RSSBytes:        hb.AgentHealthHeartbeat.GetRssBytes(),
		QueueDepth:      hb.AgentHealthHeartbeat.GetQueueDepth(),
		BlobQueueDepth:  hb.AgentHealthHeartbeat.GetBlobQueueDepth(),
		DropsSinceLast:  hb.AgentHealthHeartbeat.GetDropsSinceLast(),
		PolicyVersion:   hb.AgentHealthHeartbeat.GetPolicyVersion(),
	}
}

// byteSliceToHex converts a byte slice to a lowercase hex string.
func byteSliceToHex(b []byte) string {
	const hexChars = "0123456789abcdef"
	result := make([]byte, len(b)*2)
	for i, c := range b {
		result[i*2] = hexChars[c>>4]
		result[i*2+1] = hexChars[c&0x0f]
	}
	return string(result)
}
