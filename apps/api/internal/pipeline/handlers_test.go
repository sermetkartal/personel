package pipeline

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/auth"
)

// ---- fakes ----------------------------------------------------------------

type fakeReader struct {
	listCalls    int
	getCalls     int
	deleteCalls  int
	lastParams   ListParams
	lastGetID    string
	lastDeleteID string
	messages     []*DLQMessage
	err          error
}

func (f *fakeReader) List(_ context.Context, params ListParams) (*ListResult, error) {
	f.listCalls++
	f.lastParams = params
	if f.err != nil {
		return nil, f.err
	}
	out := make([]*DLQMessage, 0, len(f.messages))
	for _, m := range f.messages {
		if !matchesListParams(m, params) {
			continue
		}
		out = append(out, m)
		if len(out) >= params.PageSize {
			break
		}
	}
	return &ListResult{Messages: out, TotalScanned: len(f.messages)}, nil
}

func (f *fakeReader) GetByID(_ context.Context, id string) (*DLQMessage, error) {
	f.getCalls++
	f.lastGetID = id
	if f.err != nil {
		return nil, f.err
	}
	for _, m := range f.messages {
		if FormatSeqToken(m.StreamSequence) == id {
			return m, nil
		}
	}
	return nil, ErrDLQNotFound
}

func (f *fakeReader) Delete(_ context.Context, id string) error {
	f.deleteCalls++
	f.lastDeleteID = id
	return nil
}

type fakePublisher struct {
	calls    int
	lastSubj string
	lastBody []byte
	lastHdrs map[string]string
	err      error
}

func (f *fakePublisher) PublishRaw(_ context.Context, subject string, headers map[string]string, payload []byte) error {
	f.calls++
	f.lastSubj = subject
	f.lastHdrs = headers
	f.lastBody = append([]byte(nil), payload...)
	return f.err
}

type fakeCH struct {
	count int
	err   error
}

func (f *fakeCH) Count(_ context.Context, _ CHReplayFilter) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.count, nil
}

type fakeRecorder struct {
	calls []audit.Entry
	err   error
}

func (f *fakeRecorder) Append(_ context.Context, e audit.Entry) (int64, error) {
	f.calls = append(f.calls, e)
	if f.err != nil {
		return 0, f.err
	}
	return int64(len(f.calls)), nil
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ---- helpers --------------------------------------------------------------

func adminPrincipal(tenantID string) *auth.Principal {
	return &auth.Principal{
		UserID:   "admin-42",
		TenantID: tenantID,
		Roles:    []auth.Role{auth.RoleAdmin},
	}
}

func dpoPrincipal(tenantID string) *auth.Principal {
	return &auth.Principal{
		UserID:   "dpo-7",
		TenantID: tenantID,
		Roles:    []auth.Role{auth.RoleDPO},
	}
}

func investigatorPrincipal(tenantID string) *auth.Principal {
	return &auth.Principal{
		UserID:   "inv-1",
		TenantID: tenantID,
		Roles:    []auth.Role{auth.RoleInvestigator},
	}
}

func sampleDLQ(seq uint64, tenant, kind string, failedAt time.Time) *DLQMessage {
	return &DLQMessage{
		OriginalSubject: "events.raw." + tenant + ".process.start",
		OriginalHeaders: map[string]string{
			"schema-version": "v1",
		},
		OriginalPayload: []byte{0x08, byte(seq)},
		ErrorKind:       kind,
		ErrorMessage:    "synthetic failure",
		FailedAt:        failedAt,
		RetryCount:      3,
		TenantID:        tenant,
		BatchID:         seq * 10,
		StreamSequence:  seq,
	}
}

// ---- ListDLQ tests --------------------------------------------------------

func TestListDLQ_FiltersByTenantFromPrincipal(t *testing.T) {
	now := time.Now().UTC()
	reader := &fakeReader{
		messages: []*DLQMessage{
			sampleDLQ(1, "tenant-a", DLQKindDecodeConst, now),
			sampleDLQ(2, "tenant-b", DLQKindDecodeConst, now),
		},
	}
	svc := NewService(reader, nil, nil, &fakeRecorder{}, silentLogger())

	res, err := svc.ListDLQ(context.Background(), adminPrincipal("tenant-a"), ListParams{PageSize: 10})
	if err != nil {
		t.Fatalf("ListDLQ: %v", err)
	}
	if len(res.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(res.Messages))
	}
	if res.Messages[0].TenantID != "tenant-a" {
		t.Errorf("leaked tenant-b to tenant-a principal")
	}
	if reader.lastParams.TenantID != "tenant-a" {
		t.Errorf("reader was called with tenant_id=%q, want tenant-a", reader.lastParams.TenantID)
	}
}

func TestListDLQ_AllTenantsRequiresDPO(t *testing.T) {
	reader := &fakeReader{}
	svc := NewService(reader, nil, nil, &fakeRecorder{}, silentLogger())

	_, err := svc.ListDLQ(context.Background(), adminPrincipal("t"), ListParams{AllTenants: true})
	if !errors.Is(err, ErrForbiddenAllTenants) {
		t.Errorf("admin with all_tenants should be forbidden, got %v", err)
	}

	_, err = svc.ListDLQ(context.Background(), dpoPrincipal("t"), ListParams{AllTenants: true})
	if err != nil {
		t.Errorf("DPO with all_tenants should be allowed, got %v", err)
	}
}

func TestListDLQ_PageSizeClamping(t *testing.T) {
	reader := &fakeReader{}
	svc := NewService(reader, nil, nil, &fakeRecorder{}, silentLogger())

	_, err := svc.ListDLQ(context.Background(), adminPrincipal("t"), ListParams{PageSize: 9999})
	if err != nil {
		t.Fatalf("ListDLQ: %v", err)
	}
	if reader.lastParams.PageSize != maxPageSize {
		t.Errorf("PageSize not clamped: got %d, want %d", reader.lastParams.PageSize, maxPageSize)
	}

	_, err = svc.ListDLQ(context.Background(), adminPrincipal("t"), ListParams{PageSize: 0})
	if err != nil {
		t.Fatalf("ListDLQ: %v", err)
	}
	if reader.lastParams.PageSize != defaultPageSize {
		t.Errorf("PageSize default not applied: got %d", reader.lastParams.PageSize)
	}
}

// ---- Replay DLQ tests -----------------------------------------------------

func TestReplay_DLQ_DryRun(t *testing.T) {
	now := time.Now().UTC()
	reader := &fakeReader{
		messages: []*DLQMessage{sampleDLQ(7, "tenant-a", DLQKindEnrichConst, now)},
	}
	pub := &fakePublisher{}
	rec := &fakeRecorder{}
	svc := NewService(reader, pub, nil, rec, silentLogger())

	res, err := svc.Replay(context.Background(), adminPrincipal("tenant-a"), ReplayRequest{
		Source:       ReplaySourceDLQ,
		DLQMessageID: "7",
		DryRun:       true,
	})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if res.Projected != 1 {
		t.Errorf("projected = %d, want 1", res.Projected)
	}
	if res.Published != 0 {
		t.Errorf("dry_run must not publish; got published=%d", res.Published)
	}
	if pub.calls != 0 {
		t.Errorf("publisher should not have been called on dry_run; calls=%d", pub.calls)
	}
	if len(rec.calls) != 1 {
		t.Fatalf("audit recorder should have been called once; got %d", len(rec.calls))
	}
	if rec.calls[0].Action != audit.ActionPipelineReplay {
		t.Errorf("audit action = %q, want %q", rec.calls[0].Action, audit.ActionPipelineReplay)
	}
}

func TestReplay_DLQ_Publish(t *testing.T) {
	now := time.Now().UTC()
	reader := &fakeReader{
		messages: []*DLQMessage{sampleDLQ(7, "tenant-a", DLQKindEnrichConst, now)},
	}
	pub := &fakePublisher{}
	svc := NewService(reader, pub, nil, &fakeRecorder{}, silentLogger())

	res, err := svc.Replay(context.Background(), adminPrincipal("tenant-a"), ReplayRequest{
		Source:       ReplaySourceDLQ,
		DLQMessageID: "7",
		DryRun:       false,
	})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if res.Published != 1 {
		t.Errorf("published = %d, want 1", res.Published)
	}
	if pub.calls != 1 {
		t.Errorf("publisher calls = %d, want 1", pub.calls)
	}
	if pub.lastSubj != "events.raw.tenant-a.process.start" {
		t.Errorf("subject = %q", pub.lastSubj)
	}
	if pub.lastHdrs["schema-version"] != "v1" {
		t.Errorf("schema-version header not preserved: %q", pub.lastHdrs["schema-version"])
	}
}

func TestReplay_DLQ_TenantIsolation(t *testing.T) {
	now := time.Now().UTC()
	reader := &fakeReader{
		messages: []*DLQMessage{sampleDLQ(7, "tenant-b", DLQKindEnrichConst, now)},
	}
	pub := &fakePublisher{}
	svc := NewService(reader, pub, nil, &fakeRecorder{}, silentLogger())

	// Admin of tenant-a should NOT be able to replay a tenant-b entry.
	_, err := svc.Replay(context.Background(), adminPrincipal("tenant-a"), ReplayRequest{
		Source:       ReplaySourceDLQ,
		DLQMessageID: "7",
	})
	if !errors.Is(err, ErrTenantIsolation) {
		t.Errorf("expected ErrTenantIsolation, got %v", err)
	}
	if pub.calls != 0 {
		t.Errorf("publisher should not have been called")
	}
}

func TestReplay_DLQ_DPOAllTenants(t *testing.T) {
	now := time.Now().UTC()
	reader := &fakeReader{
		messages: []*DLQMessage{sampleDLQ(7, "tenant-b", DLQKindEnrichConst, now)},
	}
	pub := &fakePublisher{}
	svc := NewService(reader, pub, nil, &fakeRecorder{}, silentLogger())

	_, err := svc.Replay(context.Background(), dpoPrincipal("tenant-a"), ReplayRequest{
		Source:       ReplaySourceDLQ,
		DLQMessageID: "7",
		AllTenants:   true,
	})
	if err != nil {
		t.Errorf("DPO + all_tenants should succeed, got %v", err)
	}
	if pub.calls != 1 {
		t.Errorf("publisher calls = %d, want 1", pub.calls)
	}
}

// ---- Replay CH tests ------------------------------------------------------

func TestReplay_CH_DryRunCount(t *testing.T) {
	ch := &fakeCH{count: 42}
	svc := NewService(nil, nil, ch, &fakeRecorder{}, silentLogger())

	res, err := svc.Replay(context.Background(), dpoPrincipal("tenant-a"), ReplayRequest{
		Source: ReplaySourceClickHouse,
		CHFilter: &CHReplayFilter{
			From: time.Now().Add(-1 * time.Hour),
			To:   time.Now(),
		},
		DryRun: true,
	})
	if err != nil {
		t.Fatalf("Replay CH: %v", err)
	}
	if res.Projected != 42 {
		t.Errorf("projected = %d, want 42", res.Projected)
	}
	if res.Published != 0 {
		t.Errorf("CH replay must never publish in Phase 1")
	}
}

func TestReplay_CH_HardCap(t *testing.T) {
	ch := &fakeCH{count: 99999}
	svc := NewService(nil, nil, ch, &fakeRecorder{}, silentLogger())

	res, err := svc.Replay(context.Background(), dpoPrincipal("t"), ReplayRequest{
		Source: ReplaySourceClickHouse,
		CHFilter: &CHReplayFilter{
			From: time.Now().Add(-1 * time.Hour),
			To:   time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("Replay CH: %v", err)
	}
	if res.Projected != maxReplayCHEvents {
		t.Errorf("projected = %d, want clamped to %d", res.Projected, maxReplayCHEvents)
	}
}

// ---- Replay validation tests ---------------------------------------------

func TestReplay_RejectsUnknownSource(t *testing.T) {
	svc := NewService(&fakeReader{}, &fakePublisher{}, nil, &fakeRecorder{}, silentLogger())
	_, err := svc.Replay(context.Background(), adminPrincipal("t"), ReplayRequest{
		Source: "magic",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestReplay_RejectsDLQWithoutID(t *testing.T) {
	svc := NewService(&fakeReader{}, &fakePublisher{}, nil, &fakeRecorder{}, silentLogger())
	_, err := svc.Replay(context.Background(), adminPrincipal("t"), ReplayRequest{
		Source: ReplaySourceDLQ,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestReplay_RejectsCHWithoutFilter(t *testing.T) {
	svc := NewService(nil, nil, &fakeCH{}, &fakeRecorder{}, silentLogger())
	_, err := svc.Replay(context.Background(), adminPrincipal("t"), ReplayRequest{
		Source: ReplaySourceClickHouse,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
}

// ---- Handler tests --------------------------------------------------------

func TestListDLQHandler_AuditRequired(t *testing.T) {
	svc := NewService(&fakeReader{}, nil, nil, &fakeRecorder{}, silentLogger())
	h := ListDLQHandler(svc)

	// No principal in context — should 401.
	req := httptest.NewRequest(http.MethodGet, "/v1/pipeline/dlq", nil)
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("code = %d, want 401", w.Code)
	}
}

func TestReplayHandler_WritesAuditAndPublishes(t *testing.T) {
	now := time.Now().UTC()
	reader := &fakeReader{
		messages: []*DLQMessage{sampleDLQ(9, "tenant-a", DLQKindDecodeConst, now)},
	}
	pub := &fakePublisher{}
	rec := &fakeRecorder{}
	svc := NewService(reader, pub, nil, rec, silentLogger())
	h := ReplayHandler(svc)

	body := ReplayRequest{
		Source:       ReplaySourceDLQ,
		DLQMessageID: "9",
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/pipeline/replay", bytes.NewReader(raw))
	ctx := auth.WithPrincipal(req.Context(), adminPrincipal("tenant-a"))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	if pub.calls != 1 {
		t.Errorf("publisher calls = %d, want 1", pub.calls)
	}
	if len(rec.calls) != 1 {
		t.Errorf("audit calls = %d, want 1", len(rec.calls))
	}
	if rec.calls[0].Details["dlq_message_id"] != "9" {
		t.Errorf("audit details dlq_message_id = %v", rec.calls[0].Details["dlq_message_id"])
	}
}

func TestReplayHandler_RejectsMalformedJSON(t *testing.T) {
	svc := NewService(&fakeReader{}, &fakePublisher{}, nil, &fakeRecorder{}, silentLogger())
	h := ReplayHandler(svc)

	req := httptest.NewRequest(http.MethodPost, "/v1/pipeline/replay", bytes.NewReader([]byte("{bad json")))
	ctx := auth.WithPrincipal(req.Context(), adminPrincipal("tenant-a"))
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	h(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", w.Code)
	}
}

func TestReplayHandler_InvestigatorIsNotAllowedToReplay(t *testing.T) {
	// Router gates /replay to admin+dpo; service layer relies on that.
	// This test is a structural reminder that investigator principals
	// don't have a direct Replay call path — the service still runs
	// (if it's invoked) because its only RBAC check is tenant +
	// all_tenants. The router-level exclusion is asserted in the
	// server_test. Here we just verify that the service itself does
	// not grant investigator anything special.
	svc := NewService(&fakeReader{}, &fakePublisher{}, nil, &fakeRecorder{}, silentLogger())
	_, err := svc.Replay(context.Background(), investigatorPrincipal("t"), ReplayRequest{
		Source:     ReplaySourceDLQ,
		AllTenants: true,
	})
	if !errors.Is(err, ErrForbiddenAllTenants) {
		t.Errorf("investigator should not be able to use all_tenants, got %v", err)
	}
}

// Test-only constants to avoid importing the real ones for DLQ kinds.
// These mirror apps/gateway/internal/enricher/dlq.go DLQKind* constants.
const (
	DLQKindDecodeConst = "decode"
	DLQKindEnrichConst = "enrich"
)
