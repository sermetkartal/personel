package clickhouse

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/personel/gateway/internal/config"
	"github.com/personel/gateway/internal/observability"
)

// EventRow is the normalised form of an event ready to insert into ClickHouse.
// The payload field carries the proto-specific fields serialised as JSON.
type EventRow struct {
	TenantID         string
	EndpointID       string
	OccurredAt       time.Time
	EventID          string
	EventType        string
	SchemaVersion    uint8
	UserSID          string
	AgentVersionMaj  uint8
	AgentVersionMin  uint8
	AgentVersionPatch uint8
	Seq              uint64
	PIIClass         string
	RetentionClass   string
	ReceivedAt       time.Time
	Payload          string // JSON
	Sensitive        bool
	LegalHold        bool
	BatchID          uint64
}

// HeartbeatRow is the compact form for agent_heartbeats.
type HeartbeatRow struct {
	TenantID        string
	EndpointID      string
	OccurredAt      time.Time
	ReceivedAt      time.Time
	CPUPercent      float32
	RSSBytes        uint64
	QueueDepth      uint64
	BlobQueueDepth  uint64
	DropsSinceLast  uint64
	PolicyVersion   string
}

// Batcher accumulates EventRows and flushes them to ClickHouse in columnar
// batches. The flush is triggered either when the batch reaches MaxSize or
// when FlushInterval elapses.
type Batcher struct {
	conn    driver.Conn
	cfg     config.BatchConfig
	metrics *observability.Metrics
	logger  *slog.Logger

	mu       sync.Mutex
	rows     []EventRow
	hbRows   []HeartbeatRow
	lastFlush time.Time
}

// NewBatcher creates a Batcher backed by the given ClickHouse connection.
func NewBatcher(conn driver.Conn, cfg config.BatchConfig, metrics *observability.Metrics, logger *slog.Logger) *Batcher {
	if cfg.MaxSize == 0 {
		cfg.MaxSize = 10_000
	}
	if cfg.FlushInterval == 0 {
		cfg.FlushInterval = 2 * time.Second
	}
	return &Batcher{
		conn:      conn,
		cfg:       cfg,
		metrics:   metrics,
		logger:    logger,
		lastFlush: time.Now(),
	}
}

// Add queues a row for insertion. If the batch is full it flushes synchronously.
func (b *Batcher) Add(ctx context.Context, row EventRow) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.rows = append(b.rows, row)
	if len(b.rows) >= b.cfg.MaxSize {
		return b.flushLocked(ctx)
	}
	return nil
}

// AddHeartbeat queues a heartbeat row.
func (b *Batcher) AddHeartbeat(ctx context.Context, row HeartbeatRow) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.hbRows = append(b.hbRows, row)
	if len(b.hbRows) >= b.cfg.MaxSize {
		return b.flushHeartbeatsLocked(ctx)
	}
	return nil
}

// Run starts the periodic flush loop. Blocks until ctx is cancelled.
func (b *Batcher) Run(ctx context.Context) {
	ticker := time.NewTicker(b.cfg.FlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			// Final flush on shutdown.
			flushCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			b.Flush(flushCtx)
			cancel()
			return
		case <-ticker.C:
			if err := b.Flush(ctx); err != nil {
				b.logger.ErrorContext(ctx, "batcher: periodic flush failed",
					slog.String("error", err.Error()),
				)
			}
		}
	}
}

// Flush forcibly flushes any pending rows.
func (b *Batcher) Flush(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	var errs []error
	if len(b.rows) > 0 {
		if err := b.flushLocked(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if len(b.hbRows) > 0 {
		if err := b.flushHeartbeatsLocked(ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// flushLocked inserts all queued EventRows. Caller must hold b.mu.
func (b *Batcher) flushLocked(ctx context.Context) error {
	if len(b.rows) == 0 {
		return nil
	}
	rows := b.rows
	b.rows = nil
	b.lastFlush = time.Now()

	// Use separate tables for sensitive vs normal.
	normal, sensitiveWindow, sensitiveFile, sensitiveKeyMeta, sensitiveClipMeta :=
		splitByTable(rows)

	var errs []error
	if len(normal) > 0 {
		if err := b.insertEventsBatch(ctx, "events_raw", normal); err != nil {
			errs = append(errs, fmt.Errorf("events_raw: %w", err))
		}
	}
	for table, batch := range map[string][]EventRow{
		"events_sensitive_window":         sensitiveWindow,
		"events_sensitive_file":           sensitiveFile,
		"events_sensitive_keystroke_meta": sensitiveKeyMeta,
		"events_sensitive_clipboard_meta": sensitiveClipMeta,
	} {
		if len(batch) > 0 {
			if err := b.insertSensitiveBatch(ctx, table, batch); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", table, err))
			}
		}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func (b *Batcher) insertEventsBatch(ctx context.Context, table string, rows []EventRow) error {
	start := time.Now()
	batch, err := b.conn.PrepareBatch(ctx, fmt.Sprintf(
		"INSERT INTO %s (tenant_id, endpoint_id, occurred_at, event_id, event_type, schema_version, "+
			"user_sid, agent_version_major, agent_version_minor, agent_version_patch, seq, "+
			"pii_class, retention_class, received_at, payload, sensitive, legal_hold, batch_id)",
		table,
	))
	if err != nil {
		return fmt.Errorf("prepare batch: %w", err)
	}
	for _, r := range rows {
		if err := batch.Append(
			r.TenantID, r.EndpointID, r.OccurredAt, r.EventID, r.EventType,
			r.SchemaVersion, r.UserSID,
			r.AgentVersionMaj, r.AgentVersionMin, r.AgentVersionPatch,
			r.Seq, r.PIIClass, r.RetentionClass, r.ReceivedAt,
			r.Payload, r.Sensitive, r.LegalHold, r.BatchID,
		); err != nil {
			return fmt.Errorf("append row: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		b.metrics.ClickHouseInsertDuration.With(prometheus.Labels{
			"table": table, "status": "error",
		}).Observe(time.Since(start).Seconds())
		return fmt.Errorf("send batch: %w", err)
	}
	b.metrics.ClickHouseInsertDuration.With(prometheus.Labels{
		"table": table, "status": "ok",
	}).Observe(time.Since(start).Seconds())
	b.logger.InfoContext(ctx, "batcher: flushed",
		slog.String("table", table),
		slog.Int("rows", len(rows)),
		slog.Duration("elapsed", time.Since(start)),
	)
	return nil
}

func (b *Batcher) insertSensitiveBatch(ctx context.Context, table string, rows []EventRow) error {
	start := time.Now()
	batch, err := b.conn.PrepareBatch(ctx, fmt.Sprintf(
		"INSERT INTO %s (tenant_id, endpoint_id, occurred_at, event_id, user_sid, seq, received_at, payload, legal_hold)",
		table,
	))
	if err != nil {
		return fmt.Errorf("prepare sensitive batch: %w", err)
	}
	for _, r := range rows {
		if err := batch.Append(
			r.TenantID, r.EndpointID, r.OccurredAt,
			r.EventID, r.UserSID, r.Seq, r.ReceivedAt,
			r.Payload, r.LegalHold,
		); err != nil {
			return fmt.Errorf("append sensitive row: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		b.metrics.ClickHouseInsertDuration.With(prometheus.Labels{
			"table": table, "status": "error",
		}).Observe(time.Since(start).Seconds())
		return fmt.Errorf("send sensitive batch: %w", err)
	}
	b.metrics.ClickHouseInsertDuration.With(prometheus.Labels{
		"table": table, "status": "ok",
	}).Observe(time.Since(start).Seconds())
	return nil
}

func (b *Batcher) flushHeartbeatsLocked(ctx context.Context) error {
	if len(b.hbRows) == 0 {
		return nil
	}
	rows := b.hbRows
	b.hbRows = nil
	start := time.Now()

	batch, err := b.conn.PrepareBatch(ctx,
		"INSERT INTO agent_heartbeats (tenant_id, endpoint_id, occurred_at, received_at, "+
			"cpu_percent, rss_bytes, queue_depth, blob_queue_depth, drops_since_last, policy_version)",
	)
	if err != nil {
		return fmt.Errorf("prepare heartbeat batch: %w", err)
	}
	for _, r := range rows {
		if err := batch.Append(
			r.TenantID, r.EndpointID, r.OccurredAt, r.ReceivedAt,
			r.CPUPercent, r.RSSBytes, r.QueueDepth, r.BlobQueueDepth,
			r.DropsSinceLast, r.PolicyVersion,
		); err != nil {
			return fmt.Errorf("append heartbeat row: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send heartbeat batch: %w", err)
	}
	b.logger.DebugContext(ctx, "batcher: heartbeats flushed",
		slog.Int("rows", len(rows)),
		slog.Duration("elapsed", time.Since(start)),
	)
	return nil
}

// splitByTable partitions rows into their target tables based on event_type
// and the sensitive flag, following the retention matrix.
func splitByTable(rows []EventRow) (normal, sensWindow, sensFile, sensKeyMeta, sensClipMeta []EventRow) {
	for _, r := range rows {
		if !r.Sensitive {
			normal = append(normal, r)
			continue
		}
		switch r.EventType {
		case "window.title_changed", "window.focus_lost":
			sensWindow = append(sensWindow, r)
		case "file.created", "file.written", "file.deleted", "file.renamed", "file.read", "file.copied":
			sensFile = append(sensFile, r)
		case "keystroke.window_stats":
			sensKeyMeta = append(sensKeyMeta, r)
		case "clipboard.metadata":
			sensClipMeta = append(sensClipMeta, r)
		default:
			normal = append(normal, r)
		}
	}
	return
}

// PayloadToJSON serialises an arbitrary value to a compact JSON string.
func PayloadToJSON(v interface{}) string {
	if v == nil {
		return "{}"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}
