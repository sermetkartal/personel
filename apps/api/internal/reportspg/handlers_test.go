package reportspg

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/personel/api/internal/auth"
)

// ---------------------------------------------------------------------------
// parseRange — table-driven unit tests
// ---------------------------------------------------------------------------

func TestParseRange(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name    string
		from    string
		to      string
		wantErr bool
		// validate is called only when wantErr==false.
		validate func(t *testing.T, from, to time.Time)
	}{
		{
			name: "valid RFC3339 from/to",
			from: "2026-01-01T00:00:00Z",
			to:   "2026-01-07T00:00:00Z",
			validate: func(t *testing.T, from, to time.Time) {
				t.Helper()
				wantFrom, _ := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
				wantTo, _ := time.Parse(time.RFC3339, "2026-01-07T00:00:00Z")
				if !from.Equal(wantFrom) {
					t.Errorf("from: got %v, want %v", from, wantFrom)
				}
				if !to.Equal(wantTo) {
					t.Errorf("to: got %v, want %v", to, wantTo)
				}
			},
		},
		{
			name: "valid YYYY-MM-DD from/to",
			from: "2026-02-01",
			to:   "2026-02-10",
			validate: func(t *testing.T, from, to time.Time) {
				t.Helper()
				wantFrom, _ := time.Parse("2006-01-02", "2026-02-01")
				wantTo, _ := time.Parse("2006-01-02", "2026-02-10")
				if !from.Equal(wantFrom) {
					t.Errorf("from: got %v, want %v", from, wantFrom)
				}
				if !to.Equal(wantTo) {
					t.Errorf("to: got %v, want %v", to, wantTo)
				}
			},
		},
		{
			name:    "invalid format from=banana",
			from:    "banana",
			to:      "2026-01-07",
			wantErr: true,
		},
		{
			name:    "from after to",
			from:    "2026-03-10",
			to:      "2026-03-01",
			wantErr: true,
		},
		{
			name:    "range exceeds 92 days",
			from:    "2026-01-01",
			to:      "2026-04-10", // 99 days
			wantErr: true,
		},
		{
			name: "both params absent — default 7-day lookback",
			from: "",
			to:   "",
			validate: func(t *testing.T, from, to time.Time) {
				t.Helper()
				// to must be close to now.
				if d := to.Sub(now).Abs(); d > 2*time.Second {
					t.Errorf("to is not close to now: delta=%v", d)
				}
				// from must be ~7 days before to.
				wantFrom := to.AddDate(0, 0, -7)
				if d := from.Sub(wantFrom).Abs(); d > 2*time.Second {
					t.Errorf("from is not ~7 days before to: delta=%v", d)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			target := "/v1/reports-preview/productivity"
			if tc.from != "" {
				target += "?from=" + tc.from
				if tc.to != "" {
					target += "&to=" + tc.to
				}
			} else if tc.to != "" {
				target += "?to=" + tc.to
			}

			r := httptest.NewRequest(http.MethodGet, target, nil)

			from, to, err := parseRange(r)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got from=%v to=%v", from, to)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.validate != nil {
				tc.validate(t, from, to)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Handler smoke tests — auth guard (nil principal → 401)
// ---------------------------------------------------------------------------
//
// pgxpool.Pool cannot be easily mocked (concrete type, no interface).
// We test only the code paths that return before any pool.Query call:
//   1. principal nil → 401
//   2. bad query param → 400
//
// Pool-dependent paths (query, scan, rows.Err) are excluded from this file;
// they are covered by the integration test suite (test/integration/).

func buildRequest(path, query string, p *auth.Principal) *http.Request {
	url := path
	if query != "" {
		url += "?" + query
	}
	r := httptest.NewRequest(http.MethodGet, url, nil)
	if p != nil {
		r = r.WithContext(auth.WithPrincipal(r.Context(), p))
	}
	return r
}

func validPrincipal() *auth.Principal {
	return &auth.Principal{
		UserID:   "user-1",
		TenantID: "tenant-abc",
		Username: "testuser",
	}
}

// productivityHandlerWith returns the inner http.HandlerFunc without a real
// pool. Passing nil is safe for the auth-guard and validation branches because
// pool.Query is never reached.
func productivityHandlerWith(pool interface{}) http.HandlerFunc {
	// We pass nil as the pool; the handler closure captures it but only
	// calls it after the auth and parseRange guards, which we don't reach
	// in these tests.
	return ProductivityHandler(nil)
}

func TestProductivityHandler_NilPrincipal(t *testing.T) {
	h := ProductivityHandler(nil)
	w := httptest.NewRecorder()
	r := buildRequest("/v1/reports-preview/productivity", "", nil)
	h(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestProductivityHandler_BadRange(t *testing.T) {
	h := ProductivityHandler(nil)
	w := httptest.NewRecorder()
	r := buildRequest("/v1/reports-preview/productivity", "from=notadate", validPrincipal())
	h(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestTopAppsHandler_NilPrincipal(t *testing.T) {
	h := TopAppsHandler(nil)
	w := httptest.NewRecorder()
	r := buildRequest("/v1/reports-preview/top-apps", "", nil)
	h(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestTopAppsHandler_BadRange(t *testing.T) {
	h := TopAppsHandler(nil)
	w := httptest.NewRecorder()
	r := buildRequest("/v1/reports-preview/top-apps", "to=notadate", validPrincipal())
	h(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestIdleActiveHandler_NilPrincipal(t *testing.T) {
	h := IdleActiveHandler(nil)
	w := httptest.NewRecorder()
	r := buildRequest("/v1/reports-preview/idle-active", "", nil)
	h(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestIdleActiveHandler_BadRange(t *testing.T) {
	h := IdleActiveHandler(nil)
	w := httptest.NewRecorder()
	r := buildRequest("/v1/reports-preview/idle-active", "from=2026-04-20&to=2026-01-01", validPrincipal())
	h(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// AppBlocksHandler — full response shape test (no pool access)
// ---------------------------------------------------------------------------

func TestAppBlocksHandler_NilPrincipal(t *testing.T) {
	h := AppBlocksHandler(nil)
	w := httptest.NewRecorder()
	r := buildRequest("/v1/reports-preview/app-blocks", "", nil)
	h(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// AppBlocksHandler uses p == nil check (no TenantID check), so a principal
// with an empty TenantID still passes the auth guard and reaches JSON output.
func TestAppBlocksHandler_ValidPrincipalShape(t *testing.T) {
	h := AppBlocksHandler(nil)
	w := httptest.NewRecorder()
	p := validPrincipal()
	r := buildRequest("/v1/reports-preview/app-blocks", "", p)
	h(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	// items must be present and be an empty array.
	rawItems, ok := body["items"]
	if !ok {
		t.Fatal("response missing 'items' key")
	}
	var items []AppBlockRow
	if err := json.Unmarshal(rawItems, &items); err != nil {
		t.Fatalf("items is not a valid AppBlockRow array: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected empty items, got %d entries", len(items))
	}

	// notice_code must match the sentinel value.
	var noticeCode string
	if err := json.Unmarshal(body["notice_code"], &noticeCode); err != nil {
		t.Fatalf("notice_code decode error: %v", err)
	}
	const wantCode = "reports.app_blocks.no_source"
	if noticeCode != wantCode {
		t.Errorf("notice_code: got %q, want %q", noticeCode, wantCode)
	}

	// notice_hint must be non-empty.
	var noticeHint string
	if err := json.Unmarshal(body["notice_hint"], &noticeHint); err != nil {
		t.Fatalf("notice_hint decode error: %v", err)
	}
	if noticeHint == "" {
		t.Error("notice_hint must not be empty")
	}

	// from and to must be present (time values; we don't assert exact content
	// but they must decode without error).
	if _, ok := body["from"]; !ok {
		t.Error("response missing 'from' key")
	}
	if _, ok := body["to"]; !ok {
		t.Error("response missing 'to' key")
	}
}

func TestAppBlocksHandler_BadRange(t *testing.T) {
	h := AppBlocksHandler(nil)
	w := httptest.NewRecorder()
	r := buildRequest("/v1/reports-preview/app-blocks", "from=banana", validPrincipal())
	h(w, r)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
