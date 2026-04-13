package endpoint

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/auth"
)

// ---------------------------------------------------------------------------
// Fakes
// ---------------------------------------------------------------------------

// fakeStore is an in-memory CommandStore used by the unit tests. It
// tracks (1) which endpoints exist in which tenant, (2) which endpoints
// are under legal hold, and (3) every Create + UpdateState call so the
// tests can assert ordering + rollback behaviour.
type fakeStore struct {
	mu         sync.Mutex
	byID       map[string]*Command
	endpoints  map[string]string // endpointID → tenantID
	legalHolds map[string]bool   // endpointID → under hold
	createErr  error
	updateErr  error
	seq        int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		byID:       map[string]*Command{},
		endpoints:  map[string]string{},
		legalHolds: map[string]bool{},
	}
}

func (s *fakeStore) Create(_ context.Context, c *Command) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.createErr != nil {
		return s.createErr
	}
	s.seq++
	c.ID = "cmd-" + strconv.Itoa(s.seq)
	c.State = CommandStatePending
	// Deep-copy the input so test assertions mirror DB semantics.
	copied := *c
	s.byID[c.ID] = &copied
	return nil
}

func (s *fakeStore) UpdateState(_ context.Context, id, state, errorMsg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.updateErr != nil {
		return s.updateErr
	}
	c, ok := s.byID[id]
	if !ok {
		return errors.New("no such command")
	}
	c.State = CommandState(state)
	if errorMsg != "" {
		msg := errorMsg
		c.ErrorMessage = &msg
	}
	now := time.Now().UTC()
	switch CommandState(state) {
	case CommandStateAcknowledged:
		c.AcknowledgedAt = &now
	case CommandStateCompleted, CommandStateFailed, CommandStateTimeout:
		c.CompletedAt = &now
	}
	return nil
}

func (s *fakeStore) GetByID(_ context.Context, tenantID, id string) (*Command, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.byID[id]
	if !ok {
		return nil, nil
	}
	if c.TenantID != tenantID {
		return nil, nil
	}
	copied := *c
	return &copied, nil
}

func (s *fakeStore) ListByEndpoint(_ context.Context, tenantID, endpointID string, limit int) ([]Command, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Command
	for _, c := range s.byID {
		if c.TenantID == tenantID && c.EndpointID == endpointID {
			out = append(out, *c)
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *fakeStore) ListByTenant(_ context.Context, tenantID string, _, _ int) ([]Command, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Command
	for _, c := range s.byID {
		if c.TenantID == tenantID {
			out = append(out, *c)
		}
	}
	return out, len(out), nil
}

func (s *fakeStore) EndpointExists(_ context.Context, tenantID, endpointID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.endpoints[endpointID]
	return ok && t == tenantID, nil
}

func (s *fakeStore) IsUnderLegalHold(_ context.Context, _, endpointID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.legalHolds[endpointID], nil
}

func (s *fakeStore) countByState(state CommandState) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, c := range s.byID {
		if c.State == state {
			n++
		}
	}
	return n
}

// fakePublisher records every Publish call and can be made to fail.
type fakePublisher struct {
	mu       sync.Mutex
	subjects []string
	payloads [][]byte
	err      error
}

func (p *fakePublisher) Publish(_ context.Context, subject string, data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.subjects = append(p.subjects, subject)
	// Copy so subsequent mutations on the caller side don't taint.
	cp := make([]byte, len(data))
	copy(cp, data)
	p.payloads = append(p.payloads, cp)
	return nil
}

// fakeAuditor satisfies the commandAuditor interface used by the
// service. It returns increasing ids so the service can write audit
// rows without a real pool. err simulates a hash-chain failure.
type fakeAuditor struct {
	mu      sync.Mutex
	entries []audit.Entry
	err     error
	nextID  int64
}

func (a *fakeAuditor) Append(_ context.Context, e audit.Entry) (int64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return 0, a.err
	}
	a.nextID++
	a.entries = append(a.entries, e)
	return a.nextID, nil
}

func (a *fakeAuditor) hasAction(action audit.Action) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, e := range a.entries {
		if e.Action == action {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newTestService(t *testing.T) (*CommandService, *fakeStore, *fakePublisher, *fakeAuditor) {
	t.Helper()
	store := newFakeStore()
	pub := &fakePublisher{}
	rec := &fakeAuditor{}
	svc := NewCommandService(store, pub, rec, quietLogger())
	return svc, store, pub, rec
}

func principal(tenant, user string) *auth.Principal {
	return &auth.Principal{TenantID: tenant, UserID: user, Roles: []auth.Role{auth.RoleAdmin}}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestIssueWipeRequiresReason(t *testing.T) {
	svc, store, _, _ := newTestService(t)
	store.endpoints["ep-1"] = "tenant-a"

	_, err := svc.IssueWipe(context.Background(), principal("tenant-a", "admin-1"), "ep-1", "")
	if !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("expected ErrReasonRequired, got %v", err)
	}
}

func TestIssueWipeTenantMismatchReturnsNotFound(t *testing.T) {
	svc, store, _, _ := newTestService(t)
	// Endpoint exists but belongs to a DIFFERENT tenant.
	store.endpoints["ep-1"] = "tenant-other"

	_, err := svc.IssueWipe(context.Background(), principal("tenant-a", "admin-1"), "ep-1", "incident 42")
	if !errors.Is(err, ErrEndpointNotFound) {
		t.Fatalf("expected ErrEndpointNotFound (enumeration prevention), got %v", err)
	}
}

func TestIssueWipeRejectedUnderLegalHold(t *testing.T) {
	svc, store, pub, rec := newTestService(t)
	store.endpoints["ep-1"] = "tenant-a"
	store.legalHolds["ep-1"] = true

	_, err := svc.IssueWipe(context.Background(), principal("tenant-a", "admin-1"), "ep-1", "evidence destroy")
	if !errors.Is(err, ErrUnderLegalHold) {
		t.Fatalf("expected ErrUnderLegalHold, got %v", err)
	}
	// No row should have been created, no publish, no audit.
	if store.countByState(CommandStatePending) != 0 {
		t.Errorf("expected no pending commands, got %d", store.countByState(CommandStatePending))
	}
	if len(pub.subjects) != 0 {
		t.Errorf("expected no publish, got %d", len(pub.subjects))
	}
	if len(rec.entries) != 0 {
		t.Errorf("expected no audit entries, got %d", len(rec.entries))
	}
}

func TestIssueDeactivateAllowedUnderLegalHold(t *testing.T) {
	// Deactivate is reversible — legal hold does NOT block it.
	svc, store, pub, rec := newTestService(t)
	store.endpoints["ep-1"] = "tenant-a"
	store.legalHolds["ep-1"] = true

	cmd, err := svc.IssueDeactivate(context.Background(), principal("tenant-a", "admin-1"), "ep-1", "ops maintenance")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if cmd.State != CommandStatePending {
		t.Errorf("expected pending, got %s", cmd.State)
	}
	if len(pub.subjects) != 1 {
		t.Fatalf("expected 1 publish, got %d", len(pub.subjects))
	}
	if !rec.hasAction(audit.ActionEndpointDeactivate) {
		t.Errorf("expected deactivate audit action, got %v", rec.entries)
	}
}

func TestIssueWipeHappyPathAuditsAndPublishes(t *testing.T) {
	svc, store, pub, rec := newTestService(t)
	store.endpoints["ep-1"] = "tenant-a"

	cmd, err := svc.IssueWipe(context.Background(), principal("tenant-a", "admin-1"), "ep-1", "KVKK m.7 crypto-erase")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cmd.Kind != CommandWipe {
		t.Errorf("expected wipe kind, got %s", cmd.Kind)
	}
	// Verify subject + payload shape.
	if len(pub.subjects) != 1 {
		t.Fatalf("expected 1 publish, got %d", len(pub.subjects))
	}
	wantSubject := "endpoints.command.tenant-a.ep-1"
	if pub.subjects[0] != wantSubject {
		t.Errorf("subject = %q, want %q", pub.subjects[0], wantSubject)
	}
	var payload CommandPayload
	if err := json.Unmarshal(pub.payloads[0], &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload.CommandID != cmd.ID {
		t.Errorf("payload command_id = %q, want %q", payload.CommandID, cmd.ID)
	}
	if payload.Kind != CommandWipe {
		t.Errorf("payload kind = %q, want %q", payload.Kind, CommandWipe)
	}
	if !payload.RequireAck {
		t.Error("expected require_ack = true")
	}
	// Audit row must exist with the wipe action.
	if !rec.hasAction(audit.ActionEndpointWipe) {
		t.Errorf("expected wipe audit action, got %d entries", len(rec.entries))
	}
}

func TestIssueWipePublishFailureRollsBackRowState(t *testing.T) {
	svc, store, pub, rec := newTestService(t)
	store.endpoints["ep-1"] = "tenant-a"
	pub.err = errors.New("nats down")

	_, err := svc.IssueWipe(context.Background(), principal("tenant-a", "admin-1"), "ep-1", "retry me")
	if !errors.Is(err, ErrPublishFailed) {
		t.Fatalf("expected ErrPublishFailed, got %v", err)
	}
	// Row should exist but in failed state, not pending.
	if store.countByState(CommandStatePending) != 0 {
		t.Errorf("expected 0 pending, got %d", store.countByState(CommandStatePending))
	}
	if store.countByState(CommandStateFailed) != 1 {
		t.Errorf("expected 1 failed, got %d", store.countByState(CommandStateFailed))
	}
	// No audit entry should have been written (audit only runs on
	// successful publish).
	if rec.hasAction(audit.ActionEndpointWipe) {
		t.Error("expected no wipe audit action on publish failure")
	}
}

func TestBulkRejectsOverLimit(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	ids := make([]string, BulkLimit+1)
	for i := range ids {
		ids[i] = "ep-" + strconv.Itoa(i)
	}
	_, err := svc.BulkOperation(context.Background(), principal("tenant-a", "admin-1"), "wipe", ids, "over")
	if !errors.Is(err, ErrBulkLimitExceeded) {
		t.Fatalf("expected ErrBulkLimitExceeded, got %v", err)
	}
}

func TestBulkRejectsUnknownOperation(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	_, err := svc.BulkOperation(context.Background(), principal("tenant-a", "admin-1"), "nuke", []string{"ep-1"}, "x")
	if !errors.Is(err, ErrUnknownOperation) {
		t.Fatalf("expected ErrUnknownOperation, got %v", err)
	}
}

func TestBulkMixedSuccessFailure(t *testing.T) {
	svc, store, _, rec := newTestService(t)
	// ep-1 exists in tenant-a, ep-2 does not, ep-3 exists but under legal hold (wipe blocked).
	store.endpoints["ep-1"] = "tenant-a"
	store.endpoints["ep-3"] = "tenant-a"
	store.legalHolds["ep-3"] = true

	results, err := svc.BulkOperation(context.Background(),
		principal("tenant-a", "admin-1"),
		"wipe",
		[]string{"ep-1", "ep-2", "ep-3"},
		"pilot cleanup")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	if !results[0].Success {
		t.Errorf("ep-1 should have succeeded, got %v", results[0].Error)
	}
	if results[1].Success {
		t.Error("ep-2 should have failed (not in tenant)")
	}
	if results[2].Success {
		t.Error("ep-3 should have failed (legal hold)")
	}
	// Bulk summary audit row must exist.
	if !rec.hasAction(audit.ActionEndpointCommandBulk) {
		t.Error("expected bulk summary audit action")
	}
}

func TestAcknowledgeHappyPath(t *testing.T) {
	svc, store, _, rec := newTestService(t)
	store.endpoints["ep-1"] = "tenant-a"
	cmd, err := svc.IssueDeactivate(context.Background(), principal("tenant-a", "admin-1"), "ep-1", "x")
	if err != nil {
		t.Fatalf("seed issue: %v", err)
	}

	if err := svc.Acknowledge(context.Background(), "tenant-a", cmd.ID, string(CommandStateCompleted), ""); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}

	// Second Acknowledge is idempotent (terminal state).
	if err := svc.Acknowledge(context.Background(), "tenant-a", cmd.ID, string(CommandStateCompleted), ""); err != nil {
		t.Fatalf("duplicate acknowledge should be no-op, got %v", err)
	}

	// State must now be completed.
	got, _ := store.GetByID(context.Background(), "tenant-a", cmd.ID)
	if got.State != CommandStateCompleted {
		t.Errorf("expected completed, got %s", got.State)
	}
	if !rec.hasAction(audit.ActionEndpointCommandAck) {
		t.Error("expected ack audit action")
	}
}

func TestAcknowledgeRejectsUnknownState(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	if err := svc.Acknowledge(context.Background(), "tenant-a", "cmd-missing", "invalid", ""); err == nil {
		t.Fatal("expected error for invalid state")
	}
}

func TestAcknowledgeRejectsUnknownCommand(t *testing.T) {
	svc, _, _, _ := newTestService(t)
	err := svc.Acknowledge(context.Background(), "tenant-a", "cmd-999", string(CommandStateCompleted), "")
	if !errors.Is(err, ErrEndpointNotFound) {
		t.Fatalf("expected ErrEndpointNotFound, got %v", err)
	}
}

func TestListCommandsByEndpointTenantIsolated(t *testing.T) {
	svc, store, _, _ := newTestService(t)
	store.endpoints["ep-1"] = "tenant-a"
	if _, err := svc.IssueDeactivate(context.Background(), principal("tenant-a", "admin-1"), "ep-1", "x"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Same endpoint id from a different tenant must 404 (enumeration).
	_, err := svc.ListCommandsByEndpoint(context.Background(), "tenant-other", "ep-1", 10)
	if !errors.Is(err, ErrEndpointNotFound) {
		t.Fatalf("expected ErrEndpointNotFound, got %v", err)
	}

	items, err := svc.ListCommandsByEndpoint(context.Background(), "tenant-a", "ep-1", 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Errorf("expected 1 command, got %d", len(items))
	}
}
