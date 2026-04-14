//go:build integration

// Faz 14 #147 — Integration test for Faz 7 #74 Dead Letter Queue.
//
// Boots a real NATS JetStream testcontainer, writes a malformed event
// batch to events.raw.* that fails enricher validation, and verifies
// the bad message lands in the DLQ stream with provenance headers
// (original subject, failure reason, enricher version).
package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	natsctr "github.com/testcontainers/testcontainers-go/modules/nats"
)

func TestPipeline_BadBatch_LandsInDLQ(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	nctr, err := natsctr.Run(ctx, "nats:2.10-alpine",
		natsctr.WithArgument("jetstream", ""),
	)
	require.NoError(t, err, "start nats container")
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(nctr) })

	url, err := nctr.ConnectionString(ctx)
	require.NoError(t, err)
	t.Logf("NATS URL: %s", url)

	// Scenarios (asserted when pipeline.DLQ wiring lands in Faz 7 #74):
	//
	// 1. Publish a proto batch with an invalid tenant_id → DLQ with
	//    header `personel-dlq-reason: tenant_not_found`.
	// 2. Publish a batch whose key_version is unknown to the enricher
	//    → DLQ with `personel-dlq-reason: key_version_mismatch`.
	// 3. Publish a batch with a corrupted envelope → DLQ with
	//    `personel-dlq-reason: crypto_aead_failed`.
	//
	// Each scenario asserts:
	//   - Original subject preserved in header.
	//   - DLQ stream retention = 7 days (matches pipeline/dlq.go).
	//   - audit_log has a `pipeline.dlq_entry` row for each failure.
	t.Log("DLQ integration scaffold — awaiting Faz 7 #74 DLQ adapter")
}

func TestPipeline_DLQReplay_RoundTrips(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	// Replay path: an operator re-injects a fixed DLQ entry via the
	// pipeline.DLQReplay RPC and the enricher accepts it. Requires
	// Faz 7 #75 replay capability. Scaffold only for now.
	t.Log("DLQ replay scaffold — awaiting Faz 7 #75")
}
