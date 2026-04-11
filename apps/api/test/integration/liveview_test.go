//go:build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/liveview"
	natspkg "github.com/personel/api/internal/nats"
	vaultpkg "github.com/personel/api/internal/vault"
)

// stubNATSPublisher records published commands without connecting to NATS.
type stubNATSPublisher struct {
	starts []natspkg.LiveViewStartCommand
	stops  []natspkg.LiveViewStopCommand
}

func (s *stubNATSPublisher) PublishLiveViewStart(_ context.Context, _, _ string, cmd natspkg.LiveViewStartCommand) error {
	s.starts = append(s.starts, cmd)
	return nil
}

func (s *stubNATSPublisher) PublishLiveViewStop(_ context.Context, _, _ string, cmd natspkg.LiveViewStopCommand) error {
	s.stops = append(s.stops, cmd)
	return nil
}

// stubLiveKitMinter records rooms created and tokens minted.
type stubLiveKitMinter struct {
	rooms []string
}

func (s *stubLiveKitMinter) MintAdminToken(room string, _ time.Duration) (string, error) {
	return "admin-token-" + room, nil
}

func (s *stubLiveKitMinter) MintAgentToken(room, sessionID string, _ time.Duration) (string, error) {
	return "agent-token-" + sessionID, nil
}

func (s *stubLiveKitMinter) CreateRoom(_ context.Context, room string) error {
	s.rooms = append(s.rooms, room)
	return nil
}

// seedEndpoint inserts an endpoint row and returns its UUID.
func seedEndpoint(t *testing.T, pool *pgxpool.Pool, tenantID, hostname string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := pool.QueryRow(ctx,
		`INSERT INTO endpoints(tenant_id, hostname, status) VALUES($1, $2, 'active') RETURNING id`,
		tenantID, hostname,
	).Scan(&id)
	require.NoError(t, err)
	return id
}

// TestLiveView_RequestApproveEnd tests the full happy path:
// request (Admin) → approve (HR, different user) → end.
func TestLiveView_RequestApproveEnd(t *testing.T) {
	pool := testDB(t)
	log := testLogger(t)
	rec := testRecorder(pool, log)
	ctx := context.Background()

	tenantID := seedTenant(t, pool, "liveview-happy")
	adminID := seedUser(t, pool, tenantID, "admin", "admin@lv.test")
	hrID := seedUser(t, pool, tenantID, "hr", "hr@lv.test")
	endpointID := seedEndpoint(t, pool, tenantID, "TEST-WS-1")

	lk := &stubLiveKitMinter{}
	natsPub := &stubNATSPublisher{}
	store := liveview.NewStore(pool)
	svc := liveview.NewService(store, rec, natsPub, vaultpkg.NewStubClient(), lk, liveview.ServiceConfig{
		LiveKitHost:         "http://localhost:7880",
		MaxDuration:         15 * time.Minute,
		ApprovalTimeout:     10 * time.Minute,
		NATSLiveViewSubject: "liveview.v1",
	}, log)

	adminP := &auth.Principal{UserID: adminID, TenantID: tenantID, Roles: []auth.Role{auth.RoleAdmin}}
	hrP := &auth.Principal{UserID: hrID, TenantID: tenantID, Roles: []auth.Role{auth.RoleHR}}

	// ── Request ─────────────────────────────────────────────────────────────
	sess, err := svc.RequestLiveView(ctx, adminP, endpointID,
		"security-review", "Investigating unusual login pattern", 1800)
	require.NoError(t, err, "request live view")
	assert.Equal(t, liveview.StateRequested, sess.State)
	assert.Equal(t, adminID, sess.RequesterID)

	// ── Approve (HR, different user from requester) ──────────────────────────
	approved, err := svc.Approve(ctx, hrP, sess.ID, "approved for security review")
	require.NoError(t, err, "approve live view")
	assert.Equal(t, liveview.StateApproved, approved.State)
	require.NotNil(t, approved.ApproverID)
	assert.Equal(t, hrID, *approved.ApproverID)

	// LiveKit room should have been created.
	assert.Len(t, lk.rooms, 1, "LiveKit room should be created on approval")

	// NATS start command should have been published.
	assert.Len(t, natsPub.starts, 1, "NATS start command should be published")
	assert.Equal(t, sess.ID, natsPub.starts[0].SessionID)

	// ── Simulate agent joining (gateway calls AgentStarted) ─────────────────
	require.NoError(t, svc.AgentStarted(ctx, tenantID, sess.ID))

	active, err := store.Get(ctx, sess.ID, tenantID)
	require.NoError(t, err)
	assert.Equal(t, liveview.StateActive, active.State)

	// ── End session ─────────────────────────────────────────────────────────
	require.NoError(t, svc.EndSession(ctx, adminP, sess.ID))

	ended, err := store.Get(ctx, sess.ID, tenantID)
	require.NoError(t, err)
	assert.Equal(t, liveview.StateEnded, ended.State)
	require.NotNil(t, ended.EndedAt)
}

// TestLiveView_DualControlEnforced verifies the same user cannot approve their own request.
func TestLiveView_DualControlEnforced(t *testing.T) {
	pool := testDB(t)
	log := testLogger(t)
	rec := testRecorder(pool, log)
	ctx := context.Background()

	tenantID := seedTenant(t, pool, "liveview-dualctrl")
	hrAdminID := seedUser(t, pool, tenantID, "admin", "hradmin@lv.test")
	endpointID := seedEndpoint(t, pool, tenantID, "TEST-WS-2")

	svc := liveview.NewService(
		liveview.NewStore(pool), rec, &stubNATSPublisher{},
		vaultpkg.NewStubClient(), &stubLiveKitMinter{},
		liveview.ServiceConfig{
			LiveKitHost:  "http://localhost:7880",
			MaxDuration:  15 * time.Minute,
			ApprovalTimeout: 10 * time.Minute,
		}, log,
	)

	// A user who holds both Admin and HR roles.
	dualRoleP := &auth.Principal{
		UserID:   hrAdminID,
		TenantID: tenantID,
		Roles:    []auth.Role{auth.RoleAdmin, auth.RoleHR},
	}

	sess, err := svc.RequestLiveView(ctx, dualRoleP, endpointID,
		"test-reason", "test justification", 900)
	require.NoError(t, err)

	// Same user tries to approve their own request — must fail.
	_, err = svc.Approve(ctx, dualRoleP, sess.ID, "self-approval attempt")
	require.Error(t, err, "self-approval must be rejected")
	assert.Contains(t, err.Error(), "approver")
}

// TestLiveView_HardCapEnforced verifies that duration > 3600s is rejected at request time.
func TestLiveView_HardCapEnforced(t *testing.T) {
	pool := testDB(t)
	log := testLogger(t)
	rec := testRecorder(pool, log)
	ctx := context.Background()

	tenantID := seedTenant(t, pool, "liveview-cap")
	adminID := seedUser(t, pool, tenantID, "admin", "admin@cap.test")
	endpointID := seedEndpoint(t, pool, tenantID, "TEST-WS-3")

	svc := liveview.NewService(
		liveview.NewStore(pool), rec, &stubNATSPublisher{},
		vaultpkg.NewStubClient(), &stubLiveKitMinter{},
		liveview.ServiceConfig{
			LiveKitHost:  "http://localhost:7880",
			MaxDuration:  15 * time.Minute,
			ApprovalTimeout: 10 * time.Minute,
		}, log,
	)

	_, err := svc.RequestLiveView(ctx,
		&auth.Principal{UserID: adminID, TenantID: tenantID, Roles: []auth.Role{auth.RoleAdmin}},
		endpointID, "test-reason", "test justification",
		3601, // 1 second over the 3600s hard cap
	)
	require.Error(t, err, "duration > 3600s must be rejected")
	assert.Contains(t, err.Error(), "hard cap")
}

// TestLiveView_NonHRCannotApprove verifies that a Manager role cannot approve.
func TestLiveView_NonHRCannotApprove(t *testing.T) {
	pool := testDB(t)
	log := testLogger(t)
	rec := testRecorder(pool, log)
	ctx := context.Background()

	tenantID := seedTenant(t, pool, "liveview-nonhr")
	managerID := seedUser(t, pool, tenantID, "manager", "mgr@lv.test")
	adminID := seedUser(t, pool, tenantID, "admin", "adm@nonhr.test")
	endpointID := seedEndpoint(t, pool, tenantID, "TEST-WS-4")

	svc := liveview.NewService(
		liveview.NewStore(pool), rec, &stubNATSPublisher{},
		vaultpkg.NewStubClient(), &stubLiveKitMinter{},
		liveview.ServiceConfig{
			LiveKitHost:  "http://localhost:7880",
			MaxDuration:  15 * time.Minute,
			ApprovalTimeout: 10 * time.Minute,
		}, log,
	)

	sess, err := svc.RequestLiveView(ctx,
		&auth.Principal{UserID: adminID, TenantID: tenantID, Roles: []auth.Role{auth.RoleAdmin}},
		endpointID, "test", "test justification", 900)
	require.NoError(t, err)

	_, err = svc.Approve(ctx,
		&auth.Principal{UserID: managerID, TenantID: tenantID, Roles: []auth.Role{auth.RoleManager}},
		sess.ID, "manager trying to approve")
	require.Error(t, err, "manager (non-HR) must not be able to approve live view")
}

// TestLiveView_StateTransitions verifies the state machine rejects invalid transitions.
func TestLiveView_StateTransitions(t *testing.T) {
	// Test the pure state machine logic — no DB needed.
	tests := []struct {
		current liveview.State
		event   string
		wantErr bool
	}{
		{liveview.StateRequested, "approve", false},
		{liveview.StateRequested, "deny", false},
		{liveview.StateRequested, "expire", false},
		{liveview.StateApproved, "agent_start", false},
		{liveview.StateApproved, "agent_fail", false},
		{liveview.StateActive, "end", false},
		{liveview.StateActive, "hr_terminate", false},
		{liveview.StateActive, "dpo_terminate", false},
		// Invalid transitions
		{liveview.StateEnded, "approve", true},
		{liveview.StateApproved, "approve", true},
		{liveview.StateDenied, "agent_start", true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(string(tt.current)+"/"+tt.event, func(t *testing.T) {
			_, err := liveview.Transition(tt.current, tt.event)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestLiveView_TerminalStateIsTerminal verifies IsTerminal() on all terminal states.
func TestLiveView_TerminalStateIsTerminal(t *testing.T) {
	terminal := []liveview.State{
		liveview.StateEnded,
		liveview.StateDenied,
		liveview.StateExpired,
		liveview.StateFailed,
		liveview.StateTerminatedByHR,
		liveview.StateTerminatedByDPO,
	}
	nonTerminal := []liveview.State{
		liveview.StateRequested,
		liveview.StateApproved,
		liveview.StateActive,
	}

	for _, s := range terminal {
		assert.True(t, s.IsTerminal(), "state %s should be terminal", s)
	}
	for _, s := range nonTerminal {
		assert.False(t, s.IsTerminal(), "state %s should NOT be terminal", s)
	}
}
