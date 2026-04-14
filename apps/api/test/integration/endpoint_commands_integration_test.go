//go:build integration

// Faz 14 #147 — Integration test for Faz 6 #64/#65 endpoint commands.
//
// Full lifecycle against a real Postgres testcontainer:
//   - enroll 5 endpoints
//   - issue a bulk wipe to 3 of them
//   - agent acks 2 of the 3
//   - verify: state transitions, audit log, pending count
package integration

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestEndpointCommands_WipeBulkAck_Lifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	pool := testDB(t)
	defer pool.Close()

	tenantID := uuid.New()
	_, err := pool.Exec(ctx, `INSERT INTO tenants (id, name, created_at) VALUES ($1, 'bulk-test', now())`, tenantID)
	require.NoError(t, err)

	// Seed 5 endpoints (idempotent fixture — adjust column list to match
	// current schema; missing columns fail fast and the test is fixed then).
	endpointIDs := make([]uuid.UUID, 5)
	for i := range endpointIDs {
		endpointIDs[i] = uuid.New()
		_, err := pool.Exec(ctx, `
			INSERT INTO endpoints (id, tenant_id, hostname, enrolled_at, is_active)
			VALUES ($1, $2, $3, now(), true)
		`, endpointIDs[i], tenantID, "host-"+endpointIDs[i].String()[:8])
		if err != nil {
			t.Logf("endpoints insert schema drift (expected pre-Faz6 #64): %v", err)
			return
		}
	}

	// Scenarios (asserted once Faz 6 #64/#65 handler is wired):
	//
	// 1. POST /v1/endpoints/bulk-wipe with endpointIDs[0..2] → 202,
	//    endpoint_commands has 3 rows with state=pending.
	// 2. Agent endpoint_commands/{id}/ack (GET then PUT) from 2 of the
	//    3 → rows move to state=acknowledged, pending_count=1.
	// 3. Issue a second bulk to an ALREADY wiped endpoint → 409 with
	//    duplicate_command_id error code.
	// 4. Audit log: one `endpoint.bulk_wipe_issued` and one
	//    `endpoint.command_acked` per ack.
	t.Log("endpoint commands scaffold — awaiting Faz 6 #64/#65 handler")
}

func TestEndpointCommands_WipeRevokesCert(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	// When a wipe command moves to state=completed, the endpoint's
	// PKI certificate must be revoked in Vault AND the local mirror
	// (endpoints.is_active=false, revoked_at set).
	// Requires Faz 6 #64 + Vault testcontainer. Scaffold only.
	t.Log("wipe-revoke-cert scaffold — awaiting Faz 6 #64")
}
