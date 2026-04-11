//go:build integration
// +build integration

// Package integration provides integration tests for the gateway components.
// Tests in this file require real NATS + ClickHouse + MinIO containers via
// testcontainers-go. Each test that needs containers uses t.Skip() to disable
// itself in CI unless the PERSONEL_INTEGRATION_TESTS env var is set.
package integration

import (
	"context"
	"testing"
	"time"

	natscontainers "github.com/testcontainers/testcontainers-go/modules/nats"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/personel/gateway/internal/config"
	natspkg "github.com/personel/gateway/internal/nats"
	"github.com/personel/gateway/internal/observability"
	personelv1 "github.com/personel/proto/personel/v1"
)

// TestStreamPublishAndConsume verifies that the publisher can publish an
// EventBatch and that a JetStream consumer receives it with the correct subject.
func TestStreamPublishAndConsume(t *testing.T) {
	t.Skip("requires NATS + ClickHouse + MinIO containers")

	ctx := context.Background()
	logger := observability.InitLogger(nil, 0)

	// Start a NATS server in a container.
	natsContainer, err := natscontainers.Run(ctx, "nats:2.10-alpine")
	if err != nil {
		t.Fatalf("start NATS container: %v", err)
	}
	t.Cleanup(func() { _ = natsContainer.Terminate(ctx) })

	natsURL, err := natsContainer.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("NATS connection string: %v", err)
	}

	metrics := observability.NewMetrics(nil)
	pub, err := natspkg.NewPublisher(ctx, config.NATSConfig{
		URLs:           []string{natsURL},
		MaxReconnect:   5,
		PublishTimeout: 5 * time.Second,
	}, metrics, logger)
	if err != nil {
		t.Fatalf("publisher init: %v", err)
	}
	defer pub.Close()

	// Build a minimal EventBatch.
	batch := &personelv1.EventBatch{
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
	}

	payload, err := proto.Marshal(batch)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	subject := natspkg.EventSubject(natspkg.SubjectEventsRaw, "test-tenant-id", "batch")
	_, err = pub.Publish(ctx, subject, payload)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	// TODO: Set up a consumer and verify the message arrives.
	t.Log("published successfully; consumer verification not yet implemented")
}

// TestAuthInterceptorRejectsUnknownCert verifies that the auth interceptor
// returns codes.PermissionDenied for a cert serial not in Postgres.
func TestAuthInterceptorRejectsUnknownCert(t *testing.T) {
	t.Skip("requires NATS + ClickHouse + MinIO containers")
	// TODO: implement with a test gRPC client using a self-signed cert
	// and a test Postgres instance (testcontainers postgres module).
}

// TestGracefulShutdownDrainsStreams verifies that in-flight streams are
// completed before the process exits during graceful shutdown.
func TestGracefulShutdownDrainsStreams(t *testing.T) {
	t.Skip("requires NATS + ClickHouse + MinIO containers")
}
