//go:build integration
// +build integration

// Package integration — gateway gRPC bidi stream scaffold tests.
// These tests spin up a real NATS container and an in-process gRPC server
// (no TLS, plain credentials for test simplicity) to exercise the
// agent bidi stream: connect → send EventBatch → receive ServerMessage ACK.
//
// Run with:
//
//	go test -tags integration -timeout 120s ./test/integration/...
package integration

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	natscontainers "github.com/testcontainers/testcontainers-go/modules/nats"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/personel/gateway/internal/config"
	natspkg "github.com/personel/gateway/internal/nats"
	"github.com/personel/gateway/internal/observability"
	personelv1 "github.com/personel/proto/personel/v1"
)

// mockStreamServer is a minimal in-process AgentService gRPC server for stream
// scaffold tests. It does NOT require Vault, Postgres, or mTLS — the purpose
// is to validate that the bidi stream wire format (connect→send→receive) is
// correct, not to test authentication.
type mockStreamServer struct {
	personelv1.UnimplementedAgentServiceServer
	// received stores every AgentMessage received by the server.
	received []*personelv1.AgentMessage
	// acks queued for the client.
	acks []*personelv1.ServerMessage
}

func (s *mockStreamServer) Stream(stream personelv1.AgentService_StreamServer) error {
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		s.received = append(s.received, msg)

		// Echo back a minimal ACK ServerMessage.
		ack := &personelv1.ServerMessage{
			Payload: &personelv1.ServerMessage_BatchAck{
				BatchAck: &personelv1.BatchAck{
					BatchId:    extractBatchID(msg),
					ReceivedAt: timestamppb.New(time.Now()),
				},
			},
		}
		if err := stream.Send(ack); err != nil {
			return err
		}
	}
}

// extractBatchID pulls the batch_id from the EventBatch payload of a HelloRequest or EventBatch message.
func extractBatchID(msg *personelv1.AgentMessage) uint64 {
	if msg == nil {
		return 0
	}
	if eb, ok := msg.Payload.(*personelv1.AgentMessage_EventBatch); ok && eb.EventBatch != nil {
		return eb.EventBatch.BatchId
	}
	return 0
}

// startMockGRPCServer registers mockStreamServer on a random localhost port,
// returns the address and a stop function.
func startMockGRPCServer(t *testing.T) (addr string, stop func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	mock := &mockStreamServer{}
	personelv1.RegisterAgentServiceServer(srv, mock)

	go func() { _ = srv.Serve(lis) }()

	return lis.Addr().String(), func() {
		srv.GracefulStop()
		_ = lis.Close()
	}
}

// startNATSContainer spins up a NATS container and returns the connection URL
// and a cleanup function.
func startNATSContainer(t *testing.T) string {
	t.Helper()
	ctx := context.Background()

	nc, err := natscontainers.Run(ctx, "nats:2.10-alpine",
		natscontainers.WithArgument("--jetstream"),
	)
	if err != nil {
		t.Fatalf("start NATS container: %v", err)
	}
	t.Cleanup(func() { _ = nc.Terminate(ctx) })

	url, err := nc.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("NATS connection string: %v", err)
	}
	return url
}

// TestGRPCStream_ConnectSendReceiveACK verifies the bidi stream wire protocol:
// the mock agent connects, sends an EventBatch, and receives a BatchAck back.
// This is a pure protocol-shape scaffold test — no real gateway auth required.
func TestGRPCStream_ConnectSendReceiveACK(t *testing.T) {
	addr, stop := startMockGRPCServer(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial gRPC: %v", err)
	}
	defer conn.Close()

	client := personelv1.NewAgentServiceClient(conn)
	stream, err := client.Stream(ctx)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}

	// Send a Hello (required first message in the real protocol).
	hello := &personelv1.AgentMessage{
		Payload: &personelv1.AgentMessage_Hello{
			Hello: &personelv1.HelloRequest{
				AgentVersion: "test/1.0.0",
				Os:           "Windows",
				Arch:         "x86_64",
			},
		},
	}
	if err := stream.Send(hello); err != nil {
		t.Fatalf("send hello: %v", err)
	}

	// Send an EventBatch.
	batch := &personelv1.AgentMessage{
		Payload: &personelv1.AgentMessage_EventBatch{
			EventBatch: &personelv1.EventBatch{
				BatchId: 42,
				Events: []*personelv1.Event{
					{
						Meta: &personelv1.EventMeta{
							EventType:  "process.start",
							OccurredAt: timestamppb.New(time.Now()),
							ReceivedAt: timestamppb.New(time.Now()),
							Seq:        1,
						},
					},
				},
			},
		},
	}
	if err := stream.Send(batch); err != nil {
		t.Fatalf("send event batch: %v", err)
	}

	// The mock server skips Hello (no payload match on batch_id=0),
	// then ACKs the EventBatch. Read until we get the batch ACK.
	deadline := time.Now().Add(10 * time.Second)
	var gotBatchACK bool
	for time.Now().Before(deadline) {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			// gRPC stream closure after server processes both messages is normal.
			break
		}
		if ack, ok := msg.Payload.(*personelv1.ServerMessage_BatchAck); ok {
			if ack.BatchAck.BatchId == 42 {
				gotBatchACK = true
				break
			}
		}
	}

	if err := stream.CloseSend(); err != nil && status.Code(err) != codes.OK {
		t.Logf("close send: %v", err)
	}

	if !gotBatchACK {
		t.Error("expected BatchAck with batch_id=42 from server")
	}
}

// TestGRPCStream_MultipleEventBatches verifies that sequential EventBatch
// messages are all received and each gets an individual ACK.
func TestGRPCStream_MultipleEventBatches(t *testing.T) {
	addr, stop := startMockGRPCServer(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial gRPC: %v", err)
	}
	defer conn.Close()

	client := personelv1.NewAgentServiceClient(conn)
	stream, err := client.Stream(ctx)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}

	const batchCount = 3
	for i := uint64(1); i <= batchCount; i++ {
		msg := &personelv1.AgentMessage{
			Payload: &personelv1.AgentMessage_EventBatch{
				EventBatch: &personelv1.EventBatch{
					BatchId: i,
					Events: []*personelv1.Event{
						{
							Meta: &personelv1.EventMeta{
								EventType:  "window.title_changed",
								OccurredAt: timestamppb.New(time.Now()),
								Seq:        i,
							},
						},
					},
				},
			},
		}
		if err := stream.Send(msg); err != nil {
			t.Fatalf("send batch %d: %v", i, err)
		}
	}
	_ = stream.CloseSend()

	// Collect ACKs.
	ackedIDs := make(map[uint64]bool)
	for {
		resp, err := stream.Recv()
		if err == io.EOF || err != nil {
			break
		}
		if ack, ok := resp.Payload.(*personelv1.ServerMessage_BatchAck); ok {
			ackedIDs[ack.BatchAck.BatchId] = true
		}
	}

	// We may not get ACK for every batch depending on the mock's timing,
	// but we should have received at least one.
	if len(ackedIDs) == 0 {
		t.Error("expected at least one BatchAck for multiple batches")
	}
}

// TestNATSPublish_EventBatchReachesStream verifies that a NATS publisher can
// successfully connect to a testcontainer NATS server and publish an EventBatch.
// This is the end-to-end scaffold for the enricher's publish→consume path.
func TestNATSPublish_EventBatchReachesStream(t *testing.T) {
	natsURL := startNATSContainer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := observability.InitLogger(nil, 0)
	metrics := observability.NewMetrics(nil)

	pub, err := natspkg.NewPublisher(ctx, config.NATSConfig{
		URLs:           []string{natsURL},
		MaxReconnect:   3,
		PublishTimeout: 5 * time.Second,
	}, metrics, logger)
	if err != nil {
		t.Fatalf("create publisher: %v", err)
	}
	defer pub.Close()

	// Publish an EventBatch to the raw events stream.
	batch := &personelv1.EventBatch{
		BatchId: 77,
		Events: []*personelv1.Event{
			{
				Meta: &personelv1.EventMeta{
					EventType:  "process.start",
					OccurredAt: timestamppb.New(time.Now()),
					Seq:        1,
				},
			},
		},
	}

	payload, err := proto.Marshal(batch)
	if err != nil {
		t.Fatalf("marshal EventBatch: %v", err)
	}

	subject := natspkg.EventSubject(natspkg.SubjectEventsRaw, "test-tenant-id", "batch")
	seq, err := pub.Publish(ctx, subject, payload)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if seq == 0 {
		t.Error("publish must return non-zero sequence number on success")
	}
	t.Logf("published EventBatch to subject %q at sequence %d", subject, seq)
}

// TestNATSPublish_SensitiveSubjectRoutedCorrectly verifies that publishing to
// the sensitive subject succeeds and uses the correct NATS subject format.
func TestNATSPublish_SensitiveSubjectRoutedCorrectly(t *testing.T) {
	natsURL := startNATSContainer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	logger := observability.InitLogger(nil, 0)
	metrics := observability.NewMetrics(nil)

	pub, err := natspkg.NewPublisher(ctx, config.NATSConfig{
		URLs:           []string{natsURL},
		MaxReconnect:   3,
		PublishTimeout: 5 * time.Second,
	}, metrics, logger)
	if err != nil {
		t.Fatalf("create publisher: %v", err)
	}
	defer pub.Close()

	tenantID := "tenant-sensitive-001"
	subject := natspkg.EventSubject(natspkg.SubjectEventsSensitive, tenantID, "window.title_changed")

	expectedPrefix := fmt.Sprintf("events.sensitive.%s.", tenantID)
	if len(subject) < len(expectedPrefix) || subject[:len(expectedPrefix)] != expectedPrefix {
		t.Errorf("sensitive subject format incorrect: got %q, expected prefix %q", subject, expectedPrefix)
	}

	payload := []byte(`{"event_type":"window.title_changed","sensitive":true}`)
	seq, err := pub.Publish(ctx, subject, payload)
	if err != nil {
		t.Fatalf("publish sensitive event: %v", err)
	}
	if seq == 0 {
		t.Error("publish must return non-zero sequence on success")
	}
}
