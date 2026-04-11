package heartbeat

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	natspkg "github.com/personel/gateway/internal/nats"
)

// Publisher publishes heartbeat state transition events to NATS
// subject agent.health.<tenant_id>.<state> so admin-api and DPO dashboards
// can consume them.
type Publisher struct {
	js     jetstream.JetStream
	logger *slog.Logger
}

// NewPublisher creates a heartbeat Publisher.
func NewPublisher(js jetstream.JetStream, logger *slog.Logger) *Publisher {
	return &Publisher{js: js, logger: logger}
}

// agentSilencePayload is the NATS message body for an agent silence event.
type agentSilencePayload struct {
	TenantID          string        `json:"tenant_id"`
	EndpointID        string        `json:"endpoint_id"`
	PreviousState     string        `json:"previous_state"`
	NewState          string        `json:"new_state"`
	LastSeen          time.Time     `json:"last_seen"`
	GapSeconds        float64       `json:"gap_seconds"`
	GapClassification string        `json:"gap_classification"`
	SilenceLevel      string        `json:"silence_level"`
	OccurredAt        time.Time     `json:"occurred_at"`
}

// PublishStateTransition implements StatePublisher. It serialises the transition
// to JSON and publishes to agent.health.<tenant_id>.<new_state>.
func (p *Publisher) PublishStateTransition(ctx context.Context, ev StateTransitionEvent) error {
	level := ClassifySilence(ev.GapDuration)
	msg := agentSilencePayload{
		TenantID:          ev.TenantID,
		EndpointID:        ev.EndpointID,
		PreviousState:     string(ev.PreviousState),
		NewState:          string(ev.NewState),
		LastSeen:          ev.LastSeen,
		GapSeconds:        ev.GapDuration.Seconds(),
		GapClassification: ev.GapClassification,
		SilenceLevel:      string(level),
		OccurredAt:        time.Now().UTC(),
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("heartbeat publisher: marshal: %w", err)
	}

	subject := fmt.Sprintf("%s.%s.%s", natspkg.SubjectAgentHealth, ev.TenantID, ev.NewState)

	_, err = p.js.Publish(ctx, subject, payload)
	if err != nil {
		return fmt.Errorf("heartbeat publisher: nats publish: %w", err)
	}

	p.logger.InfoContext(ctx, "heartbeat: published state transition",
		slog.String("subject", subject),
		slog.String("silence_level", string(level)),
	)
	return nil
}
