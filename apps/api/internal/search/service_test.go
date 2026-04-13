package search

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/personel/api/internal/audit"
)

// fakeClient is the queryClient stand-in used by every service test.
// It records the tenantID it was called with so tests can assert the
// service never forwards a client-supplied tenant.
type fakeClient struct {
	lastTenantID string
	lastAuditQ   AuditQuery
	lastEventQ   EventQuery
	auditErr     error
	eventErr     error
}

func (f *fakeClient) SearchAudit(_ context.Context, tenantID string, q AuditQuery) (*AuditResult, error) {
	f.lastTenantID = tenantID
	f.lastAuditQ = q
	if f.auditErr != nil {
		return nil, f.auditErr
	}
	return &AuditResult{Hits: []AuditHit{}, Total: 0, Page: q.Page, Size: q.PageSize}, nil
}

func (f *fakeClient) SearchEvents(_ context.Context, tenantID string, q EventQuery) (*EventResult, error) {
	f.lastTenantID = tenantID
	f.lastEventQ = q
	if f.eventErr != nil {
		return nil, f.eventErr
	}
	return &EventResult{Hits: []EventHit{}, Total: 0, Page: q.Page, Size: q.PageSize}, nil
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestService(fc *fakeClient) *Service {
	return newServiceWithClient(fc, quietLogger())
}

// TestTenantIDCannotBeBypassed is the single most important test in this
// package: a malicious caller with one tenant's JWT cannot read another
// tenant's rows by stuffing a tenant_id into the "q" string. The service
// must forward whatever tenantID was passed by the handler layer and
// the handler must only take it from the verified Principal.
func TestTenantIDCannotBeBypassed(t *testing.T) {
	fc := &fakeClient{}
	svc := newTestService(fc)
	ctx := context.Background()

	_, err := svc.SearchAudit(ctx, "tenant-A", AuditQuery{
		Q: `tenant_id:"tenant-B" OR action:anything`,
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if fc.lastTenantID != "tenant-A" {
		t.Fatalf("service forwarded wrong tenant: got %q, want tenant-A", fc.lastTenantID)
	}
	// Even though the query string contains "tenant_id:tenant-B", the
	// service has NO path to replace the tenant argument. The client
	// tests below verify buildQuery() injects the hard filter clause.
}

// TestBuildQueryInjectsTenantIDFilter asserts the query builder always
// places a tenant_id term clause at the top of the filter list, no
// matter what was passed in extraFilters.
func TestBuildQueryInjectsTenantIDFilter(t *testing.T) {
	body := buildQuery(
		"tenant-A",
		"free text",
		[]map[string]any{
			// Pretend a malicious filter was somehow smuggled in.
			{"term": map[string]any{"tenant_id": "tenant-EVIL"}},
		},
		[]string{"action"},
		1, 25, "timestamp",
	)
	query, ok := body["query"].(map[string]any)
	if !ok {
		t.Fatalf("query shape unexpected: %#v", body["query"])
	}
	boolQ, ok := query["bool"].(map[string]any)
	if !ok {
		t.Fatalf("bool shape unexpected: %#v", query["bool"])
	}
	filter, ok := boolQ["filter"].([]map[string]any)
	if !ok || len(filter) < 1 {
		t.Fatalf("filter list empty or wrong type: %#v", boolQ["filter"])
	}
	first, ok := filter[0]["term"].(map[string]any)
	if !ok {
		t.Fatalf("first filter term shape unexpected: %#v", filter[0])
	}
	if first["tenant_id"] != "tenant-A" {
		t.Fatalf("first filter must pin tenant_id to the injected value, got %v", first)
	}
	// The injected clause must be the FIRST filter so the cluster can
	// short-circuit early. If someone refactors this they will break
	// the defence-in-depth assumption, so we assert position 0 strictly.
}

// TestDateRangeMaxDaysRejected verifies the 90-day cap.
func TestDateRangeMaxDaysRejected(t *testing.T) {
	fc := &fakeClient{}
	svc := newTestService(fc)
	ctx := context.Background()

	from := time.Now().Add(-100 * 24 * time.Hour)
	to := time.Now()
	_, err := svc.SearchAudit(ctx, "tenant-A", AuditQuery{From: from, To: to})
	if err == nil || !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if !strings.Contains(err.Error(), "90") {
		t.Fatalf("error should mention 90 day limit, got: %v", err)
	}
}

// TestDateRangeWithin90DaysAccepted verifies the happy path.
func TestDateRangeWithin90DaysAccepted(t *testing.T) {
	fc := &fakeClient{}
	svc := newTestService(fc)
	ctx := context.Background()

	from := time.Now().Add(-30 * 24 * time.Hour)
	to := time.Now()
	if _, err := svc.SearchAudit(ctx, "tenant-A", AuditQuery{From: from, To: to}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

// TestActionAllowlist verifies only audit.AllActions values are accepted.
func TestActionAllowlist(t *testing.T) {
	fc := &fakeClient{}
	svc := newTestService(fc)
	ctx := context.Background()

	// Unknown action → reject.
	_, err := svc.SearchAudit(ctx, "tenant-A", AuditQuery{Action: "not.a.real.action"})
	if err == nil || !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation err for unknown action, got %v", err)
	}

	// Known action → accept.
	known := string(audit.ActionAdminLoginSuccess)
	if _, err := svc.SearchAudit(ctx, "tenant-A", AuditQuery{Action: known}); err != nil {
		t.Fatalf("known action rejected: %v", err)
	}
}

// TestPageBounds verifies page and page_size limits.
func TestPageBounds(t *testing.T) {
	fc := &fakeClient{}
	svc := newTestService(fc)
	ctx := context.Background()

	cases := []struct {
		name     string
		page     int
		pageSize int
		wantErr  bool
	}{
		{"default", 0, 0, false},
		{"page=1 size=25", 1, 25, false},
		{"page=1000 size=100", 1000, 100, false},
		{"page=1001", 1001, 25, true},
		{"size=5 below min", 1, 5, true},
		{"size=101 above max", 1, 101, true},
		{"page=-1", -1, 25, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.SearchAudit(ctx, "tenant-A", AuditQuery{Page: tc.page, PageSize: tc.pageSize})
			if tc.wantErr && err == nil {
				t.Fatalf("want error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("want ok, got %v", err)
			}
		})
	}
}

// TestQueryLengthLimit verifies the 200-char q cap.
func TestQueryLengthLimit(t *testing.T) {
	fc := &fakeClient{}
	svc := newTestService(fc)
	ctx := context.Background()

	long := strings.Repeat("a", 201)
	_, err := svc.SearchAudit(ctx, "tenant-A", AuditQuery{Q: long})
	if err == nil || !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation err, got %v", err)
	}
}

// TestNilClientReturnsUnavailable verifies the degraded-mode contract:
// when cmd/api/main.go cannot dial OpenSearch at boot, it builds the
// service with a nil client; every call must return ErrSearchUnavailable.
func TestNilClientReturnsUnavailable(t *testing.T) {
	svc := NewService(nil, quietLogger())
	ctx := context.Background()

	_, err := svc.SearchAudit(ctx, "tenant-A", AuditQuery{})
	if !errors.Is(err, ErrSearchUnavailable) {
		t.Fatalf("expected ErrSearchUnavailable, got %v", err)
	}
	_, err = svc.SearchEvents(ctx, "tenant-A", EventQuery{})
	if !errors.Is(err, ErrSearchUnavailable) {
		t.Fatalf("expected ErrSearchUnavailable, got %v", err)
	}
}

// TestMissingTenantIDRejected verifies the empty-tenant guard.
func TestMissingTenantIDRejected(t *testing.T) {
	fc := &fakeClient{}
	svc := newTestService(fc)
	ctx := context.Background()

	_, err := svc.SearchAudit(ctx, "", AuditQuery{})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation err, got %v", err)
	}
}

// TestKeystrokeEventKindRejected enforces the ADR 0013 defence-in-depth
// rule: admin APIs may not expose keystroke events even via metadata.
func TestKeystrokeEventKindRejected(t *testing.T) {
	fc := &fakeClient{}
	svc := newTestService(fc)
	ctx := context.Background()

	_, err := svc.SearchEvents(ctx, "tenant-A", EventQuery{EventKind: "KEYSTROKE_CAPTURED"})
	if !errors.Is(err, ErrValidation) {
		t.Fatalf("expected validation err, got %v", err)
	}
}

// TestSanitiseEventHitRedactsContent verifies the defence-in-depth
// payload redaction. If a misconfigured enricher somehow leaks keystroke
// content into the events index, the handler strips it before write.
func TestSanitiseEventHitRedactsContent(t *testing.T) {
	hit := EventHit{
		Payload: []byte(`{"content":"secret123","window_title":"bank"}`),
	}
	sanitiseEventHit(&hit)
	if strings.Contains(string(hit.Payload), "secret123") {
		t.Fatalf("content not redacted: %s", hit.Payload)
	}
	if !strings.Contains(string(hit.Payload), "[redacted]") {
		t.Fatalf("expected [redacted] marker, got %s", hit.Payload)
	}
	if !strings.Contains(string(hit.Payload), "bank") {
		t.Fatalf("other fields should survive, got %s", hit.Payload)
	}
}

// TestSanitiseEventHitKeystrokeContentField verifies the alternate
// keystroke_content key is also redacted.
func TestSanitiseEventHitKeystrokeContentField(t *testing.T) {
	hit := EventHit{
		Payload: []byte(`{"keystroke_content":"password","endpoint":"x"}`),
	}
	sanitiseEventHit(&hit)
	if strings.Contains(string(hit.Payload), "password") {
		t.Fatalf("keystroke_content not redacted: %s", hit.Payload)
	}
}

// TestNormaliseRangeDefaults verifies zero-value from/to expand to the
// 7-day default lookback window.
func TestNormaliseRangeDefaults(t *testing.T) {
	var from, to time.Time
	normaliseRange(&from, &to)
	if from.IsZero() || to.IsZero() {
		t.Fatal("normaliseRange should populate both bounds")
	}
	delta := to.Sub(from)
	// Allow a second of clock slew.
	if delta < 7*24*time.Hour-time.Second || delta > 7*24*time.Hour+time.Second {
		t.Fatalf("default lookback should be 7d, got %v", delta)
	}
}
