package enricher

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/personel/gateway/internal/postgres"
	personelv1 "github.com/personel/proto/personel/v1"
)

// EnrichedEvent wraps a proto Event with server-side enrichment fields.
type EnrichedEvent struct {
	Event       *personelv1.Event
	TenantID    string
	EndpointID  string
	TenantName  string
	Hostname    string
	OSVersion   string
	Sensitive   bool
	PayloadJSON map[string]interface{}
	// Category is derived from event_type for dashboard aggregation.
	Category string
}

// Enricher adds tenant/endpoint context to events. It caches endpoint metadata
// with a short TTL to avoid a Postgres round-trip per event on the hot path.
type Enricher struct {
	pg     *postgres.Pool
	cache  *metaCache
	// policyCache stores the SensitivityGuard config per tenant, refreshed periodically.
	policyMu    sync.RWMutex
	policyCache map[string]*personelv1.SensitivityGuard
}

// NewEnricher creates an Enricher backed by the given Postgres pool.
func NewEnricher(pg *postgres.Pool) *Enricher {
	return &Enricher{
		pg:          pg,
		cache:       newMetaCache(5 * time.Minute),
		policyCache: make(map[string]*personelv1.SensitivityGuard),
	}
}

// Enrich adds tenant/endpoint metadata to an event and derives the JSON payload map.
func (e *Enricher) Enrich(ctx context.Context, ev *personelv1.Event) (*EnrichedEvent, error) {
	meta := ev.GetMeta()
	if meta == nil {
		return nil, fmt.Errorf("enrich: event has no meta")
	}

	tenantID := byteSliceToUUID(meta.GetTenantId().GetValue())
	endpointID := byteSliceToUUID(meta.GetEndpointId().GetValue())

	epMeta, err := e.cache.get(ctx, endpointID, func(ctx context.Context) (*postgres.EndpointMeta, error) {
		uid, parseErr := uuid.Parse(endpointID)
		if parseErr != nil {
			return nil, fmt.Errorf("enrich: parse endpoint_id: %w", parseErr)
		}
		return e.pg.GetEndpointMetadata(ctx, uid)
	})
	if err != nil {
		// Non-fatal: we can proceed without metadata; log and continue.
		epMeta = &postgres.EndpointMeta{
			TenantID:   uuid.MustParse(tenantID),
			EndpointID: uuid.MustParse(endpointID),
		}
	}

	payloadJSON := protoPayloadToMap(ev)
	category := deriveCategory(meta.GetEventType())

	return &EnrichedEvent{
		Event:       ev,
		TenantID:    tenantID,
		EndpointID:  endpointID,
		TenantName:  epMeta.TenantName,
		Hostname:    epMeta.Hostname,
		OSVersion:   epMeta.OSVersion,
		Sensitive:   false, // set by ApplySensitivity
		PayloadJSON: payloadJSON,
		Category:    category,
	}, nil
}

// UpdatePolicyCache stores the SensitivityGuard for a tenant. Called when the
// enricher receives a policy update from NATS.
func (e *Enricher) UpdatePolicyCache(tenantID string, guard *personelv1.SensitivityGuard) {
	e.policyMu.Lock()
	defer e.policyMu.Unlock()
	e.policyCache[tenantID] = guard
}

// policy returns the current SensitivityGuard for the given tenant.
// Returns nil if no policy is cached (sensitivity rules are not applied).
func (e *Enricher) policy(_ context.Context, tenantID string) *personelv1.SensitivityGuard {
	e.policyMu.RLock()
	defer e.policyMu.RUnlock()
	return e.policyCache[tenantID]
}

// deriveCategory maps event_type prefixes to coarse dashboard categories.
func deriveCategory(eventType string) string {
	if len(eventType) == 0 {
		return "unknown"
	}
	for _, prefix := range []struct{ p, cat string }{
		{"process.", "process"},
		{"window.", "window"},
		{"session.", "session"},
		{"screenshot.", "screenshot"},
		{"screenclip.", "screenshot"},
		{"file.", "file"},
		{"clipboard.", "clipboard"},
		{"keystroke.", "keystroke"},
		{"network.", "network"},
		{"usb.", "usb"},
		{"print.", "print"},
		{"web.", "web"},
		{"app.", "app"},
		{"agent.", "agent"},
		{"live_view.", "live_view"},
	} {
		if len(eventType) >= len(prefix.p) && eventType[:len(prefix.p)] == prefix.p {
			return prefix.cat
		}
	}
	return "other"
}

// byteSliceToUUID converts a raw UUID byte slice to a string UUID.
func byteSliceToUUID(b []byte) string {
	if len(b) != 16 {
		return ""
	}
	uid, _ := uuid.FromBytes(b)
	return uid.String()
}

// protoPayloadToMap converts the proto event payload to a map[string]interface{}
// for JSON serialisation. Marshals the payload field using standard JSON.
func protoPayloadToMap(ev *personelv1.Event) map[string]interface{} {
	result := make(map[string]interface{})
	if ev.Payload == nil {
		return result
	}
	// Marshal the payload (a proto oneof value) to JSON.
	raw, err := json.Marshal(ev.Payload)
	if err != nil {
		return result
	}
	_ = json.Unmarshal(raw, &result)
	return result
}

// ----- endpoint metadata cache -----

type metaCache struct {
	mu      sync.RWMutex
	entries map[string]*metaCacheEntry
	ttl     time.Duration
}

type metaCacheEntry struct {
	meta      *postgres.EndpointMeta
	expiresAt time.Time
}

func newMetaCache(ttl time.Duration) *metaCache {
	return &metaCache{
		entries: make(map[string]*metaCacheEntry),
		ttl:     ttl,
	}
}

type fetchFn func(ctx context.Context) (*postgres.EndpointMeta, error)

func (c *metaCache) get(ctx context.Context, endpointID string, fetch fetchFn) (*postgres.EndpointMeta, error) {
	c.mu.RLock()
	entry, ok := c.entries[endpointID]
	c.mu.RUnlock()
	if ok && time.Now().Before(entry.expiresAt) {
		return entry.meta, nil
	}

	meta, err := fetch(ctx)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.entries[endpointID] = &metaCacheEntry{
		meta:      meta,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()
	return meta, nil
}
