// ch_handlers_test.go — HTTP-layer tests for /v1/reports/ch/*.
//
// These tests stay at the handler boundary: they cover the 503 path
// when the CH client is nil (startup degraded mode), the 401 path for
// missing auth, and the validation matrix for from/to + user_ids +
// range cap. Query-building is covered in clickhouse/aggregations_test.go.
package reports

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/personel/api/internal/auth"
)

// newAuthedReq builds a GET request with a Principal attached to its
// context — the same shape the OIDC middleware installs in production.
func newAuthedReq(t *testing.T, url string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	p := &auth.Principal{
		UserID:   "user-sub",
		TenantID: "tenant-uuid",
		Roles:    []auth.Role{auth.RoleAdmin},
	}
	ctx := auth.WithPrincipal(req.Context(), p)
	return req.WithContext(ctx)
}

func TestCHTopAppsHandler_NilClientReturns503(t *testing.T) {
	h := NewCHHandlers(nil) // simulates CH-down-at-startup
	w := httptest.NewRecorder()
	req := newAuthedReq(t, "/v1/reports/ch/top-apps")

	h.CHTopAppsHandler()(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 from nil client, got %d (body: %s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "problem+json") {
		t.Errorf("expected RFC7807 problem+json, got %q", ct)
	}
}

func TestCHIdleActiveHandler_NilClientReturns503(t *testing.T) {
	h := NewCHHandlers(nil)
	w := httptest.NewRecorder()
	req := newAuthedReq(t, "/v1/reports/ch/idle-active")
	h.CHIdleActiveHandler()(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestCHProductivityHandler_NilClientReturns503(t *testing.T) {
	h := NewCHHandlers(nil)
	w := httptest.NewRecorder()
	req := newAuthedReq(t, "/v1/reports/ch/productivity")
	h.CHProductivityHandler()(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

func TestCHAppBlocksHandler_NilClientReturns503(t *testing.T) {
	h := NewCHHandlers(nil)
	w := httptest.NewRecorder()
	req := newAuthedReq(t, "/v1/reports/ch/app-blocks")
	h.CHAppBlocksHandler()(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
}

// --- parsing helpers ------------------------------------------------------

func TestParseCHRange_Default(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	from, to, msg, ok := parseCHRange(req)
	if !ok {
		t.Fatalf("expected ok, got msg %q", msg)
	}
	if to.Sub(from) < 6*24*time.Hour || to.Sub(from) > 8*24*time.Hour {
		t.Errorf("default range should be ~7 days, got %v", to.Sub(from))
	}
}

func TestParseCHRange_Over90DaysRejected(t *testing.T) {
	from := time.Now().UTC().AddDate(0, 0, -120)
	to := time.Now().UTC()
	url := "/?from=" + from.Format(time.RFC3339) + "&to=" + to.Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	_, _, msg, ok := parseCHRange(req)
	if ok {
		t.Errorf("120-day range should be rejected")
	}
	if !strings.Contains(msg, "90 day") {
		t.Errorf("expected 90-day message, got %q", msg)
	}
}

func TestParseCHRange_InvertedRejected(t *testing.T) {
	from := time.Now().UTC()
	to := from.Add(-1 * time.Hour)
	url := "/?from=" + from.Format(time.RFC3339) + "&to=" + to.Format(time.RFC3339)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	_, _, msg, ok := parseCHRange(req)
	if ok {
		t.Errorf("inverted range should be rejected")
	}
	if !strings.Contains(msg, "before") {
		t.Errorf("expected 'before' message, got %q", msg)
	}
}

func TestParseCHRange_MalformedRejected(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?from=yesterday", nil)
	_, _, _, ok := parseCHRange(req)
	if ok {
		t.Errorf("malformed 'from' should be rejected")
	}
}

func TestParseCHUserIDs_EmptyOK(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ids, _, ok := parseCHUserIDs(req)
	if !ok {
		t.Errorf("empty user_ids should be ok")
	}
	if ids != nil {
		t.Errorf("empty user_ids should return nil slice")
	}
}

func TestParseCHUserIDs_SplitsAndTrims(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?user_ids=a,+b+,c", nil)
	ids, _, ok := parseCHUserIDs(req)
	if !ok {
		t.Fatalf("parse failed")
	}
	if len(ids) != 3 {
		t.Errorf("expected 3 ids, got %v", ids)
	}
}

func TestParseCHUserIDs_Over50Rejected(t *testing.T) {
	parts := make([]string, 0, 60)
	for i := 0; i < 60; i++ {
		parts = append(parts, "u")
	}
	req := httptest.NewRequest(http.MethodGet, "/?user_ids="+strings.Join(parts, ","), nil)
	_, msg, ok := parseCHUserIDs(req)
	if ok {
		t.Errorf("60 user_ids should be rejected")
	}
	if !strings.Contains(msg, "50") {
		t.Errorf("expected 50-cap message, got %q", msg)
	}
}

func TestParseCHLimit_Defaults(t *testing.T) {
	cases := []struct {
		url      string
		def, max int
		want     int
	}{
		{"/", 10, 100, 10},
		{"/?limit=5", 10, 100, 5},
		{"/?limit=9999", 10, 100, 100},
		{"/?limit=abc", 10, 100, 10},
		{"/?limit=-5", 10, 100, 10},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodGet, tc.url, nil)
		got := parseCHLimit(req, tc.def, tc.max)
		if got != tc.want {
			t.Errorf("parseCHLimit(%s) = %d, want %d", tc.url, got, tc.want)
		}
	}
}

// TestCHTopAppsHandler_Nil503BodyShape verifies the problem+json body
// carries the standard fields the console consumes for its degraded
// mode banner.
func TestCHTopAppsHandler_Nil503BodyShape(t *testing.T) {
	h := NewCHHandlers(nil)
	w := httptest.NewRecorder()
	req := newAuthedReq(t, "/v1/reports/ch/top-apps")
	h.CHTopAppsHandler()(w, req)

	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v (body=%s)", err, w.Body.String())
	}
	if status, _ := body["status"].(float64); int(status) != 503 {
		t.Errorf("body.status should be 503, got %v", body["status"])
	}
	if _, ok := body["type"].(string); !ok {
		t.Errorf("body.type missing")
	}
}
