// Package audit — stream.go provides an in-process publish/subscribe Broker
// that fans out audit.Entry values to live WebSocket subscribers AFTER the
// Postgres INSERT has committed. This backs GET /v1/audit/stream (Faz 6 #66).
//
// Design constraints:
//
//   - Best-effort fanout. A slow or stuck subscriber MUST NOT block the audit
//     hot path — the Recorder.Append success path is authoritative and cannot
//     be delayed for in-flight WebSocket clients. Publish uses a non-blocking
//     send; if the subscriber buffer is full the entry is dropped for that
//     subscriber and a drop counter is incremented (observability surface).
//
//   - Tenant isolation is enforced at publish time. A subscriber bound to
//     tenant T can only see entries whose Entry.TenantID matches T. DPO
//     subscribers MAY pass AllTenants=true to bypass the filter; the audit
//     trail of the subscription itself captures this elevation.
//
//   - KVKK invariant: payloads streamed over the wire must NOT include
//     keystroke content. See StripSensitive below. The broker strips details
//     keys that match the keystroke_content pattern before every send.
//
//   - This broker is authoritative for in-process real-time delivery only.
//     Historical replay uses the SearchService (Faz 6 #67) against OpenSearch;
//     subscribers reconnecting after a gap MUST rely on search for backfill.
package audit

import (
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
)

// StreamFilter narrows which entries a Subscriber receives. All three fields
// are ANDed together — an entry must satisfy every non-empty filter.
// Empty slices / empty strings mean "no filter on this dimension".
type StreamFilter struct {
	// Actions is an allowlist of action strings (e.g. "policy.pushed",
	// "endpoint.wipe"). Matched case-sensitively against Entry.Action.
	Actions []string

	// ActorIDs is an allowlist of actor user IDs (Keycloak sub).
	ActorIDs []string

	// TargetPrefix filters entries whose Target begins with this prefix,
	// e.g. "endpoint." to receive only endpoint-scoped events.
	TargetPrefix string
}

// match reports whether the entry satisfies every filter dimension.
func (f StreamFilter) match(e Entry) bool {
	if len(f.Actions) > 0 {
		ok := false
		for _, a := range f.Actions {
			if a == string(e.Action) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(f.ActorIDs) > 0 {
		ok := false
		for _, a := range f.ActorIDs {
			if a == e.Actor {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if f.TargetPrefix != "" && !strings.HasPrefix(e.Target, f.TargetPrefix) {
		return false
	}
	return true
}

// Subscriber is a single live WebSocket consumer. The broker owns the
// lifecycle of ch — subscribers MUST NOT close it. The consumer goroutine
// drains ch until it receives the zero value (closed) or its own ctx ends
// and the handler calls Broker.Unsubscribe.
type Subscriber struct {
	// TenantID is the Keycloak tenant of the subscribing principal. Entries
	// for other tenants are filtered out at publish time unless AllTenants
	// is set (DPO only).
	TenantID string

	// AllTenants bypasses the per-tenant filter. Only valid when the
	// subscribing principal has the DPO role; the handler is responsible
	// for enforcing this and for auditing the elevation.
	AllTenants bool

	// Filter is an optional narrowing of action/actor/target.
	Filter StreamFilter

	// ch is the outbound queue. Capacity is chosen so typical burst
	// (policy push fanout, bulk endpoint ops) does not cause drops.
	ch chan Entry

	// dropped counts entries the broker could not enqueue because the
	// channel was full. Surfaced via Dropped() for observability.
	dropped atomic.Int64
}

// NewSubscriber constructs a subscriber with a bounded buffer. Call
// Broker.Subscribe to register it and Broker.Unsubscribe to remove it.
func NewSubscriber(tenantID string, allTenants bool, filter StreamFilter) *Subscriber {
	return &Subscriber{
		TenantID:   tenantID,
		AllTenants: allTenants,
		Filter:     filter,
		ch:         make(chan Entry, 64),
	}
}

// C returns a receive-only channel for the handler to range over.
func (s *Subscriber) C() <-chan Entry { return s.ch }

// Dropped returns the number of entries the broker has discarded for this
// subscriber because its channel was full. Exposed for observability and
// for the WebSocket handler to surface in a terminal frame on close.
func (s *Subscriber) Dropped() int64 { return s.dropped.Load() }

// Broker is the in-process fanout registry. Zero value is NOT usable —
// use NewBroker. It is safe for concurrent Subscribe/Unsubscribe/Publish.
type Broker struct {
	mu   sync.RWMutex
	subs map[*Subscriber]struct{}
	log  *slog.Logger
}

// NewBroker constructs an empty Broker.
func NewBroker(log *slog.Logger) *Broker {
	return &Broker{
		subs: make(map[*Subscriber]struct{}),
		log:  log,
	}
}

// Subscribe registers s to receive subsequent Publish calls. Idempotent.
func (b *Broker) Subscribe(s *Subscriber) {
	if s == nil {
		return
	}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
}

// Unsubscribe removes s and closes its channel. The handler goroutine
// ranging over Subscriber.C() will observe the close and return. Safe to
// call multiple times (subsequent calls are no-ops).
func (b *Broker) Unsubscribe(s *Subscriber) {
	if s == nil {
		return
	}
	b.mu.Lock()
	if _, ok := b.subs[s]; ok {
		delete(b.subs, s)
		close(s.ch)
	}
	b.mu.Unlock()
}

// Publish fans e out to every subscriber whose tenant and filter match.
// Non-blocking per subscriber: a full channel causes a drop and a counter
// increment on that specific subscriber, never stalling publish.
//
// Every entry is passed through StripSensitive before fanout so no
// keystroke content can reach the wire regardless of what the handler
// recorded in Details.
func (b *Broker) Publish(e Entry) {
	e = StripSensitive(e)

	b.mu.RLock()
	defer b.mu.RUnlock()
	for s := range b.subs {
		if !s.AllTenants && s.TenantID != e.TenantID {
			continue
		}
		if !s.Filter.match(e) {
			continue
		}
		select {
		case s.ch <- e:
		default:
			// Subscriber is behind; drop and count. We deliberately do
			// NOT log at warn level per publish: a runaway producer
			// would flood stderr. The counter plus a scheduled summary
			// in the handler close path is sufficient.
			s.dropped.Add(1)
		}
	}
}

// Subscribers returns the current number of registered subscribers.
// Used by the handler health surface and by tests.
func (b *Broker) Subscribers() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}

// sensitiveDetailKeys are Entry.Details map keys whose values are ALWAYS
// stripped before fanout. The authoritative list here intentionally errs
// on the side of the KVKK invariant — if a new field that may carry
// keystroke-adjacent data is introduced, add it here first, then verify
// downstream consumers still receive what they need.
var sensitiveDetailKeys = []string{
	"keystroke_content",
	"keystroke_plaintext",
	"keystroke_blob",
	"keystroke_ciphertext",
	"keystroke_bytes",
	"pe_dek_plaintext",
	"dek_plaintext",
	"password",
	"secret",
}

// StripSensitive returns a copy of e with all keystroke-content-adjacent
// keys removed from Details. The original entry is NOT mutated so the
// Recorder's audit chain input is preserved. ADR 0013 compliance — admins
// can subscribe to the stream and still cannot observe keystroke content
// even if a handler accidentally placed it in Details.
func StripSensitive(e Entry) Entry {
	if len(e.Details) == 0 {
		return e
	}
	needStrip := false
	for _, k := range sensitiveDetailKeys {
		if _, ok := e.Details[k]; ok {
			needStrip = true
			break
		}
	}
	if !needStrip {
		return e
	}
	clone := make(map[string]any, len(e.Details))
	for k, v := range e.Details {
		if isSensitiveKey(k) {
			continue
		}
		clone[k] = v
	}
	e.Details = clone
	return e
}

func isSensitiveKey(k string) bool {
	lk := strings.ToLower(k)
	for _, s := range sensitiveDetailKeys {
		if lk == s {
			return true
		}
	}
	return false
}
