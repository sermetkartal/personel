// Package enricher — event deduplication cache.
//
// Faz 7 / Roadmap item #78.
//
// The Deduper keeps an in-memory set of recently-seen event hashes and
// tells the enricher whether a given event has been seen within the
// configured TTL window. It is intended to absorb at-least-once
// redelivery from NATS JetStream (crash/NAK redelivery) without
// doing a ClickHouse round-trip on the hot path.
//
// Design choices:
//
//   - Canonical hash: SHA-256 over `agent_id || ts_ns || event_kind ||
//     payload`. For KeystrokeContentEncrypted events we deliberately
//     EXCLUDE the payload from the hash input — hashing an admin-
//     opaque keystroke ciphertext would leak information via the
//     dedup cache's timing, and per KVKK m.6 keystroke content must
//     never be observed outside the DLP isolation boundary. For
//     those events we fall back to `agent_id || ts_ns || event_kind`
//     which is deterministic enough given a monotonic agent seq.
//
//   - Eviction: two policies combined. A bounded map (default 100k)
//     evicts in FIFO order once full; a periodic sweeper drops
//     entries older than TTL (default 5m). The FIFO approximation is
//     good enough here — strict LRU would double the memory footprint
//     per entry and the access pattern is dominated by the sweep TTL
//     anyway.
//
//   - Concurrency: one mutex. Hot path is a single map lookup + insert
//     under the lock, O(1) amortised.
//
//   - Prometheus metrics: `personel_enricher_dedup_hits_total`,
//     `personel_enricher_dedup_misses_total` (no labels — tenant-level
//     granularity is available via EventsReceived in the DQM).
package enricher

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// keystrokeContentEventKind is the EventMeta.event_type value for
// KeystrokeContentEncrypted. It is duplicated as a const here so the
// dedup package does not need to import the full proto. The canonical
// dotted form matches what agents emit on the wire (see
// apps/gateway/test/load/synthetic_test.go and the agent collectors).
const keystrokeContentEventKind = "keystroke.content_encrypted"

// Deduper is a bounded TTL-aware set of recently-seen event hashes.
type Deduper struct {
	mu        sync.Mutex
	seen      map[[32]byte]time.Time
	order     [][32]byte // FIFO eviction order; len == len(seen)
	capacity  int
	ttl       time.Duration
	stopCh    chan struct{}
	closeOnce sync.Once

	hits   prometheus.Counter
	misses prometheus.Counter
}

// NewDeduper creates a Deduper with the given capacity and TTL.
//
// capacity == 0 ⇒ default 100_000.
// ttl == 0 ⇒ default 5 * time.Minute.
//
// The returned Deduper starts a background sweeper goroutine; call
// Close() to stop it.
func NewDeduper(capacity int, ttl time.Duration, reg prometheus.Registerer) *Deduper {
	if capacity <= 0 {
		capacity = 100_000
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	factory := promauto.With(reg)
	d := &Deduper{
		seen:     make(map[[32]byte]time.Time, capacity),
		order:    make([][32]byte, 0, capacity),
		capacity: capacity,
		ttl:      ttl,
		stopCh:   make(chan struct{}),
		hits: factory.NewCounter(prometheus.CounterOpts{
			Namespace: "personel_enricher",
			Name:      "dedup_hits_total",
			Help:      "Events skipped because their hash was already in the dedup cache.",
		}),
		misses: factory.NewCounter(prometheus.CounterOpts{
			Namespace: "personel_enricher",
			Name:      "dedup_misses_total",
			Help:      "Events not present in the dedup cache (first-time arrivals).",
		}),
	}
	go d.sweepLoop()
	return d
}

// Seen returns true if this (agentID, tsNs, eventKind, payload) tuple has
// been observed within the TTL window. On a miss, the tuple is recorded
// so subsequent calls will hit.
//
// For keystroke content events, the payload is intentionally excluded
// from the hash input per KVKK m.6 — see package doc.
func (d *Deduper) Seen(_ context.Context, agentID, eventKind string, tsNs int64, payload []byte) bool {
	key := d.canonicalHash(agentID, eventKind, tsNs, payload)

	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()

	if seenAt, ok := d.seen[key]; ok {
		if now.Sub(seenAt) <= d.ttl {
			d.hits.Inc()
			return true
		}
		// stale — fall through and re-insert below
	}

	// Evict FIFO if at capacity.
	if len(d.seen) >= d.capacity && len(d.order) > 0 {
		victim := d.order[0]
		d.order = d.order[1:]
		delete(d.seen, victim)
	}

	d.seen[key] = now
	d.order = append(d.order, key)
	d.misses.Inc()
	return false
}

// Close stops the background sweeper. Safe to call multiple times.
func (d *Deduper) Close() {
	d.closeOnce.Do(func() { close(d.stopCh) })
}

// canonicalHash produces a deterministic 32-byte key for (agent, event,
// ts, payload). For keystroke content events we drop the payload.
func (d *Deduper) canonicalHash(agentID, eventKind string, tsNs int64, payload []byte) [32]byte {
	h := sha256.New()
	// Length-prefix each field so the hash is unambiguous.
	writeLenPrefixed(h, []byte(agentID))
	writeLenPrefixed(h, []byte(eventKind))
	var tsBuf [8]byte
	binary.BigEndian.PutUint64(tsBuf[:], uint64(tsNs))
	h.Write(tsBuf[:])
	if eventKind != keystrokeContentEventKind {
		writeLenPrefixed(h, payload)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// writeLenPrefixed emits BigEndian u32 length followed by the bytes, so
// two different splits of the same concatenation produce different
// hashes.
func writeLenPrefixed(h hash.Hash, b []byte) {
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(b)))
	_, _ = h.Write(lenBuf[:])
	_, _ = h.Write(b)
}

// sweepLoop periodically drops entries older than TTL. Runs every
// ttl/2 so the worst-case staleness is 1.5 * TTL.
func (d *Deduper) sweepLoop() {
	interval := d.ttl / 2
	if interval < time.Second {
		interval = time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-d.stopCh:
			return
		case now := <-t.C:
			d.sweep(now)
		}
	}
}

// sweep removes entries older than TTL. O(n) in the number of live
// entries — acceptable since it runs once every TTL/2.
func (d *Deduper) sweep(now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	cutoff := now.Add(-d.ttl)
	// Walk order slice; anything before cutoff is expired. Because the
	// slice is append-only FIFO, expired entries cluster at the head.
	i := 0
	for i < len(d.order) {
		k := d.order[i]
		ts, ok := d.seen[k]
		if !ok {
			i++
			continue
		}
		if ts.Before(cutoff) {
			delete(d.seen, k)
			i++
			continue
		}
		break
	}
	if i > 0 {
		d.order = d.order[i:]
	}
}

// Len returns the current number of live entries. Test helper.
func (d *Deduper) Len() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.seen)
}
