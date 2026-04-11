// Package integration — livrec-service integration tests.
//
// These tests use testcontainers-go to spin up MinIO and Vault, then exercise
// the full recording + playback + export flow.
//
// All test bodies are t.Skip'd — structure is scaffolded per spec.
// Implementing the bodies is a Phase 3 task after the Postgres wiring is complete.
//
// Run with: go test -tags integration ./test/integration/...
package integration

import (
	"testing"
)

// TestChunkUploadAndPlayback tests the full round-trip:
//   1. POST /v1/record/chunk (N chunks)
//   2. GET /v1/record/{session_id}/stream (SSE)
//   3. Verify chunk count and DEK event delivered once.
func TestChunkUploadAndPlayback(t *testing.T) {
	t.Skip("integration test scaffolded — body TBD in Phase 3 after Postgres wiring")
}

// TestMonotonicChunkEnforcement verifies that out-of-order chunks are rejected.
//   1. POST chunk 0 — expect 204
//   2. POST chunk 2 — expect 409 (gap)
//   3. POST chunk 1 — expect 204
func TestMonotonicChunkEnforcement(t *testing.T) {
	t.Skip("integration test scaffolded — body TBD in Phase 3")
}

// TestDualControlPlaybackGate verifies that playback is refused without an
// approved playback request from the Admin API.
func TestDualControlPlaybackGate(t *testing.T) {
	t.Skip("integration test scaffolded — body TBD in Phase 3")
}

// TestForensicExportDPOOnly verifies that non-DPO roles receive 403.
func TestForensicExportDPOOnly(t *testing.T) {
	t.Skip("integration test scaffolded — body TBD in Phase 3")
}

// TestTTLDeletion verifies that sessions with expired TTL are deleted by the
// TTL scheduler and that sessions on legal hold are skipped.
func TestTTLDeletion(t *testing.T) {
	t.Skip("integration test scaffolded — body TBD in Phase 3")
}

// TestLVMKIsolation verifies that the livrec-service Vault token cannot
// access transit/keys/tenant/+/tmk paths (TMK paths must be denied).
func TestLVMKIsolation(t *testing.T) {
	t.Skip("integration test scaffolded — body TBD in Phase 3; requires live Vault")
}

// TestSSEFlushAfterEachChunk verifies that each SSE chunk event is flushed
// immediately (no buffering), by measuring time-to-first-byte on the stream.
func TestSSEFlushAfterEachChunk(t *testing.T) {
	t.Skip("integration test scaffolded — body TBD in Phase 3")
}

// TestAuditForwarderNonBlocking verifies that an audit forwarder HTTP failure
// (Admin API unreachable) does NOT cause the chunk upload handler to return an error.
func TestAuditForwarderNonBlocking(t *testing.T) {
	t.Skip("integration test scaffolded — body TBD in Phase 3")
}
