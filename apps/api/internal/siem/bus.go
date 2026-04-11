package siem

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
)

// Bus is the in-process fan-out layer that dispatches events to all
// registered exporters. One Bus per api process; multiple Exporters
// per Bus (one per tenant × SIEM combination).
//
// Design:
//   - Non-blocking: Publish returns immediately after enqueueing to a
//     bounded per-exporter buffer. If the buffer is full, the event
//     is DROPPED and the drops counter is incremented.
//   - Dropped events are logged at Warn level (not Error) because the
//     SIEM is a secondary observability target; the primary audit
//     trail is the Postgres chain + WORM sink.
//   - Shutdown: Stop flushes in-flight events with a deadline and
//     signals all worker goroutines to exit.
type Bus struct {
	exporters []*busWorker
	log       *slog.Logger
}

type busWorker struct {
	exporter Exporter
	queue    chan Event
	wg       sync.WaitGroup

	publishedTotal atomic.Uint64
	droppedTotal   atomic.Uint64
	errorTotal     atomic.Uint64
}

// NewBus constructs a Bus with an empty exporter list.
func NewBus(log *slog.Logger) *Bus {
	return &Bus{log: log}
}

// AddExporter registers an exporter with the bus and starts its worker
// goroutine. bufferSize is the per-exporter buffer capacity; typical
// value 10000 events.
func (b *Bus) AddExporter(ctx context.Context, exporter Exporter, bufferSize int) {
	w := &busWorker{
		exporter: exporter,
		queue:    make(chan Event, bufferSize),
	}
	w.wg.Add(1)
	go b.runWorker(ctx, w)
	b.exporters = append(b.exporters, w)

	b.log.InfoContext(ctx, "siem exporter started",
		slog.String("exporter", exporter.Name()),
		slog.Int("buffer_size", bufferSize),
	)
}

// Publish fans out an event to all registered exporters. Non-blocking.
// Each exporter gets its own copy — if one is backed up, others still
// receive the event.
func (b *Bus) Publish(event Event) {
	for _, w := range b.exporters {
		select {
		case w.queue <- event:
			// Enqueued; worker will publish asynchronously.
		default:
			// Buffer full; drop this event for this exporter.
			w.droppedTotal.Add(1)
			b.log.Warn("siem event dropped (buffer full)",
				slog.String("exporter", w.exporter.Name()),
				slog.String("event_type", string(event.Type)),
				slog.String("event_id", event.ID),
			)
		}
	}
}

// Stats returns a snapshot of per-exporter counters. Used by /metrics.
func (b *Bus) Stats() []ExporterStats {
	out := make([]ExporterStats, 0, len(b.exporters))
	for _, w := range b.exporters {
		out = append(out, ExporterStats{
			Name:           w.exporter.Name(),
			PublishedTotal: w.publishedTotal.Load(),
			DroppedTotal:   w.droppedTotal.Load(),
			ErrorTotal:     w.errorTotal.Load(),
			QueueDepth:     len(w.queue),
			QueueCapacity:  cap(w.queue),
		})
	}
	return out
}

// ExporterStats is a snapshot of one exporter's counters.
type ExporterStats struct {
	Name           string
	PublishedTotal uint64
	DroppedTotal   uint64
	ErrorTotal     uint64
	QueueDepth     int
	QueueCapacity  int
}

// Stop shuts down all exporter workers. Blocks until all buffered
// events have been sent or the context deadline is reached.
func (b *Bus) Stop() {
	for _, w := range b.exporters {
		close(w.queue)
	}
	for _, w := range b.exporters {
		w.wg.Wait()
	}
}

func (b *Bus) runWorker(ctx context.Context, w *busWorker) {
	defer w.wg.Done()

	// Batch buffer for adapters that support PublishBatch.
	const maxBatch = 100
	batch := make([]Event, 0, maxBatch)
	capabilities := w.exporter.Capabilities()

	for {
		select {
		case event, ok := <-w.queue:
			if !ok {
				// Queue closed — flush any remaining batch and exit.
				if len(batch) > 0 {
					b.flushBatch(ctx, w, batch, capabilities)
				}
				return
			}
			batch = append(batch, event)
			if len(batch) >= maxBatch {
				b.flushBatch(ctx, w, batch, capabilities)
				batch = batch[:0]
			}
		case <-ctx.Done():
			if len(batch) > 0 {
				b.flushBatch(ctx, w, batch, capabilities)
			}
			return
		}
	}
}

func (b *Bus) flushBatch(ctx context.Context, w *busWorker, batch []Event, cap Capabilities) {
	if cap.SupportsBatch && len(batch) > 1 {
		if err := w.exporter.PublishBatch(ctx, batch); err != nil {
			w.errorTotal.Add(uint64(len(batch)))
			b.log.Warn("siem batch publish failed",
				slog.String("exporter", w.exporter.Name()),
				slog.Int("batch_size", len(batch)),
				slog.String("error", err.Error()),
			)
			return
		}
		w.publishedTotal.Add(uint64(len(batch)))
		return
	}

	// Fall back to per-event publish.
	for _, event := range batch {
		if err := w.exporter.Publish(ctx, event); err != nil {
			w.errorTotal.Add(1)
			b.log.Warn("siem publish failed",
				slog.String("exporter", w.exporter.Name()),
				slog.String("event_id", event.ID),
				slog.String("error", err.Error()),
			)
			continue
		}
		w.publishedTotal.Add(1)
	}
}
