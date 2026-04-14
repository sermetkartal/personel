// Package nats — NATS JetStream publisher for policy and live view commands.
package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	gonats "github.com/nats-io/nats.go"
)

// LiveViewPublisher is the interface that liveview.Service depends on.
// Satisfied by *Publisher and by test stubs.
type LiveViewPublisher interface {
	PublishLiveViewStart(ctx context.Context, tenantID, endpointID string, cmd LiveViewStartCommand) error
	PublishLiveViewStop(ctx context.Context, tenantID, endpointID string, cmd LiveViewStopCommand) error
}

// Publisher wraps the NATS connection for publishing.
type Publisher struct {
	nc  *gonats.Conn
	js  gonats.JetStreamContext
	log *slog.Logger
}

// New creates a Publisher by connecting to the given NATS URL.
func New(url, credsFile string, log *slog.Logger) (*Publisher, error) {
	opts := []gonats.Option{
		gonats.Name("personel-admin-api"),
		gonats.ReconnectWait(2 * time.Second),
		gonats.MaxReconnects(-1),
	}
	if credsFile != "" {
		opts = append(opts, gonats.UserCredentials(credsFile))
	}

	nc, err := gonats.Connect(url, opts...)
	if err != nil {
		return nil, fmt.Errorf("nats: connect %s: %w", url, err)
	}

	js, err := nc.JetStream()
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("nats: jetstream: %w", err)
	}

	log.Info("nats: connected", slog.String("url", url))
	return &Publisher{nc: nc, js: js, log: log}, nil
}

// JS exposes the underlying JetStreamContext for packages (e.g. the
// pipeline DLQ reader) that need direct pull-consumer access beyond
// what Publish offers. Callers MUST NOT close the returned context;
// Publisher.Close() owns the lifetime.
func (p *Publisher) JS() gonats.JetStreamContext {
	return p.js
}

// Publish publishes a raw message to a NATS subject.
func (p *Publisher) Publish(_ context.Context, subject string, data []byte) error {
	_, err := p.js.Publish(subject, data)
	if err != nil {
		return fmt.Errorf("nats: publish %s: %w", subject, err)
	}
	return nil
}

// LiveViewStartCommand carries the data for a live view start control message.
type LiveViewStartCommand struct {
	SessionID        string    `json:"session_id"`
	LiveKitURL       string    `json:"livekit_url"`
	LiveKitRoom      string    `json:"livekit_room"`
	AgentToken       string    `json:"agent_token"`
	NotAfter         time.Time `json:"not_after"`
	ControlSignature []byte    `json:"control_signature"`
	SigningKeyID     string    `json:"signing_key_id"`
	ReasonCode       string    `json:"reason_code"`
}

// LiveViewStopCommand carries the data for a live view stop control message.
type LiveViewStopCommand struct {
	SessionID        string `json:"session_id"`
	Reason           string `json:"reason"`
	ControlSignature []byte `json:"control_signature"`
	SigningKeyID     string `json:"signing_key_id"`
}

// PublishLiveViewStart publishes a live view start command to the gateway.
//
// Subject scheme matches the live_view_control JetStream filter
// `live_view.control.>` — using `liveview.v1.start.*` landed on a subject
// that no stream captured, producing "no response from stream" errors.
func (p *Publisher) PublishLiveViewStart(ctx context.Context, tenantID, endpointID string, cmd LiveViewStartCommand) error {
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("nats: marshal live view start: %w", err)
	}
	subject := fmt.Sprintf("live_view.control.start.%s.%s", tenantID, endpointID)
	return p.Publish(ctx, subject, data)
}

// PublishLiveViewStop publishes a live view stop command to the gateway.
func (p *Publisher) PublishLiveViewStop(ctx context.Context, tenantID, endpointID string, cmd LiveViewStopCommand) error {
	data, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("nats: marshal live view stop: %w", err)
	}
	subject := fmt.Sprintf("live_view.control.stop.%s.%s", tenantID, endpointID)
	return p.Publish(ctx, subject, data)
}

// Close closes the NATS connection.
func (p *Publisher) Close() {
	p.nc.Close()
}

// NowUnix returns current Unix timestamp.
func NowUnix() int64 {
	return time.Now().UTC().Unix()
}
