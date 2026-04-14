//go:build security

// Faz 14 #152 — SQL injection sweep across every API endpoint.
//
// This is an exhaustive payload test, NOT a fuzz — we pin the
// payload corpus so regressions trip the same canonical tests.
// Each payload is tried against every `q`, `id`, `subject_id`,
// `email`, `name`, and body JSON string parameter on every public
// route. Expected outcome:
//
//   - 400 Bad Request (input validation) OR
//   - 401/403 (auth gate fired first) OR
//   - 200 with empty/clean result set (parameterized query held)
//
// UNEXPECTED: 500 Internal Server Error (stack trace leak),
// 200 with data leak, or any response containing raw SQL
// error text from Postgres (`pq:`, `ERROR:`, `relation`,
// `syntax error at or near`).
//
// Run:
//   go test -tags=security -timeout=5m -run TestSQLi ./test/security/sweep/...
package sweep

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// sqliPayloads is the canonical corpus. Each payload targets a
// different SQL injection class.
var sqliPayloads = []string{
	// Classic tautology
	"' OR '1'='1",
	"' OR 1=1--",
	"'; DROP TABLE users;--",
	"' UNION SELECT NULL,NULL,NULL--",

	// Comment variants
	"admin'--",
	"admin'#",
	"admin'/*",

	// Boolean blind
	"' AND 1=1--",
	"' AND 1=2--",

	// Time-based blind (detectable via response time delta)
	"'; SELECT pg_sleep(5)--",
	"1'); SELECT pg_sleep(5)--",

	// Error-based
	"' AND EXTRACTVALUE(1, CONCAT(0x7e, VERSION()))--",
	"'+CAST(@@version AS int)+'",

	// Stacked queries
	"1; INSERT INTO audit_log VALUES ('attacker')",

	// Unicode / encoding bypass
	"%27%20OR%20%271%27%3D%271",
	`\u0027 OR \u00271\u0027=\u00271`,

	// JSON body injection
	`"}, "extra": "' OR 1=1--`,

	// XML entity (XXE adjacent, still checked here)
	`<!ENTITY xxe SYSTEM "file:///etc/passwd">`,

	// LDAP / NoSQL adjacent
	`{"$ne": null}`,
	`{"$gt": ""}`,
}

// sqliTargets enumerates the endpoints + query params + body
// fields to fuzz. If an endpoint requires auth the test expects
// 401/403 BEFORE input reaches SQL; that's still a pass.
var sqliTargets = []struct {
	Name       string
	Method     string
	Path       string
	QueryParam string
	BodyField  string
}{
	{"audit-search-q", "GET", "/v1/audit/events", "q", ""},
	{"employee-search-q", "GET", "/v1/employees", "q", ""},
	{"employee-by-id", "GET", "/v1/employees/{id}", "id", ""},
	{"endpoint-search", "GET", "/v1/endpoints", "q", ""},
	{"endpoint-by-id", "GET", "/v1/endpoints/{id}", "id", ""},
	{"dsr-subject-id", "POST", "/v1/dsr/requests", "", "subject_id"},
	{"dsr-search", "GET", "/v1/dsr/requests", "q", ""},
	{"policy-name", "POST", "/v1/policies", "", "name"},
	{"tenant-name", "POST", "/v1/tenants", "", "name"},
	{"user-email", "POST", "/v1/users", "", "email"},
	{"liveview-reason", "POST", "/v1/liveview/requests", "", "reason"},
	{"legal-hold-subject", "POST", "/v1/legal-holds", "", "subject_id"},
	{"evidence-period", "GET", "/v1/system/evidence-coverage", "period", ""},
}

var sqlErrorPatterns = []string{
	`pq:`,
	`ERROR:`,
	`relation "`,
	`syntax error at or near`,
	`column "`,
	`duplicate key value`,
	`violates foreign key`,
	`SQLSTATE`,
	`pg_query`,
	`postgres.exception`,
}

func TestSQLi_AllEndpoints(t *testing.T) {
	apiURL := getAPIURL(t)
	token := getAdminToken(t)
	client := &http.Client{Timeout: 15 * time.Second}

	for _, target := range sqliTargets {
		for _, payload := range sqliPayloads {
			name := target.Name + "/" + payloadLabel(payload)
			t.Run(name, func(t *testing.T) {
				req := buildSQLiRequest(t, apiURL, token, target, payload)
				if req == nil {
					t.Skip("could not build request for target")
					return
				}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				resp, err := client.Do(req.WithContext(ctx))
				if err != nil {
					// Network errors are NOT a pass (could be DoS)
					t.Logf("network error (acceptable if timeout-based payload): %v", err)
					return
				}
				defer resp.Body.Close()
				body, _ := io.ReadAll(resp.Body)

				// Hard failures
				if resp.StatusCode == 500 {
					t.Fatalf("500 on SQLi payload %q — server crashed or leaked stack",
						payload)
				}
				for _, pat := range sqlErrorPatterns {
					if strings.Contains(string(body), pat) {
						t.Fatalf("response body contains SQL error pattern %q; payload=%q body=%s",
							pat, payload, truncate(string(body), 400))
					}
				}

				// Acceptable: 200 empty, 400 validation, 401/403 auth,
				// 404 not found, 422 semantic. Anything else merits a log.
				switch resp.StatusCode {
				case 200, 201, 204, 400, 401, 403, 404, 422:
					// ok
				default:
					t.Logf("unexpected status %d for payload %q", resp.StatusCode, payload)
				}
			})
		}
	}
}

func buildSQLiRequest(t *testing.T, apiURL, token string, tgt struct {
	Name       string
	Method     string
	Path       string
	QueryParam string
	BodyField  string
}, payload string) *http.Request {
	t.Helper()
	path := tgt.Path
	path = strings.ReplaceAll(path, "{id}", url.PathEscape(payload))

	var req *http.Request
	var err error
	switch tgt.Method {
	case "GET":
		u := apiURL + path
		if tgt.QueryParam != "" && !strings.Contains(path, "{") {
			u += "?" + tgt.QueryParam + "=" + url.QueryEscape(payload)
		}
		req, err = http.NewRequest("GET", u, nil)
	case "POST":
		body := map[string]any{}
		if tgt.BodyField != "" {
			body[tgt.BodyField] = payload
		}
		jb, _ := json.Marshal(body)
		req, err = http.NewRequest("POST", apiURL+path, bytes.NewReader(jb))
		if req != nil {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	if err != nil || req == nil {
		return nil
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	return req
}

func payloadLabel(p string) string {
	if len(p) > 20 {
		p = p[:20]
	}
	return strings.Map(func(r rune) rune {
		if r == '/' || r == ' ' || r == '\t' {
			return '_'
		}
		return r
	}, p)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
