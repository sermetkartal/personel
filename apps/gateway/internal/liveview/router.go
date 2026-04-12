// Package liveview routes LiveViewStart commands from admin-api (via NATS) to
// the specific agent gRPC stream identified by endpoint_id.
package liveview

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/nats-io/nats.go/jetstream"

	personelv1 "github.com/personel/proto/personel/v1"
)

// Router maintains a registry of active gRPC send channels, one per
// connected endpoint. When a LiveViewStart command arrives from admin-api
// via NATS, the router looks up the stream and injects the ServerMessage.
type Router struct {
	mu      sync.RWMutex
	streams map[string]chan<- *personelv1.ServerMessage // keyed by endpoint_id
	logger  *slog.Logger
}

// NewRouter creates a Router.
func NewRouter(logger *slog.Logger) *Router {
	return &Router{
		streams: make(map[string]chan<- *personelv1.ServerMessage),
		logger:  logger,
	}
}

// Register associates the endpoint_id with an outbound message channel.
// The stream.go handler calls this on stream open.
func (r *Router) Register(endpointID string, ch chan<- *personelv1.ServerMessage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.streams[endpointID] = ch
	r.logger.Info("liveview router: registered stream", slog.String("endpoint_id", endpointID))
}

// Unregister removes the endpoint's channel when the stream closes.
func (r *Router) Unregister(endpointID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.streams, endpointID)
	r.logger.Info("liveview router: unregistered stream", slog.String("endpoint_id", endpointID))
}

// SendToEndpoint delivers a ServerMessage to the named endpoint's stream.
// Returns ErrEndpointNotConnected if the endpoint has no active stream on this
// gateway instance (admin-api should retry on another gateway instance or
// return an error to the caller).
func (r *Router) SendToEndpoint(endpointID string, msg *personelv1.ServerMessage) error {
	r.mu.RLock()
	ch, ok := r.streams[endpointID]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("liveview: %w: endpoint_id=%s", ErrEndpointNotConnected, endpointID)
	}
	select {
	case ch <- msg:
		return nil
	default:
		return fmt.Errorf("liveview: outbound channel full for endpoint_id=%s", endpointID)
	}
}

// ErrEndpointNotConnected is returned when the target endpoint has no active stream.
var ErrEndpointNotConnected = fmt.Errorf("endpoint not connected to this gateway")

// liveViewCommand is the JSON body expected from admin-api on the
// live_view.control NATS subject.
type liveViewCommand struct {
	CommandType  string `json:"command_type"` // "start" | "stop"
	EndpointID   string `json:"endpoint_id"`
	SessionID    string `json:"session_id"`
	LiveKitRoom  string `json:"livekit_room"`
	LiveKitToken string `json:"livekit_token"`
}

// SubscribeNATS subscribes to live_view.control.> subjects on JetStream and
// forwards commands to registered agent streams. Runs until ctx is cancelled.
func (r *Router) SubscribeNATS(ctx context.Context, js jetstream.JetStream) error {
	cons, err := js.CreateOrUpdateConsumer(ctx, "live_view_control", jetstream.ConsumerConfig{
		Durable:       "gateway-liveview-router",
		FilterSubject: "live_view.control.>",
		AckPolicy:     jetstream.AckExplicitPolicy,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	})
	if err != nil {
		return fmt.Errorf("liveview router: create NATS consumer: %w", err)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			batch, fetchErr := cons.FetchNoWait(16)
			if fetchErr != nil {
				r.logger.Warn("liveview router: fetch error",
					slog.String("error", fetchErr.Error()),
				)
				continue
			}
			for msg := range batch.Messages() {
				r.handleNATSMessage(ctx, msg)
			}
		}
	}()
	return nil
}

// handleNATSMessage processes a single live_view.control message.
func (r *Router) handleNATSMessage(ctx context.Context, msg jetstream.Msg) {
	defer func() { _ = msg.Ack() }()

	var cmd liveViewCommand
	if err := json.Unmarshal(msg.Data(), &cmd); err != nil {
		r.logger.WarnContext(ctx, "liveview router: unmarshal command failed",
			slog.String("error", err.Error()),
		)
		return
	}

	var serverMsg *personelv1.ServerMessage
	switch cmd.CommandType {
	case "start":
		serverMsg = &personelv1.ServerMessage{
			Payload: &personelv1.ServerMessage_LiveViewStart{
				LiveViewStart: &personelv1.LiveViewStart{
					LivekitRoom: cmd.LiveKitRoom,
					AgentToken:  cmd.LiveKitToken,
					SessionId:   &personelv1.SessionId{Value: []byte(cmd.SessionID)},
					ReasonCode:  "admin_request",
				},
			},
		}
	case "stop":
		serverMsg = &personelv1.ServerMessage{
			Payload: &personelv1.ServerMessage_LiveViewStop{
				LiveViewStop: &personelv1.LiveViewStop{
					SessionId: &personelv1.SessionId{Value: []byte(cmd.SessionID)},
					Reason:    "admin_end",
				},
			},
		}
	default:
		r.logger.WarnContext(ctx, "liveview router: unknown command type",
			slog.String("command_type", cmd.CommandType),
		)
		return
	}

	if err := r.SendToEndpoint(cmd.EndpointID, serverMsg); err != nil {
		r.logger.WarnContext(ctx, "liveview router: send to endpoint failed",
			slog.String("endpoint_id", cmd.EndpointID),
			slog.String("error", err.Error()),
		)
	}
}
