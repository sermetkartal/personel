//go:build integration

package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/personel/api/internal/audit"
)

// fakeCheckpointSigner is a minimal audit.CheckpointSigner for tests.
// Uses SHA-256 so signature collision is infeasible; real production
// uses Vault transit Ed25519.
type fakeCheckpointSigner struct{}

func (fakeCheckpointSigner) Sign(_ context.Context, payload []byte) ([]byte, string, error) {
	h := sha256.Sum256(payload)
	return h[:], "fake:v1", nil
}

// recordingSink captures every checkpoint write so the test can assert
// the verifier produced a checkpoint with the expected shape.
type recordingSink struct {
	calls []sinkCall
}

type sinkCall struct {
	day      time.Time
	tenantID string
	payload  []byte
}

func (s *recordingSink) Write(_ context.Context, day time.Time, tenantID string, payload []byte) error {
	s.calls = append(s.calls, sinkCall{day: day, tenantID: tenantID, payload: payload})
	return nil
}

// TestAuditVerifier_FullChainRealPostgres runs the real hash-chain
// verifier against a real Postgres container with rows written via the
// real Recorder → audit.append_event stored procedure. This is the
// end-to-end test for ADR 0014: Go-side hash recomputation must agree
// with Postgres-side stored hashes across arbitrary row content.
//
// If this test ever fails, the audit chain is not actually verifiable —
// the entire integrity story for SOC 2 CC7.1 collapses.
func TestAuditVerifier_FullChainRealPostgres(t *testing.T) {
	pool := testDB(t)
	log := testLogger(t)
	rec := testRecorder(pool, log)
	ctx := context.Background()

	tenantID := seedTenant(t, pool, "audit-verifier-chain")

	// Write 10 audit entries with varied content — some with nested
	// details, some with empty, some with special characters — so the
	// canonicalize path is exercised with realistic payloads.
	_, err := rec.Append(ctx, audit.Entry{
		Actor: "admin-1", TenantID: tenantID,
		Action: audit.ActionPolicyCreated, Target: "policy:test-1",
		Details: map[string]any{"name": "Default Strict", "version": 1},
	})
	require.NoError(t, err)

	_, err = rec.Append(ctx, audit.Entry{
		Actor: "admin-1", TenantID: tenantID,
		Action: audit.ActionPolicyPushed, Target: "policy:test-1",
		Details: map[string]any{"endpoint_id": "*"},
	})
	require.NoError(t, err)

	_, err = rec.Append(ctx, audit.Entry{
		Actor: "dpo-1", TenantID: tenantID,
		Action: audit.ActionDSRSubmitted, Target: "dsr:123",
		Details: nil, // explicit nil path
	})
	require.NoError(t, err)

	_, err = rec.Append(ctx, audit.Entry{
		Actor: "hr-1", TenantID: tenantID,
		Action: audit.ActionLiveViewApproved, Target: "session:abc",
		Details: map[string]any{
			"notes":        "quick look",
			"requester_id": "admin-1",
			// Nested map to exercise json.Marshal key-ordering
			"metadata": map[string]any{"region": "istanbul", "priority": "p2"},
		},
	})
	require.NoError(t, err)

	_, err = rec.Append(ctx, audit.Entry{
		Actor: "system", TenantID: tenantID,
		Action: audit.ActionBackupRun, Target: "backup:postgres",
		Details: map[string]any{"size_bytes": 1234567890, "duration_s": 42},
	})
	require.NoError(t, err)

	// A few more to exercise the batch loop without actually reaching
	// the 50k batch size — just proves ordering is stable.
	for i := 0; i < 5; i++ {
		_, err = rec.Append(ctx, audit.Entry{
			Actor: fmt.Sprintf("user-%d", i), TenantID: tenantID,
			Action: audit.ActionPolicyUpdated, Target: fmt.Sprintf("p-extra-%d", i),
			Details: map[string]any{"seq": i},
		})
		require.NoError(t, err)
	}

	// Run the verifier. worm=nil is acceptable in tests (the verifier
	// logs a warning). sink captures the checkpoint output.
	sink := &recordingSink{}
	verifier := audit.NewVerifier(pool, fakeCheckpointSigner{}, sink, nil, rec, log)
	err = verifier.RunForTenant(ctx, tenantID)
	require.NoError(t, err, "verifier must successfully verify the chain it just saw get written")

	// The verifier should have written exactly one checkpoint for today.
	assert.Len(t, sink.calls, 1, "expected exactly one checkpoint write")
	if len(sink.calls) > 0 {
		assert.Equal(t, tenantID, sink.calls[0].tenantID)
		assert.NotEmpty(t, sink.calls[0].payload)
	}

	// Verify the checkpoint row landed in Postgres too.
	var checkpointCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM audit.audit_checkpoint WHERE tenant_id = $1::uuid`,
		tenantID).Scan(&checkpointCount))
	assert.Equal(t, 1, checkpointCount, "verifier must have written exactly one checkpoint row")

	// The checkpoint's entry_count and last_id should reflect the 10 rows.
	var entryCount, lastID int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT entry_count, last_id FROM audit.audit_checkpoint WHERE tenant_id = $1::uuid`,
		tenantID).Scan(&entryCount, &lastID))
	assert.Equal(t, int64(10), entryCount)
	assert.Greater(t, lastID, int64(0))
}

// TestAuditVerifier_DetectsTampering asserts that a deliberately
// corrupted hash in Postgres breaks the chain verification. Because
// audit.audit_log has an append-only trigger that rejects UPDATE, we
// tamper by raw SQL with the trigger temporarily disabled — exactly
// what a compromised DBA superuser would do, so this test also
// demonstrates the R-API-001 attack the WORM sink mitigates.
func TestAuditVerifier_DetectsTampering(t *testing.T) {
	pool := testDB(t)
	log := testLogger(t)
	rec := testRecorder(pool, log)
	ctx := context.Background()

	tenantID := seedTenant(t, pool, "audit-verifier-tamper")

	// Write 5 rows.
	for i := 0; i < 5; i++ {
		_, err := rec.Append(ctx, audit.Entry{
			Actor: "admin", TenantID: tenantID,
			Action: audit.ActionPolicyCreated, Target: fmt.Sprintf("p-%d", i),
			Details: map[string]any{"i": i},
		})
		require.NoError(t, err)
	}

	// Tamper: flip a byte in row 3's hash with the append-only trigger
	// temporarily disabled (simulating a DBA bypass).
	_, err := pool.Exec(ctx, `ALTER TABLE audit.audit_log DISABLE TRIGGER trg_audit_log_no_update`)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`UPDATE audit.audit_log SET hash = $1 WHERE tenant_id = $2::uuid AND id = (
		    SELECT id FROM audit.audit_log WHERE tenant_id = $2::uuid ORDER BY id LIMIT 1 OFFSET 2
		)`,
		bytes.Repeat([]byte{0xDE}, 32), tenantID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `ALTER TABLE audit.audit_log ENABLE TRIGGER trg_audit_log_no_update`)
	require.NoError(t, err)

	// Verifier must detect the tampered chain and return an error. We
	// don't care about the exact error message as long as it's a chain
	// break signal.
	sink := &recordingSink{}
	verifier := audit.NewVerifier(pool, fakeCheckpointSigner{}, sink, nil, rec, log)
	err = verifier.RunForTenant(ctx, tenantID)
	require.Error(t, err, "tampered chain must fail verification")

	// And the verifier must NOT write a checkpoint for a broken chain —
	// a checkpoint is an attestation that the chain is valid.
	assert.Empty(t, sink.calls, "no checkpoint must be written for a broken chain")

	// Sanity check: the error message should mention "chain broken"
	// — the verifier emits this exact phrase when prev_hash or hash
	// verification fails.
	assert.True(t, strings.Contains(err.Error(), "chain broken"),
		"error message should mention 'chain broken': %v", err)
}
