package grpcserver

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/personel/gateway/internal/heartbeat"
	"github.com/personel/gateway/internal/liveview"
	natspub "github.com/personel/gateway/internal/nats"
	"github.com/personel/gateway/internal/observability"
	"github.com/personel/gateway/internal/postgres"
	"github.com/personel/gateway/internal/vault"
	personelv1 "github.com/personel/proto/personel/v1"
)

var tracer = otel.Tracer("personel.gateway")

// streamHandler implements the AgentService.Stream RPC.
// It is stateful per connection: it holds the auth identity, rate limiter slot,
// window, and references to upstream state (liveview router, heartbeat monitor).
type streamHandler struct {
	db          *postgres.Pool
	pub         *natspub.Publisher
	vc          *vault.Client
	rl          *RateLimiter
	hvMonitor   *heartbeat.Monitor
	lvRouter    *liveview.Router
	metrics     *observability.Metrics
	logger      *slog.Logger
	maxUnacked  int
	serverVer   string
}

// Stream is the main bidirectional RPC handler. One goroutine is used per
// stream: we receive from the agent in a blocking Recv loop and push outbound
// ServerMessages (policy push, live view control) onto a send channel that is
// drained by the same goroutine via a select.
func (h *streamHandler) Stream(stream personelv1.AgentService_StreamServer) error {
	ctx := stream.Context()

	ai, err := AuthInfoFromContext(ctx)
	if err != nil {
		return status.Error(codes.Unauthenticated, "missing auth context")
	}

	log := observability.FromContext(ctx).With(
		slog.String("tenant_id", ai.TenantID),
		slog.String("endpoint_id", ai.EndpointID),
	)

	h.metrics.ActiveStreams.Inc()
	defer h.metrics.ActiveStreams.Dec()
	defer h.rl.RemoveEndpoint(ai.EndpointID)

	// Register this stream with the live view router so admin commands can reach it.
	outbound := make(chan *personelv1.ServerMessage, 32)
	h.lvRouter.Register(ai.EndpointID, outbound)
	defer h.lvRouter.Unregister(ai.EndpointID)

	// Window for backpressure — bounded ACK window.
	window := NewWindow(h.maxUnacked)

	// State machine: must receive Hello as the very first message.
	var handshakeDone bool

	log.InfoContext(ctx, "stream: agent connected")

	for {
		// Multiplex: drain outbound server messages AND receive from agent.
		// We use a non-blocking send attempt first to keep latency low.
		select {
		case msg := <-outbound:
			if err := stream.Send(msg); err != nil {
				log.WarnContext(ctx, "stream: send server message failed",
					slog.String("error", err.Error()),
				)
				return err
			}
			continue
		default:
		}

		// Receive next agent message.
		agentMsg, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				log.InfoContext(ctx, "stream: agent closed stream (clean EOF)")
				h.hvMonitor.RecordBye(ai.EndpointID)
				return nil
			}
			log.WarnContext(ctx, "stream: recv error",
				slog.String("error", err.Error()),
			)
			h.hvMonitor.RecordDisconnect(ai.EndpointID, false)
			return err
		}

		switch payload := agentMsg.Payload.(type) {

		case *personelv1.AgentMessage_Hello:
			if handshakeDone {
				return status.Error(codes.InvalidArgument, "duplicate Hello message")
			}
			if err := h.handleHello(ctx, stream, payload.Hello, ai, log); err != nil {
				return err
			}
			handshakeDone = true

		case *personelv1.AgentMessage_Heartbeat:
			if !handshakeDone {
				return status.Error(codes.FailedPrecondition, "Hello required before Heartbeat")
			}
			h.hvMonitor.RecordHeartbeat(ai.EndpointID, ai.TenantID)

		case *personelv1.AgentMessage_EventBatch:
			if !handshakeDone {
				return status.Error(codes.FailedPrecondition, "Hello required before EventBatch")
			}
			if err := h.handleEventBatch(ctx, stream, payload.EventBatch, ai, window, log); err != nil {
				return err
			}

		case *personelv1.AgentMessage_Csr:
			if err := h.handleCsrSubmit(ctx, stream, payload.Csr, ai, log); err != nil {
				return err
			}

		case *personelv1.AgentMessage_PolicyAck:
			log.InfoContext(ctx, "stream: policy ack received",
				slog.String("policy_version", payload.PolicyAck.GetPolicyVersion()),
				slog.Bool("applied", payload.PolicyAck.GetApplied()),
			)

		case *personelv1.AgentMessage_UpdateAck:
			log.InfoContext(ctx, "stream: update ack received",
				slog.Bool("success", payload.UpdateAck.GetSuccess()),
			)

		case *personelv1.AgentMessage_QueueHealth:
			log.DebugContext(ctx, "stream: queue health",
				slog.Uint64("total_bytes", payload.QueueHealth.GetTotalBytes()),
				slog.Uint64("evictions", payload.QueueHealth.GetEvictionsSinceLast()),
			)

		default:
			log.WarnContext(ctx, "stream: unhandled agent message type")
		}

		// Also drain any queued outbound messages after each inbound.
	drain:
		for {
			select {
			case msg := <-outbound:
				if err := stream.Send(msg); err != nil {
					return err
				}
			default:
				break drain
			}
		}
	}
}

// handleHello processes the first Hello message, validates key versions, and
// sends a Welcome response.
func (h *streamHandler) handleHello(
	ctx context.Context,
	stream personelv1.AgentService_StreamServer,
	hello *personelv1.Hello,
	ai AuthInfo,
	log *slog.Logger,
) error {
	log.InfoContext(ctx, "stream: Hello received",
		slog.String("agent_build", hello.GetAgentBuild()),
		slog.String("os_version", hello.GetOsVersion()),
		slog.Uint64("pe_dek_version", uint64(hello.GetPeDekVersion())),
		slog.Uint64("tmk_version", uint64(hello.GetTmkVersion())),
	)

	// Key version handshake (see key-hierarchy.md §Key Version Handshake).
	if err := keyVersionHandshake(ctx, hello, h.db, h.metrics, log); err != nil {
		if isKeyVersionStale(err) {
			// Tell the agent to re-enroll with fresh key material.
			rotateMsg := &personelv1.ServerMessage{
				Payload: &personelv1.ServerMessage_RotateCert{
					RotateCert: &personelv1.RotateCert{
						Reason: "rekey",
					},
				},
			}
			_ = stream.Send(rotateMsg)
		}
		return status.Errorf(codes.PermissionDenied, "key version check failed: %v", err)
	}

	welcome := &personelv1.ServerMessage{
		Payload: &personelv1.ServerMessage_Welcome{
			Welcome: &personelv1.Welcome{
				ServerTime:    timestamppb.New(time.Now().UTC()),
				ServerVersion: h.serverVer,
				AckUpToSeq:    hello.GetLastAckedSeq(),
			},
		},
	}
	if err := stream.Send(welcome); err != nil {
		return fmt.Errorf("stream: send Welcome: %w", err)
	}

	h.hvMonitor.RecordConnect(ai.EndpointID, ai.TenantID)
	return nil
}

// handleEventBatch publishes the batch to NATS JetStream and sends a BatchAck.
// Backpressure is applied via the Window: if all window slots are taken, Acquire
// blocks until NATS acks a previous batch.
func (h *streamHandler) handleEventBatch(
	ctx context.Context,
	stream personelv1.AgentService_StreamServer,
	batch *personelv1.EventBatch,
	ai AuthInfo,
	window *Window,
	log *slog.Logger,
) error {
	ctx, span := tracer.Start(ctx, "gateway.EventBatch",
		trace.WithAttributes(
			attribute.String("tenant_id", ai.TenantID),
			attribute.Int64("batch_id", int64(batch.GetBatchId())),
			attribute.Int("event_count", len(batch.GetEvents())),
		),
	)
	defer span.End()

	n := len(batch.GetEvents())
	if n == 0 {
		return nil
	}

	// Rate limit check (per-endpoint + per-tenant).
	if !h.rl.AllowBatch(ctx, ai.TenantID, ai.EndpointID, n) {
		log.WarnContext(ctx, "stream: rate limit exceeded",
			slog.Int("event_count", n),
		)
		// Still send a BatchAck with rejected_count so the agent doesn't block.
		return stream.Send(&personelv1.ServerMessage{
			Payload: &personelv1.ServerMessage_BatchAck{
				BatchAck: &personelv1.BatchAck{
					BatchId:       batch.GetBatchId(),
					AcceptedCount: 0,
					RejectedCount: uint64(n),
				},
			},
		})
	}

	// Stamp server-side received_at on each event.
	receivedAt := timestamppb.New(time.Now().UTC())
	for _, ev := range batch.GetEvents() {
		if ev.GetMeta() != nil {
			ev.GetMeta().ReceivedAt = receivedAt
		}
	}

	// Serialize the batch for NATS publishing.
	payload, err := proto.Marshal(batch)
	if err != nil {
		return status.Errorf(codes.Internal, "marshal EventBatch: %v", err)
	}

	// Build NATS subject. Sensitive detection is done in the enricher; at this
	// layer we publish all events to events.raw. The enricher routes to
	// events.sensitive when the sensitivity guard triggers.
	subject := natspub.EventSubject(natspub.SubjectEventsRaw, ai.TenantID, "batch")

	// Acquire window slot (backpressure).
	if !window.Acquire(ctx.Done()) {
		return status.Error(codes.Unavailable, "stream closed while waiting for window slot")
	}

	start := time.Now()
	_, pubErr := h.pub.Publish(ctx, subject, payload)
	window.Release()

	elapsed := time.Since(start)
	h.metrics.BatchAckLatency.WithLabelValues(ai.TenantID).Observe(elapsed.Seconds())

	var acceptedCount uint64
	var rejectedCount uint64

	if pubErr != nil {
		log.ErrorContext(ctx, "stream: NATS publish failed",
			slog.String("error", pubErr.Error()),
			slog.Uint64("batch_id", batch.GetBatchId()),
		)
		rejectedCount = uint64(n)
	} else {
		acceptedCount = uint64(n)
		for _, ev := range batch.GetEvents() {
			h.metrics.EventsReceived.WithLabelValues(
				ai.TenantID,
				eventTypeSafe(ev),
			).Inc()
		}
		h.metrics.EventsPublished.WithLabelValues(ai.TenantID, "events_raw").Inc()
	}

	return stream.Send(&personelv1.ServerMessage{
		Payload: &personelv1.ServerMessage_BatchAck{
			BatchAck: &personelv1.BatchAck{
				BatchId:       batch.GetBatchId(),
				AcceptedCount: acceptedCount,
				RejectedCount: rejectedCount,
			},
		},
	})
}

// handleCsrSubmit processes a CsrSubmit message: calls Vault to sign the CSR
// and returns the signed cert via CsrResponse.
func (h *streamHandler) handleCsrSubmit(
	ctx context.Context,
	stream personelv1.AgentService_StreamServer,
	csr *personelv1.CsrSubmit,
	ai AuthInfo,
	log *slog.Logger,
) error {
	log.InfoContext(ctx, "stream: CSR submit received",
		slog.String("reason", csr.GetReason()),
	)

	signed, err := h.vc.SignAgentCSR(ctx, ai.TenantID, csr.GetCsrDer())
	if err != nil {
		log.ErrorContext(ctx, "stream: CSR signing failed", slog.String("error", err.Error()))
		return status.Errorf(codes.Internal, "CSR signing failed: %v", err)
	}

	csrResp := &personelv1.CsrResponse{
		CertDer:  signed.CertPEM,
		NotAfter: timestamppb.New(signed.NotAfter),
	}
	for _, chain := range signed.ChainPEM {
		csrResp.ChainDer = append(csrResp.ChainDer, chain)
	}
	resp := &personelv1.ServerMessage{
		Payload: &personelv1.ServerMessage_CsrResponse{
			CsrResponse: csrResp,
		},
	}

	if err := stream.Send(resp); err != nil {
		return fmt.Errorf("stream: send CsrResponse: %w", err)
	}

	log.InfoContext(ctx, "stream: cert renewed",
		slog.String("not_after", signed.NotAfter.String()),
	)
	return nil
}

// eventTypeSafe extracts the event_type string from an Event for metrics labels.
// Returns "unknown" if the meta is missing.
func eventTypeSafe(ev *personelv1.Event) string {
	if ev == nil || ev.GetMeta() == nil {
		return "unknown"
	}
	t := ev.GetMeta().GetEventType()
	if t == "" {
		return "unknown"
	}
	return t
}

