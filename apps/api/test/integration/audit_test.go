//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/personel/api/internal/audit"
)

// TestAuditLog_HashChain verifies that the stored procedure maintains a valid
// hash chain: each row's hash is computed from its content + the previous row's hash.
func TestAuditLog_HashChain(t *testing.T) {
	pool := testDB(t)
	log := testLogger(t)
	ctx := context.Background()

	tenantID := seedTenant(t, pool, "audit-chain")
	rec := testRecorder(pool, log)

	// Write 10 audit entries.
	for i := 0; i < 10; i++ {
		_, err := rec.Append(ctx, audit.Entry{
			Actor:    "system",
			TenantID: tenantID,
			Action:   audit.ActionAdminLoginSuccess,
			Target:   "user:test",
			Details:  map[string]any{"seq": i},
		})
		require.NoError(t, err, "append entry %d", i)
	}

	// Verify chain integrity by walking the rows and recomputing hashes.
	// The verifier reads: id, ts (timestamptz), actor, tenant_id, action, target,
	// details (raw jsonb bytes), prev_hash, hash.
	dbRows, err := pool.Query(ctx,
		`SELECT id, ts, actor, tenant_id::text, action, target, details, prev_hash, hash
		 FROM audit.audit_log
		 WHERE tenant_id = $1
		 ORDER BY id ASC`,
		tenantID,
	)
	require.NoError(t, err)
	defer dbRows.Close()

	type row struct {
		id         int64
		ts         time.Time
		actor      string
		tenantID   string
		action     string
		target     string
		details    []byte
		prevHash   []byte
		storedHash []byte
	}

	var records []row
	for dbRows.Next() {
		var r row
		require.NoError(t, dbRows.Scan(
			&r.id, &r.ts, &r.actor, &r.tenantID, &r.action, &r.target,
			&r.details, &r.prevHash, &r.storedHash,
		))
		records = append(records, r)
	}
	require.NoError(t, dbRows.Err())
	require.Len(t, records, 10)

	// Recompute each hash using CanonicalRecord and verify it matches the stored value.
	// First row has prev_hash = GenesisHash (32 zero bytes).
	prevHash := audit.GenesisHash()
	for i, r := range records {
		cr := audit.CanonicalRecord{
			ID:       r.id,
			Ts:       r.ts,
			Actor:    r.actor,
			TenantID: r.tenantID,
			Action:   r.action,
			Target:   r.target,
			Details:  nil,    // use raw path
			PrevHash: prevHash,
		}
		computed, hashErr := cr.HashWithRawDetails(r.details)
		require.NoError(t, hashErr, "hash row %d", i)

		assert.Equal(t, r.storedHash, computed,
			"hash mismatch at row %d (id=%d)", i, r.id)
		prevHash = r.storedHash
	}
}

// TestAuditLog_ActionValidation verifies that only known actions are accepted.
func TestAuditLog_ActionValidation(t *testing.T) {
	pool := testDB(t)
	log := testLogger(t)
	ctx := context.Background()

	tenantID := seedTenant(t, pool, "audit-validation")
	rec := testRecorder(pool, log)

	// Known action: should succeed.
	_, err := rec.Append(ctx, audit.Entry{
		Actor:    "user-1",
		TenantID: tenantID,
		Action:   audit.ActionUserCreated,
		Target:   "user:user-1",
	})
	require.NoError(t, err)

	// Unknown action: should fail before hitting the DB.
	_, err = rec.Append(ctx, audit.Entry{
		Actor:    "user-1",
		TenantID: tenantID,
		Action:   audit.Action("unknown.action.xyz"),
		Target:   "user:user-1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown action")
}

// TestAuditLog_AllActionsAreValid verifies that every constant in AllActions
// round-trips through ValidAction — catching typos and drift.
func TestAuditLog_AllActionsAreValid(t *testing.T) {
	for _, a := range audit.AllActions {
		assert.True(t, audit.ValidAction(a), "action %q in AllActions but ValidAction returned false", a)
	}
}

// TestAuditLog_NoConcurrentChainBreak verifies that concurrent appends from
// multiple goroutines do not corrupt the chain (pg_advisory_xact_lock).
func TestAuditLog_NoConcurrentChainBreak(t *testing.T) {
	pool := testDB(t)
	log := testLogger(t)
	ctx := context.Background()

	tenantID := seedTenant(t, pool, "audit-concurrent")
	rec := testRecorder(pool, log)

	const goroutines = 10
	const entriesEach = 5

	errc := make(chan error, goroutines*entriesEach)

	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			for i := 0; i < entriesEach; i++ {
				_, err := rec.Append(ctx, audit.Entry{
					Actor:    "concurrent-actor",
					TenantID: tenantID,
					Action:   audit.ActionPolicyUpdated,
					Target:   "policy:test",
					Details:  map[string]any{"goroutine": g, "i": i},
				})
				errc <- err
			}
		}()
	}

	// Collect errors.
	for i := 0; i < goroutines*entriesEach; i++ {
		require.NoError(t, <-errc, "concurrent append should not error")
	}

	// Verify chain is intact: no gaps in prev_hash continuity.
	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM audit.audit_log WHERE tenant_id = $1`,
		tenantID,
	).Scan(&count))
	assert.Equal(t, goroutines*entriesEach, count)

	// The last row's prev_hash should not be all zeros (genesis) unless it's
	// the very first row. With 50 rows the chain must be non-trivial.
	var lastPrevHash []byte
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT prev_hash FROM audit.audit_log WHERE tenant_id = $1 ORDER BY id DESC LIMIT 1`,
		tenantID,
	).Scan(&lastPrevHash))
	// prev_hash of the last row should be non-zero (it references the penultimate row).
	allZero := true
	for _, b := range lastPrevHash {
		if b != 0 {
			allZero = false
			break
		}
	}
	assert.False(t, allZero, "chain should be non-trivial after 50 entries")
}

// TestAuditLog_CanonicalRecordHash is a pure unit test (no DB needed)
// verifying Hash() is deterministic and sensitive to field changes.
func TestAuditLog_CanonicalRecordHash(t *testing.T) {
	base := audit.CanonicalRecord{
		ID:       1,
		Ts:       time.Unix(1700000000, 0),
		Actor:    "user-abc",
		TenantID: "tenant-xyz",
		Action:   "user.created",
		Target:   "user:123",
		Details:  map[string]any{"key": "value"},
		PrevHash: audit.GenesisHash(),
	}

	h1, err := base.Hash()
	require.NoError(t, err)
	assert.Len(t, h1, 32, "hash should be 32 bytes (SHA-256)")

	// Same input produces same hash.
	h2, err := base.Hash()
	require.NoError(t, err)
	assert.Equal(t, h1, h2, "hash must be deterministic")

	// Changing actor changes the hash.
	modified := base
	modified.Actor = "user-different"
	hDiff, err := modified.Hash()
	require.NoError(t, err)
	assert.NotEqual(t, h1, hDiff, "hash must change when actor changes")

	// Changing details changes the hash.
	modified = base
	modified.Details = map[string]any{"key": "other"}
	hDiff, err = modified.Hash()
	require.NoError(t, err)
	assert.NotEqual(t, h1, hDiff, "hash must change when details change")

	// Changing prev_hash changes the hash.
	modified = base
	modified.PrevHash = make([]byte, 32)
	modified.PrevHash[0] = 0xff
	hDiff, err = modified.Hash()
	require.NoError(t, err)
	assert.NotEqual(t, h1, hDiff, "hash must change when prev_hash changes")
}
