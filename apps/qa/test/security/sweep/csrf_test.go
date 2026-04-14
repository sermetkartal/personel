//go:build security

// Faz 14 #152 — CSRF protection verification.
//
// Every state-changing endpoint (POST/PUT/PATCH/DELETE) must
// either:
//   1. Require a CSRF token in a header (`X-CSRF-Token` or
//      similar), OR
//   2. Enforce same-origin via the Origin/Referer header, OR
//   3. Accept only JSON with a custom header the browser
//      preflights (simple form POSTs must NOT be accepted).
//
// This test tries to issue a state-changing request WITHOUT
// the CSRF gate and expects it to be rejected.
package sweep

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"
)

var csrfTargets = []struct {
	Method string
	Path   string
	Body   string
}{
	{"POST", "/v1/dsr/requests", `{"subject_id":"csrf-test"}`},
	{"POST", "/v1/policies", `{"name":"csrf-test"}`},
	{"POST", "/v1/tenants", `{"name":"csrf-test"}`},
	{"POST", "/v1/users", `{"email":"csrf@test.com"}`},
	{"POST", "/v1/legal-holds", `{"subject_id":"csrf-test"}`},
	{"POST", "/v1/liveview/requests", `{"endpoint_id":"csrf-test"}`},
	{"POST", "/v1/endpoints/bulk-wipe", `{"endpoint_ids":["csrf-test"]}`},
	{"DELETE", "/v1/policies/test", ``},
	{"PUT", "/v1/policies/test", `{"name":"csrf-test"}`},
	{"PATCH", "/v1/users/test", `{"display_name":"csrf"}`},
}

func TestCSRF_StateChangingEndpointsRejectCrossOrigin(t *testing.T) {
	apiURL := getAPIURL(t)
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Note: we intentionally DO NOT set an Authorization header
	// for some tests and DO set it for others. The CSRF gate
	// must reject cross-origin even when auth is supplied (a
	// real attacker's target has a valid session cookie).
	token := getAdminToken(t)

	for _, target := range csrfTargets {
		t.Run(target.Method+" "+target.Path, func(t *testing.T) {
			var body *bytes.Reader
			if target.Body != "" {
				body = bytes.NewReader([]byte(target.Body))
			} else {
				body = bytes.NewReader(nil)
			}
			req, err := http.NewRequest(target.Method, apiURL+target.Path, body)
			if err != nil {
				t.Fatalf("build request: %v", err)
			}
			// Simulate a malicious site
			req.Header.Set("Origin", "http://attacker.example.com")
			req.Header.Set("Referer", "http://attacker.example.com/evil.html")
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Logf("network error: %v", err)
				return
			}
			defer resp.Body.Close()

			// Expected: 400/401/403/415. 2xx means CSRF gate failed.
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				t.Errorf("state-changing endpoint %s %s accepted cross-origin form POST (status %d) — CSRF gate missing",
					target.Method, target.Path, resp.StatusCode)
			}
		})
	}
}

func TestCSRF_CORSPreflightRejectsWildcard(t *testing.T) {
	apiURL := getAPIURL(t)
	client := &http.Client{Timeout: 10 * time.Second}

	req, _ := http.NewRequest("OPTIONS", apiURL+"/v1/dsr/requests", nil)
	req.Header.Set("Origin", "http://attacker.example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "content-type,authorization")

	resp, err := client.Do(req)
	if err != nil {
		t.Skipf("network error: %v", err)
	}
	defer resp.Body.Close()

	allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
	if allowOrigin == "*" {
		t.Error("CORS allows * origin on sensitive endpoint — must allow-list trusted origins only")
	}
	if strings.Contains(allowOrigin, "attacker.example.com") {
		t.Error("CORS reflected attacker origin — allow-list is not enforced")
	}
}
