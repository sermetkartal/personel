// Package enricher implements the NATS JetStream consumer that reads raw event
// batches, enriches them with tenant/endpoint metadata, applies sensitivity
// classification, and routes them to ClickHouse or MinIO.
package enricher

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"

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

	// dedup is optional. When non-nil, events whose (agent_id, ts_ns,
	// event_kind, payload) tuple has been seen within the dedup TTL are
	// skipped before enrichment + ClickHouse write. Wired via SetDeduper
	// from cmd/enricher/main.go. See dedup.go (Faz 7 item #78).
	dedup *Deduper

	// dqm is optional. When non-nil, per-tenant DQM counters are updated
	// on receive / decode / drop. Wired via SetDQM from main.go.
	// See dqm.go (Faz 7 item #80).
	dqm *DQM

	// decoder routes raw NATS payloads to the correct schema Decoder
	// based on the "schema-version" header. Faz 7 #73. Wired via
	// SetDecoder from main.go. Nil-guard defaults to NewDefaultDecoder().
	decoder *VersionedDecoder

	// dlq publishes irrecoverable failures to the events_dlq stream.
	// Faz 7 #74. When nil, the consumer falls back to msg.Term() for
	// decode errors and msg.NakWithDelay() for everything else — the
	// legacy behaviour.
	dlq *DLQPublisher
}

// SetDeduper attaches a deduplication cache to the consumer. Call before
// Run(). Passing nil disables dedup (the default).
func (c *Consumer) SetDeduper(d *Deduper) {
	c.dedup = d
}

// SetDQM attaches a data-quality monitor to the consumer. Call before
// Run(). Passing nil disables DQM tracking (the default).
func (c *Consumer) SetDQM(dqm *DQM) {
	c.dqm = dqm
}

// SetDecoder attaches a VersionedDecoder. Call before Run(). If never
// called, processMessage falls back to NewDefaultDecoder() at first use.
// Exposed as a setter so cmd/enricher/main.go can override the registry
// without changing the NewConsumer signature (backward-compat with
// every existing test that constructs a Consumer).
func (c *Consumer) SetDecoder(vd *VersionedDecoder) {
	c.decoder = vd
}

// SetDLQ attaches a DLQPublisher. Call before Run(). When nil, the
// consumer falls back to the legacy NAK/Term behaviour (which was
// causing infinite redelivery on decode errors prior to Faz 7 #74).
func (c *Consumer) SetDLQ(d *DLQPublisher) {
	c.dlq = d
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
// to the appropriate sink. On success it ACKs the message; on retriable
// error it NAKs with a delay (JetStream redelivery); after MaxRetries
// retries OR on any decode error, it parks the message in the DLQ and
// ACKs the original so redelivery stops.
func (c *Consumer) processMessage(ctx context.Context, msg jetstream.Msg) {
	// Lazy-init the decoder so existing tests that construct a Consumer
	// without calling SetDecoder keep working.
	if c.decoder == nil {
		c.decoder = NewDefaultDecoder()
	}

	// --- Schema dispatch (Faz 7 #73) ---
	batch, schemaVersion, err := c.decoder.Dispatch(msg.Headers(), msg.Data())
	if err != nil {
		// Unknown schema version is still classified as "decode" —
		// the operator-facing distinction is the ErrorMessage, not
		// a separate kind. Narrow the enum so DLQ filters stay short.
		c.logger.ErrorContext(ctx, "enricher: decode failed",
			slog.String("error", err.Error()),
			slog.String("schema_version", schemaVersion),
			slog.String("subject", msg.Subject()),
		)
		c.sendToDLQ(ctx, msg, DLQKindDecode, err.Error(), "", 0)
		_ = msg.Ack()
		return
	}

	// --- Enrichment + sink write (Faz 7 #74) ---
	if err := c.processBatch(ctx, batch); err != nil {
		retry := RetryCountOf(msg.Headers())
		maxRetries := 3
		if c.dlq != nil {
			maxRetries = c.dlq.MaxRetries()
		}

		if retry >= maxRetries {
			c.logger.ErrorContext(ctx, "enricher: retry budget exhausted, routing to DLQ",
				slog.String("error", err.Error()),
				slog.Int("retry_count", retry),
				slog.Int("max_retries", maxRetries),
			)
			kind := classifyProcessError(err)
			tenantHint := batchTenantHint(batch)
			c.sendToDLQ(ctx, msg, kind, err.Error(), tenantHint, batch.GetBatchId())
			_ = msg.Ack()
			return
		}

		c.logger.WarnContext(ctx, "enricher: process batch failed; NAK with delay",
			slog.String("error", err.Error()),
			slog.Int("retry_count", retry),
		)
		_ = msg.NakWithDelay(5 * time.Second)
		return
	}

	_ = msg.Ack()
}

// sendToDLQ publishes the failing message to events_dlq. Nil-guard so
// legacy test harnesses without a DLQ wired can still run.
func (c *Consumer) sendToDLQ(ctx context.Context, msg jetstream.Msg, kind, errMsg, tenantID string, batchID uint64) {
	if c.dlq == nil {
		return
	}
	dm := BuildDLQMessage(msg, kind, errMsg, tenantID, batchID)
	if _, err := c.dlq.Publish(ctx, dm); err != nil {
		c.logger.ErrorContext(ctx, "enricher: DLQ publish failed; event is lost",
			slog.String("error", err.Error()),
			slog.String("kind", kind),
		)
	}
}

// classifyProcessError maps an error from processBatch to a DLQ kind.
// String matching is deliberately narrow — categories are visible to
// operators filtering /v1/pipeline/dlq so false positives are worse
// than unknown-labelled entries.
func classifyProcessError(err error) string {
	if err == nil {
		return DLQKindRetryExhausted
	}
	s := err.Error()
	switch {
	case strings.Contains(s, "clickhouse"):
		return DLQKindClickHouse
	case strings.Contains(s, "minio"):
		return DLQKindMinIO
	case strings.Contains(s, "enrich"):
		return DLQKindEnrich
	default:
		return DLQKindRetryExhausted
	}
}

// batchTenantHint extracts a best-effort tenant_id from the first
// event of a decoded batch. Returns "" when the batch is empty.
func batchTenantHint(b *personelv1.EventBatch) string {
	if b == nil {
		return ""
	}
	for _, ev := range b.GetEvents() {
		if t := byteSliceToUUID(ev.GetMeta().GetTenantId().GetValue()); t != "" {
			return t
		}
	}
	return ""
}

// processBatch enriches and routes all events in a batch.
func (c *Consumer) processBatch(ctx context.Context, batch *personelv1.EventBatch) error {
	for _, ev := range batch.GetEvents() {
		meta := ev.GetMeta()
		tenantHint := byteSliceToUUID(meta.GetTenantId().GetValue())

		// DQM: record every event seen on the wire.
		if c.dqm != nil {
			c.dqm.RecordReceived(tenantHint)
		}

		// Dedup check — before any Postgres enrichment to save the cost
		// on duplicate deliveries. Hash input uses endpoint_id as the
		// agent identity (the Personel mTLS identity one-to-one maps
		// agent ↔ endpoint_id).
		if c.dedup != nil {
			endpointHint := byteSliceToUUID(meta.GetEndpointId().GetValue())
			var tsNs int64
			if meta.GetOccurredAt() != nil {
				tsNs = meta.GetOccurredAt().AsTime().UnixNano()
			}
			payloadRef := extractPayloadForDedup(ev)
			if c.dedup.Seen(ctx, endpointHint, meta.GetEventType(), tsNs, payloadRef) {
				if c.dqm != nil {
					c.dqm.RecordDropped(tenantHint, "duplicate")
				}
				continue
			}
		}

		decodeStart := time.Now()
		enriched, err := c.enricher.Enrich(ctx, ev)
		if err != nil {
			if c.dqm != nil {
				c.dqm.RecordDropped(tenantHint, "enrich_error")
			}
			return fmt.Errorf("enrich event %q: %w", meta.GetEventId(), err)
		}
		if c.dqm != nil {
			c.dqm.RecordDecoded(tenantHint, float64(time.Since(decodeStart).Milliseconds()))
		}

		// Apply sensitivity guard.
		ApplySensitivity(enriched, c.enricher.policy(ctx, enriched.TenantID))

		// Route to correct sink.
		destination := c.router.Route(enriched)

		enrichStart := time.Now()
		switch destination.Sink {
		case SinkClickHouse:
			if err := c.batcher.Add(ctx, eventToRow(enriched, batch.GetBatchId())); err != nil {
				if c.dqm != nil {
					c.dqm.RecordDropped(enriched.TenantID, "clickhouse_add_error")
				}
				return fmt.Errorf("clickhouse add: %w", err)
			}
		case SinkClickHouseHeartbeat:
			row := heartbeatToRow(ev, enriched)
			if err := c.batcher.AddHeartbeat(ctx, row); err != nil {
				if c.dqm != nil {
					c.dqm.RecordDropped(enriched.TenantID, "clickhouse_heartbeat_error")
				}
				return fmt.Errorf("clickhouse heartbeat add: %w", err)
			}
		case SinkDrop:
			if c.dqm != nil {
				c.dqm.RecordDropped(enriched.TenantID, "purge_retention")
			}
			c.logger.DebugContext(ctx, "enricher: event dropped (purge retention)",
				slog.String("event_type", meta.GetEventType()),
			)
		}
		if c.dqm != nil {
			c.dqm.RecordEnriched(enriched.TenantID, float64(time.Since(enrichStart).Milliseconds()))
		}
	}
	return nil
}

// extractPayloadForDedup returns a small byte slice derived from the
// event's payload oneof for hashing purposes. We cannot cheaply
// marshal the whole proto here (would be O(N) on every event), so we
// use the event_id — which the agent generates as a ULID over
// (ts, random) and is therefore a strong-enough uniqueness signal
// for dedup purposes. If event_id is empty, we fall back to nil.
func extractPayloadForDedup(ev *personelv1.Event) []byte {
	if ev == nil || ev.GetMeta() == nil {
		return nil
	}
	return ev.GetMeta().GetEventId().GetValue()
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
