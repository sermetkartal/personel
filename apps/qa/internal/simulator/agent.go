// agent.go implements a single simulated Personel agent.
//
// One SimAgent instance models one endpoint: it establishes an mTLS gRPC
// bidi stream to the gateway, sends a Hello message (with key version fields
// from key-hierarchy.md), streams EventBatch messages, handles incoming
// ServerMessage variants (BatchAck, PolicyPush, RotateCert, Ping), and tracks
// backpressure from the gateway.
//
// The agent is designed to be run in a goroutine and is stopped via context
// cancellation. It reconnects on transport errors with exponential backoff,
// mirroring the Rust agent's BackoffConfig from client.rs.
package simulator

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/types/known/timestamppb"

	personelv1 "github.com/personel/qa/internal/proto"
)

// AgentConfig holds parameters for a single simulated agent.
type AgentConfig struct {
	GatewayAddr    string        // host:port
	TenantID       string        // UUID string
	EndpointID     string        // UUID string
	AgentVersion   string        // "1.0.0"
	PEDEKVersion   uint32        // from key-hierarchy.md
	TMKVersion     uint32        // from key-hierarchy.md
	TLSConfig      *tls.Config   // built by TestCA.ClientTLSConfig
	HeartbeatEvery time.Duration // default 30s
	UploadEvery    time.Duration // default 5s
	BatchSize      int           // max events per batch
	Metrics        *SimulatorMetrics
	Seed           uint64 // 0 = random
}

// DefaultAgentConfig returns sensible defaults matching the real Rust agent.
func DefaultAgentConfig() AgentConfig {
	return AgentConfig{
		AgentVersion:   "1.0.0-sim",
		PEDEKVersion:   DefaultPEDEKVersion,
		TMKVersion:     DefaultTMKVersion,
		HeartbeatEvery: 30 * time.Second,
		UploadEvery:    5 * time.Second,
		BatchSize:      50,
	}
}

// SimAgent is a single simulated endpoint agent.
type SimAgent struct {
	cfg      AgentConfig
	cert     *AgentCert
	gen      *EventGenerator
	dek      *TestPEDEK
	metrics  *SimulatorMetrics
	batchSeq atomic.Uint64
	eventSeq atomic.Uint64
	connectedFlag atomic.Bool
	mu       sync.Mutex
	log      *slog.Logger
}

// NewSimAgent creates a new simulated agent ready to be Run.
func NewSimAgent(cfg AgentConfig, cert *AgentCert) *SimAgent {
	gen := NewEventGenerator(cfg.EndpointID, cfg.TenantID, cfg.Seed)
	dek := NewTestPEDEK(cfg.EndpointID, cfg.PEDEKVersion, cfg.TMKVersion)

	return &SimAgent{
		cfg:     cfg,
		cert:    cert,
		gen:     gen,
		dek:     dek,
		metrics: cfg.Metrics,
		log: slog.Default().With(
			"component", "sim_agent",
			"endpoint_id", cfg.EndpointID,
		),
	}
}

// Run starts the agent's stream loop. Blocks until ctx is cancelled.
// Reconnects on errors using exponential backoff (matching the Rust agent).
func (a *SimAgent) Run(ctx context.Context) {
	b := newBackoff(2*time.Second, 120*time.Second, 2.0, 0.3)

	for {
		select {
		case <-ctx.Done():
			a.log.Info("agent stopping", "reason", ctx.Err())
			return
		default:
		}

		if err := a.runOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			delay := b.next()
			a.log.Warn("stream error; reconnecting",
				"error", err,
				"delay", delay,
			)
			if a.metrics != nil {
				a.metrics.RecordError("stream_broken")
				a.metrics.StreamRestarts.Inc()
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		} else {
			b.reset()
		}
	}
}

// runOnce establishes one gRPC stream and runs until the stream ends or ctx
// is cancelled. Returns nil on clean close.
func (a *SimAgent) runOnce(ctx context.Context) error {
	connectStart := time.Now()

	creds := credentials.NewTLS(a.cfg.TLSConfig)
	conn, err := grpc.NewClient(
		a.cfg.GatewayAddr,
		grpc.WithTransportCredentials(creds),
	)
	if err != nil {
		if a.metrics != nil {
			a.metrics.RecordError("connect_failed")
		}
		return fmt.Errorf("dial gateway: %w", err)
	}
	defer conn.Close()

	client := personelv1.NewAgentServiceClient(conn)

	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()

	stream, err := client.Stream(streamCtx)
	if err != nil {
		if a.metrics != nil {
			a.metrics.RecordError("stream_open_failed")
		}
		return fmt.Errorf("open stream: %w", err)
	}

	// Send Hello with key version handshake as per key-hierarchy.md
	// §Key Version Handshake. The gateway validates these integers against
	// Postgres keystroke_keys; any mismatch triggers RotateCert + rekey.
	endpointIDBytes := uuidBytes(a.cfg.EndpointID)
	tenantIDBytes := uuidBytes(a.cfg.TenantID)

	hello := &personelv1.AgentMessage{
		Payload: &personelv1.AgentMessage_Hello{
			Hello: &personelv1.Hello{
				AgentVersion: &personelv1.AgentVersion{
					Major: 1, Minor: 0, Patch: 0, Build: "sim",
				},
				EndpointId:    &personelv1.EndpointId{Value: endpointIDBytes},
				TenantId:      &personelv1.TenantId{Value: tenantIDBytes},
				HwFingerprint: &personelv1.HardwareFingerprint{Blob: deterministicFingerprint(a.cfg.EndpointID)},
				OsVersion:     "Windows 11 Pro 22H2 (sim)",
				AgentBuild:    "sim-1.0.0",
				PeDekVersion:  a.cfg.PEDEKVersion,
				TmkVersion:    a.cfg.TMKVersion,
				LastAckedSeq:  a.eventSeq.Load(),
			},
		},
	}

	if err := stream.Send(hello); err != nil {
		return fmt.Errorf("send hello: %w", err)
	}

	// Await Welcome.
	welcomeMsg, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("recv welcome: %w", err)
	}
	if _, ok := welcomeMsg.Payload.(*personelv1.ServerMessage_Welcome); !ok {
		return fmt.Errorf("expected Welcome, got %T", welcomeMsg.Payload)
	}

	if a.metrics != nil {
		a.metrics.ConnectLatency.Observe(time.Since(connectStart).Seconds())
		a.metrics.AgentsActive.Inc()
	}
	a.connectedFlag.Store(true)

	a.log.Info("stream established")

	defer func() {
		a.connectedFlag.Store(false)
		if a.metrics != nil {
			a.metrics.AgentsActive.Dec()
		}
	}()

	// Start the receive loop in a goroutine.
	recvErrCh := make(chan error, 1)
	go func() {
		recvErrCh <- a.receiveLoop(stream)
	}()

	heartbeatTicker := time.NewTicker(a.cfg.HeartbeatEvery)
	uploadTicker := time.NewTicker(a.cfg.UploadEvery)
	defer heartbeatTicker.Stop()
	defer uploadTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case err := <-recvErrCh:
			return err

		case <-heartbeatTicker.C:
			hb := &personelv1.AgentMessage{
				Payload: &personelv1.AgentMessage_Heartbeat{
					Heartbeat: &personelv1.Heartbeat{
						SentAt:        timestamppb.Now(),
						QueueDepth:    a.gen.Seq(),
						CpuPercent:    0.8 + 0.4*randFloat64(),
						RssBytes:      120 * 1024 * 1024,
						PolicyVersion: "v1.0.0-test",
					},
				},
			}
			if err := stream.Send(hb); err != nil {
				return fmt.Errorf("send heartbeat: %w", err)
			}

		case <-uploadTicker.C:
			if err := a.sendBatch(stream); err != nil {
				return fmt.Errorf("send batch: %w", err)
			}
		}
	}
}

// sendBatch generates and sends one EventBatch.
func (a *SimAgent) sendBatch(stream personelv1.AgentService_StreamClient) error {
	events := a.gen.GenerateBatch(time.Now(), 10*time.Second, a.cfg.BatchSize)
	if len(events) == 0 {
		return nil
	}

	batchID := a.batchSeq.Add(1)
	protoEvents := make([]*personelv1.Event, 0, len(events))

	for _, ev := range events {
		seq := a.eventSeq.Add(1)
		protoEv := buildProtoEvent(ev, a.cfg.TenantID, a.cfg.EndpointID, seq, a.gen)
		protoEvents = append(protoEvents, protoEv)

		if a.metrics != nil {
			a.metrics.RecordEventSent(string(ev.EventType))
		}
	}

	batch := &personelv1.AgentMessage{
		Payload: &personelv1.AgentMessage_EventBatch{
			EventBatch: &personelv1.EventBatch{
				BatchId: batchID,
				Events:  protoEvents,
			},
		},
	}

	if err := stream.Send(batch); err != nil {
		return err
	}

	if a.metrics != nil {
		a.metrics.BatchesSent.Inc()
	}

	return nil
}

// receiveLoop handles incoming ServerMessage variants.
func (a *SimAgent) receiveLoop(stream personelv1.AgentService_StreamClient) error {
	for {
		msg, err := stream.Recv()
		if err != nil {
			return err
		}

		switch p := msg.Payload.(type) {
		case *personelv1.ServerMessage_BatchAck:
			ack := p.BatchAck
			a.log.Debug("batch acked",
				"batch_id", ack.BatchId,
				"accepted", ack.AcceptedCount,
				"rejected", ack.RejectedCount,
			)
			if a.metrics != nil {
				a.metrics.BatchesAcked.Inc()
				outcome := "accepted"
				if ack.RejectedCount > 0 {
					outcome = "rejected"
				}
				a.metrics.AcksReceived.WithLabelValues(outcome).Add(float64(ack.AcceptedCount + ack.RejectedCount))
			}

		case *personelv1.ServerMessage_PolicyPush:
			push := p.PolicyPush
			a.log.Info("policy pushed", "version", push.PolicyVersion)
			ack := &personelv1.AgentMessage{
				Payload: &personelv1.AgentMessage_PolicyAck{
					PolicyAck: &personelv1.PolicyAck{
						PolicyVersion: push.PolicyVersion,
						Applied:       true,
					},
				},
			}
			if err := stream.Send(ack); err != nil {
				return fmt.Errorf("send policy ack: %w", err)
			}

		case *personelv1.ServerMessage_RotateCert:
			rotate := p.RotateCert
			a.log.Warn("cert rotation requested",
				"reason", rotate.Reason,
			)
			// In the simulator we log but don't actually rotate.
			// keyrotation_test.go exercises this path end-to-end against the stack.

		case *personelv1.ServerMessage_Ping:
			// Heartbeat serves as keep-alive; no explicit pong needed.

		default:
			a.log.Debug("unhandled server message", "type", fmt.Sprintf("%T", msg.Payload))
		}
	}
}

// Connected returns true if the agent currently has an active stream.
func (a *SimAgent) Connected() bool {
	return a.connectedFlag.Load()
}

// EndpointID returns the endpoint identifier for this agent.
func (a *SimAgent) EndpointID() string {
	return a.cfg.EndpointID
}

// buildProtoEvent constructs a proto Event for the given GeneratedEvent.
func buildProtoEvent(ev GeneratedEvent, tenantID, endpointID string, seq uint64, gen *EventGenerator) *personelv1.Event {
	meta := &personelv1.EventMeta{
		EventId:       &personelv1.EventId{Value: uuidBytes(ev.EventID)},
		EventType:     string(ev.EventType),
		SchemaVersion: 1,
		TenantId:      &personelv1.TenantId{Value: uuidBytes(tenantID)},
		EndpointId:    &personelv1.EndpointId{Value: uuidBytes(endpointID)},
		UserSid:       &personelv1.WindowsUserSid{Value: "S-1-5-21-1234567890-1234567890-1234567890-1001"},
		OccurredAt:    timestamppb.New(ev.OccurredAt),
		AgentVersion:  &personelv1.AgentVersion{Major: 1, Minor: 0, Patch: 0, Build: "sim"},
		Seq:           seq,
	}

	event := &personelv1.Event{Meta: meta}

	switch ev.EventType {
	case EventTypeProcessStart:
		event.Payload = &personelv1.Event_ProcessStart{
			ProcessStart: &personelv1.ProcessStart{
				Pid:            uint32(1000 + seq%5000),
				ParentPid:      1024,
				ImagePath:      `C:\Windows\System32\` + gen.SyntheticProcessName(),
				IntegrityLevel: "medium",
			},
		}
	case EventTypeWindowTitleChanged:
		event.Payload = &personelv1.Event_WindowTitleChanged{
			WindowTitleChanged: &personelv1.WindowTitleChanged{
				Pid:                  uint32(2000 + seq%100),
				Hwnd:                 seq * 0x10001,
				Title:                gen.SyntheticWindowTitle(),
				ExeName:              gen.SyntheticProcessName(),
				DurationMsInPrevious: uint64(1000 + seq*300%10000),
			},
		}
	case EventTypeKeystrokeWindowStats:
		event.Payload = &personelv1.Event_KeystrokeWindowStats{
			KeystrokeWindowStats: &personelv1.KeystrokeWindowStats{
				Hwnd:             seq * 0x10001,
				ExeName:          gen.SyntheticProcessName(),
				KeystrokeCount:   uint32(100 + seq%400),
				BackspaceCount:   uint32(5 + seq%20),
				PasteCount:       uint32(seq % 5),
				WindowDurationMs: uint64(60000 + seq*1000%300000),
			},
		}
	case EventTypeKeystrokeContent:
		event.Payload = &personelv1.Event_KeystrokeContentEncrypted{
			KeystrokeContentEncrypted: &personelv1.KeystrokeContentEncrypted{
				Hwnd:          seq * 0x10001,
				ExeName:       gen.SyntheticProcessName(),
				CiphertextRef: ev.BlobRef,
				DekWrapRef:    fmt.Sprintf("vault://transit/keys/tenant/%s/tmk/v%d", tenantID, DefaultTMKVersion),
				Nonce:         make([]byte, 12),
				Aad:           buildAAD(endpointID, seq),
				ByteLen:       uint32(ev.PayloadSize),
				KeyVersion:    DefaultPEDEKVersion,
			},
		}
	case EventTypeNetworkFlowSummary:
		destIP, destPort := gen.SyntheticNetworkDest()
		event.Payload = &personelv1.Event_NetworkFlowSummary{
			NetworkFlowSummary: &personelv1.NetworkFlowSummary{
				Pid:       uint32(3000 + seq%200),
				ExeName:   gen.SyntheticProcessName(),
				DestIp:    destIP,
				DestPort:  destPort,
				Protocol:  "tcp",
				BytesOut:  uint64(1024 + seq*100%50000),
				BytesIn:   uint64(4096 + seq*500%200000),
				FlowStart: timestamppb.New(ev.OccurredAt.Add(-time.Second)),
				FlowEnd:   timestamppb.New(ev.OccurredAt),
			},
		}
	case EventTypeAgentHealthHeartbeat:
		event.Payload = &personelv1.Event_AgentHealthHeartbeat{
			AgentHealthHeartbeat: &personelv1.AgentHealthHeartbeat{
				CpuPercent:    0.8 + 0.4*randFloat64(),
				RssBytes:      120 * 1024 * 1024,
				QueueDepth:    seq % 100,
				PolicyVersion: "v1.0.0-test",
			},
		}
	case EventTypeFileWritten:
		event.Payload = &personelv1.Event_FileWritten{
			FileWritten: &personelv1.FileWritten{
				Path:       gen.SyntheticFilePath(),
				Pid:        uint32(2000 + seq%300),
				BytesDelta: uint64(512 + seq*128%65536),
			},
		}
	default:
		// Unmapped event types use a minimal valid heartbeat payload.
		event.Payload = &personelv1.Event_AgentHealthHeartbeat{
			AgentHealthHeartbeat: &personelv1.AgentHealthHeartbeat{
				CpuPercent: 0.5,
				RssBytes:   100 * 1024 * 1024,
			},
		}
	}

	return event
}

// uuidBytes converts a UUID string to a 16-byte slice.
func uuidBytes(id string) []byte {
	b := make([]byte, 16)
	clean := make([]byte, 0, 32)
	for _, c := range []byte(id) {
		if c != '-' {
			clean = append(clean, c)
		}
	}
	if len(clean) == 32 {
		for i := 0; i < 16; i++ {
			b[i] = hexVal(clean[i*2])<<4 | hexVal(clean[i*2+1])
		}
	}
	return b
}

func hexVal(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 0
}

func deterministicFingerprint(endpointID string) []byte {
	h := sha256.Sum256([]byte("hw-fingerprint:" + endpointID))
	return h[:]
}

// randFloat64 returns a pseudo-random float64 using a simple LCG.
// Not cryptographic; used only for synthetic CPU/RAM jitter values.
var randState uint64 = 0x12345678abcdef01

func randFloat64() float64 {
	randState = randState*6364136223846793005 + 1442695040888963407
	return float64(randState>>11) / (1 << 53)
}

// backoff implements exponential backoff with jitter, mirroring the Rust
// agent's BackoffConfig from apps/agent/crates/personel-transport/src/client.rs.
type backoff struct {
	base       time.Duration
	max        time.Duration
	multiplier float64
	jitter     float64
	current    time.Duration
}

func newBackoff(base, max time.Duration, multiplier, jitter float64) *backoff {
	return &backoff{base: base, max: max, multiplier: multiplier, jitter: jitter, current: base}
}

func (b *backoff) next() time.Duration {
	d := b.current
	next := time.Duration(float64(b.current) * b.multiplier)
	if next > b.max {
		next = b.max
	}
	jitterNs := float64(d) * b.jitter * (2*randFloat64() - 1)
	d += time.Duration(jitterNs)
	if d < b.base/2 {
		d = b.base / 2
	}
	b.current = next
	return d
}

func (b *backoff) reset() {
	b.current = b.base
}
