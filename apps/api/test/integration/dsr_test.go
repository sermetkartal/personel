//go:build integration

package integration

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/dsr"
)

// TestDSR_FullLifecycle tests the complete DSR lifecycle:
// submit → assign → respond, verifying state transitions and audit entries.
func TestDSR_FullLifecycle(t *testing.T) {
	pool := testDB(t)
	log := testLogger(t)
	rec := testRecorder(pool, log)
	ctx := context.Background()

	tenantID := seedTenant(t, pool, "dsr-lifecycle")
	employeeID := seedUser(t, pool, tenantID, "employee", "employee@dsr.test")
	dpoID := seedUser(t, pool, tenantID, "dpo", "dpo@dsr.test")

	store := dsr.NewStore(pool)
	notifier := &recordingNotifier{}
	svc := dsr.NewService(store, rec, notifier, log)

	// ── Submit ──────────────────────────────────────────────────────────────
	req, err := svc.Submit(ctx, dsr.SubmitInput{
		TenantID:       tenantID,
		EmployeeUserID: employeeID,
		RequestType:    dsr.RequestTypeAccess,
		ScopeJSON:      map[string]any{"categories": []string{"app_usage", "screenshots"}},
		Justification:  "I want to know what data is held about me",
		ActorIP:        "192.168.1.1",
		ActorUA:        "test-agent/1.0",
	})
	require.NoError(t, err, "submit DSR")
	assert.NotEmpty(t, req.ID)
	assert.Equal(t, dsr.StateOpen, req.State)
	assert.Equal(t, tenantID, req.TenantID)
	assert.Equal(t, employeeID, req.EmployeeUserID)
	assert.Equal(t, dsr.RequestTypeAccess, req.RequestType)

	// SLA deadline should be 30 days after creation.
	assert.WithinDuration(t, req.CreatedAt.AddDate(0, 0, 30), req.SLADeadline, 5)

	// DPO and employee notifications should have fired.
	assert.Len(t, notifier.dpoCalls, 1)
	assert.Len(t, notifier.employeeCalls, 1)

	// ── Assign ──────────────────────────────────────────────────────────────
	err = svc.Assign(ctx, tenantID, req.ID, dpoID, dpoID)
	require.NoError(t, err, "assign DSR")

	got, err := svc.Get(ctx, tenantID, req.ID)
	require.NoError(t, err)
	require.NotNil(t, got.AssignedTo)
	assert.Equal(t, dpoID, *got.AssignedTo)

	// ── Respond ─────────────────────────────────────────────────────────────
	artifactRef := "dsr-responses/tenant-id/dsr-" + req.ID + ".pdf"
	err = svc.Respond(ctx, tenantID, req.ID, dpoID, artifactRef)
	require.NoError(t, err, "respond to DSR")

	resolved, err := svc.Get(ctx, tenantID, req.ID)
	require.NoError(t, err)
	assert.Equal(t, dsr.StateResolved, resolved.State)
	require.NotNil(t, resolved.ResponseArtifactRef)
	assert.Equal(t, artifactRef, *resolved.ResponseArtifactRef)
	require.NotNil(t, resolved.AuditChainRef)
	assert.NotEmpty(t, *resolved.AuditChainRef, "audit chain ref should be stored")
	require.NotNil(t, resolved.ClosedAt)
}

// TestDSR_Reject verifies rejection closes the DSR with the correct state.
func TestDSR_Reject(t *testing.T) {
	pool := testDB(t)
	log := testLogger(t)
	rec := testRecorder(pool, log)
	ctx := context.Background()

	tenantID := seedTenant(t, pool, "dsr-reject")
	employeeID := seedUser(t, pool, tenantID, "employee", "employee@reject.test")
	dpoID := seedUser(t, pool, tenantID, "dpo", "dpo@reject.test")

	store := dsr.NewStore(pool)
	svc := dsr.NewService(store, rec, &recordingNotifier{}, log)

	req, err := svc.Submit(ctx, dsr.SubmitInput{
		TenantID:       tenantID,
		EmployeeUserID: employeeID,
		RequestType:    dsr.RequestTypeErase,
		ScopeJSON:      nil,
		Justification:  "Please delete my data",
	})
	require.NoError(t, err)

	err = svc.Reject(ctx, tenantID, req.ID, dpoID, "Request falls outside KVKK m.11 scope for employees of this entity.")
	require.NoError(t, err)

	rejected, err := svc.Get(ctx, tenantID, req.ID)
	require.NoError(t, err)
	assert.Equal(t, dsr.StateRejected, rejected.State)
	require.NotNil(t, rejected.ClosedAt)
}

// TestDSR_Extend verifies SLA extension moves the deadline forward and records extension metadata.
func TestDSR_Extend(t *testing.T) {
	pool := testDB(t)
	log := testLogger(t)
	rec := testRecorder(pool, log)
	ctx := context.Background()

	tenantID := seedTenant(t, pool, "dsr-extend")
	employeeID := seedUser(t, pool, tenantID, "employee", "employee@extend.test")
	dpoID := seedUser(t, pool, tenantID, "dpo", "dpo@extend.test")

	store := dsr.NewStore(pool)
	svc := dsr.NewService(store, rec, &recordingNotifier{}, log)

	req, err := svc.Submit(ctx, dsr.SubmitInput{
		TenantID:       tenantID,
		EmployeeUserID: employeeID,
		RequestType:    dsr.RequestTypePortability,
	})
	require.NoError(t, err)

	originalDeadline := req.SLADeadline

	reason := "Awaiting data from a third-party processor under KVKK m.11."
	err = svc.Extend(ctx, tenantID, req.ID, dpoID, reason)
	require.NoError(t, err, "extension should succeed for an open DSR")

	extended, err := svc.Get(ctx, tenantID, req.ID)
	require.NoError(t, err)
	require.NotNil(t, extended.ExtendedAt, "extended_at should be set after extension")
	assert.True(t, extended.SLADeadline.After(originalDeadline),
		"deadline should be later than original after extension")
	assert.Equal(t, reason, *extended.ExtensionReason)
}

// TestDSR_List verifies listing by state filter.
func TestDSR_List(t *testing.T) {
	pool := testDB(t)
	log := testLogger(t)
	rec := testRecorder(pool, log)
	ctx := context.Background()

	tenantID := seedTenant(t, pool, "dsr-list")
	emp1 := seedUser(t, pool, tenantID, "employee", "emp1@list.test")
	emp2 := seedUser(t, pool, tenantID, "employee", "emp2@list.test")
	dpoID := seedUser(t, pool, tenantID, "dpo", "dpo@list.test")

	store := dsr.NewStore(pool)
	svc := dsr.NewService(store, rec, &recordingNotifier{}, log)

	// Submit 2 open DSRs.
	r1, err := svc.Submit(ctx, dsr.SubmitInput{TenantID: tenantID, EmployeeUserID: emp1, RequestType: dsr.RequestTypeAccess})
	require.NoError(t, err)
	r2, err := svc.Submit(ctx, dsr.SubmitInput{TenantID: tenantID, EmployeeUserID: emp2, RequestType: dsr.RequestTypeRectify})
	require.NoError(t, err)

	// Resolve one.
	require.NoError(t, svc.Respond(ctx, tenantID, r1.ID, dpoID, "dsr/artifact-r1.pdf"))

	// List all.
	all, err := svc.List(ctx, tenantID, nil)
	require.NoError(t, err)
	assert.Len(t, all, 2)

	// List open only.
	open, err := svc.List(ctx, tenantID, []dsr.State{dsr.StateOpen})
	require.NoError(t, err)
	assert.Len(t, open, 1)
	assert.Equal(t, r2.ID, open[0].ID)

	// List resolved.
	resolved, err := svc.List(ctx, tenantID, []dsr.State{dsr.StateResolved})
	require.NoError(t, err)
	assert.Len(t, resolved, 1)
	assert.Equal(t, r1.ID, resolved[0].ID)
}

// TestDSR_SLATick verifies the SLA tick transitions states correctly.
func TestDSR_SLATick(t *testing.T) {
	pool := testDB(t)
	log := testLogger(t)
	ctx := context.Background()

	tenantID := seedTenant(t, pool, "dsr-sla")
	employeeID := seedUser(t, pool, tenantID, "employee", "employee@sla.test")

	store := dsr.NewStore(pool)

	// Insert a DSR directly with a past SLA deadline (day 21 — should become at_risk).
	var id string
	err := pool.QueryRow(ctx,
		`INSERT INTO dsr_requests(id, tenant_id, employee_user_id, request_type, justification, state, sla_deadline)
		 VALUES(gen_random_uuid()::text, $1, $2, 'access', 'test', 'open',
		        now() - INTERVAL '21 days' + INTERVAL '30 days')
		 RETURNING id`,
		tenantID, employeeID,
	).Scan(&id)
	require.NoError(t, err)

	// Insert a DSR that is overdue (SLA deadline in the past).
	var overdueID string
	err = pool.QueryRow(ctx,
		`INSERT INTO dsr_requests(id, tenant_id, employee_user_id, request_type, justification, state, sla_deadline)
		 VALUES(gen_random_uuid()::text, $1, $2, 'erase', 'test', 'open', now() - INTERVAL '1 hour')
		 RETURNING id`,
		tenantID, employeeID,
	).Scan(&overdueID)
	require.NoError(t, err)

	// Run the SLA tick for this tenant.
	require.NoError(t, store.TickSLAs(ctx, tenantID))

	// Check transitions.
	atRisk, err := store.Get(ctx, id, tenantID)
	require.NoError(t, err)
	assert.Equal(t, dsr.StateAtRisk, atRisk.State)

	overdue, err := store.Get(ctx, overdueID, tenantID)
	require.NoError(t, err)
	assert.Equal(t, dsr.StateOverdue, overdue.State)
}

// TestDSR_AuditBeforeSideEffect verifies that the audit entry ID is captured
// in audit_chain_ref on the DSR row, proving audit-before-side-effect.
func TestDSR_AuditBeforeSideEffect(t *testing.T) {
	pool := testDB(t)
	log := testLogger(t)
	rec := testRecorder(pool, log)
	ctx := context.Background()

	tenantID := seedTenant(t, pool, "dsr-audit-order")
	employeeID := seedUser(t, pool, tenantID, "employee", "emp@audit.test")
	dpoID := seedUser(t, pool, tenantID, "dpo", "dpo@audit.test")

	store := dsr.NewStore(pool)
	svc := dsr.NewService(store, rec, &recordingNotifier{}, log)

	req, err := svc.Submit(ctx, dsr.SubmitInput{
		TenantID:       tenantID,
		EmployeeUserID: employeeID,
		RequestType:    dsr.RequestTypeObject,
		Justification:  "I object to processing",
	})
	require.NoError(t, err)

	// Capture the audit log row count before respond.
	var beforeCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM audit.audit_log WHERE tenant_id = $1`,
		tenantID,
	).Scan(&beforeCount))
	assert.GreaterOrEqual(t, beforeCount, 1, "audit entry for submission should exist")

	require.NoError(t, svc.Respond(ctx, tenantID, req.ID, dpoID, "dsr/artifact.pdf"))

	// The audit_chain_ref on the DSR should reference an audit entry.
	var chainRef *string
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT audit_chain_ref FROM dsr_requests WHERE id = $1`,
		req.ID,
	).Scan(&chainRef))
	require.NotNil(t, chainRef)
	assert.Contains(t, *chainRef, "audit:", "audit_chain_ref should reference an audit entry ID")

	// Total audit entries should have grown.
	var afterCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM audit.audit_log WHERE tenant_id = $1`,
		tenantID,
	).Scan(&afterCount))
	assert.Greater(t, afterCount, beforeCount)
}

// recordingNotifier captures notification calls for assertion in tests.
type recordingNotifier struct {
	dpoCalls      []string
	employeeCalls []string
	escalations   []string
}

func (r *recordingNotifier) NotifyDPO(_ context.Context, _, id, _ string) error {
	r.dpoCalls = append(r.dpoCalls, id)
	return nil
}

func (r *recordingNotifier) NotifyEmployee(_ context.Context, _, id, _, _ string) error {
	r.employeeCalls = append(r.employeeCalls, id)
	return nil
}

func (r *recordingNotifier) EscalateToDPOSecondary(_ context.Context, _, id string) error {
	r.escalations = append(r.escalations, id)
	return nil
}
