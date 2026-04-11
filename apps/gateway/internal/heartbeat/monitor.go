// Package heartbeat implements Flow 7 — employee-initiated agent disable detection.
// It tracks last-seen timestamps per endpoint and classifies gaps.
package heartbeat

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/personel/gateway/internal/config"
	"github.com/personel/gateway/internal/observability"
)

// State represents the connectivity state of an endpoint.
type State string

const (
	StateOnline           State = "online"
	StateDegraded         State = "degraded"
	StateOffline          State = "offline"
	StateOfflineExtended  State = "offline_extended"
)

// EndpointState holds live tracking data for one endpoint.
type EndpointState struct {
	EndpointID     string
	TenantID       string
	LastSeen       time.Time
	ConnectedAt    time.Time
	LastHeartbeat  time.Time
	GracefulBye    bool
	CurrentState   State
	// AlertSent prevents duplicate alert notifications.
	AlertSent bool
}

// Monitor tracks heartbeat timestamps for all connected and recently-seen
// endpoints. It runs a periodic sweep goroutine to classify state transitions.
type Monitor struct {
	mu        sync.RWMutex
	endpoints map[string]*EndpointState // keyed by endpoint_id

	cfg     config.HeartbeatConfig
	metrics *observability.Metrics
	logger  *slog.Logger

	// publisher is called when a state transition requires an audit/alert event.
	publisher StatePublisher
}

// StatePublisher is the interface the monitor calls when an endpoint changes state.
// Implemented by heartbeat.Publisher; accepts interface for testability.
type StatePublisher interface {
	PublishStateTransition(ctx context.Context, ev StateTransitionEvent) error
}

// StateTransitionEvent carries the context of an endpoint state change.
type StateTransitionEvent struct {
	TenantID         string
	EndpointID       string
	PreviousState    State
	NewState         State
	LastSeen         time.Time
	GapDuration      time.Duration
	GapClassification string
}

// NewMonitor creates a Monitor. Call Start to begin the sweep goroutine.
func NewMonitor(
	cfg config.HeartbeatConfig,
	metrics *observability.Metrics,
	logger *slog.Logger,
	publisher StatePublisher,
) *Monitor {
	return &Monitor{
		endpoints: make(map[string]*EndpointState),
		cfg:       cfg,
		metrics:   metrics,
		logger:    logger,
		publisher: publisher,
	}
}

// RecordConnect records that an agent stream opened.
func (m *Monitor) RecordConnect(endpointID, tenantID string) {
	now := time.Now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.endpoints[endpointID] = &EndpointState{
		EndpointID:   endpointID,
		TenantID:     tenantID,
		LastSeen:     now,
		ConnectedAt:  now,
		CurrentState: StateOnline,
	}
}

// RecordHeartbeat updates the last-seen timestamp and resets state to online.
func (m *Monitor) RecordHeartbeat(endpointID, tenantID string) {
	now := time.Now().UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	ep, ok := m.endpoints[endpointID]
	if !ok {
		ep = &EndpointState{
			EndpointID:  endpointID,
			TenantID:    tenantID,
			ConnectedAt: now,
		}
		m.endpoints[endpointID] = ep
	}
	gap := now.Sub(ep.LastHeartbeat)
	if ep.LastHeartbeat.IsZero() {
		gap = 0
	}
	ep.LastSeen = now
	ep.LastHeartbeat = now
	ep.GracefulBye = false
	prevState := ep.CurrentState
	ep.CurrentState = StateOnline
	ep.AlertSent = false

	if prevState != StateOnline && !ep.LastHeartbeat.IsZero() {
		m.metrics.HeartbeatGapSeconds.WithLabelValues(tenantID).Observe(gap.Seconds())
	}
}

// RecordBye records a graceful stream close (EOF from agent).
func (m *Monitor) RecordBye(endpointID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ep, ok := m.endpoints[endpointID]; ok {
		ep.GracefulBye = true
		ep.LastSeen = time.Now().UTC()
	}
}

// RecordDisconnect records an ungraceful stream close.
// graceful=false means the TCP stream was broken without a Bye message.
func (m *Monitor) RecordDisconnect(endpointID string, graceful bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ep, ok := m.endpoints[endpointID]; ok {
		ep.GracefulBye = graceful
		ep.LastSeen = time.Now().UTC()
	}
}

// Start launches the sweep goroutine. It runs until ctx is cancelled.
func (m *Monitor) Start(ctx context.Context) {
	interval := m.cfg.CheckInterval
	if interval == 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.sweep(ctx)
		}
	}
}

// sweep iterates all tracked endpoints and emits state transitions.
func (m *Monitor) sweep(ctx context.Context) {
	now := time.Now().UTC()

	m.mu.Lock()
	// Snapshot to avoid holding lock during publish calls.
	snapshot := make([]*EndpointState, 0, len(m.endpoints))
	for _, ep := range m.endpoints {
		cp := *ep
		snapshot = append(snapshot, &cp)
	}
	m.mu.Unlock()

	for _, ep := range snapshot {
		gap := now.Sub(ep.LastSeen)
		newState := m.classifyGap(gap, ep)
		if newState == ep.CurrentState {
			continue
		}

		classification := classifyGap(ep.GracefulBye, gap)
		ev := StateTransitionEvent{
			TenantID:          ep.TenantID,
			EndpointID:        ep.EndpointID,
			PreviousState:     ep.CurrentState,
			NewState:          newState,
			LastSeen:          ep.LastSeen,
			GapDuration:       gap,
			GapClassification: classification,
		}

		m.logger.InfoContext(ctx, "heartbeat: endpoint state transition",
			slog.String("endpoint_id", ep.EndpointID),
			slog.String("tenant_id", ep.TenantID),
			slog.String("prev_state", string(ep.CurrentState)),
			slog.String("new_state", string(newState)),
			slog.String("gap_classification", classification),
			slog.Duration("gap", gap),
		)

		if err := m.publisher.PublishStateTransition(ctx, ev); err != nil {
			m.logger.WarnContext(ctx, "heartbeat: publish state transition failed",
				slog.String("error", err.Error()),
			)
		}

		// Update the live map.
		m.mu.Lock()
		if live, ok := m.endpoints[ep.EndpointID]; ok {
			live.CurrentState = newState
			if newState == StateOfflineExtended && !live.AlertSent {
				live.AlertSent = true
			}
		}
		m.mu.Unlock()
	}
}

// classifyGap returns the endpoint State given the gap duration and config.
func (m *Monitor) classifyGap(gap time.Duration, ep *EndpointState) State {
	degraded := m.cfg.DegradedAfter
	if degraded == 0 {
		degraded = 90 * time.Second
	}
	offline := m.cfg.OfflineAfter
	if offline == 0 {
		offline = 5 * time.Minute
	}
	extended := m.cfg.OfflineExtendedAfter
	if extended == 0 {
		extended = 2 * time.Hour
	}

	switch {
	case gap >= extended:
		return StateOfflineExtended
	case gap >= offline:
		return StateOffline
	case gap >= degraded:
		return StateDegraded
	default:
		return StateOnline
	}
}

// classifyGap returns a human-readable gap classification string for audit.
// Matches the threat model's enum: graceful_shutdown, suspected_suspend,
// unreachable, disappeared_unexpectedly.
func classifyGap(gracefulBye bool, gap time.Duration) string {
	if gracefulBye {
		return "graceful_shutdown"
	}
	// Heuristic: short gaps often indicate a machine suspend/sleep.
	if gap < 15*time.Minute {
		return "suspected_suspend"
	}
	if gap < 2*time.Hour {
		return "unreachable"
	}
	return "disappeared_unexpectedly"
}

// Snapshot returns a copy of all current endpoint states for admin queries.
func (m *Monitor) Snapshot() []EndpointState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]EndpointState, 0, len(m.endpoints))
	for _, ep := range m.endpoints {
		result = append(result, *ep)
	}
	return result
}
