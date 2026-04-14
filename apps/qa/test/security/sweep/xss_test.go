//go:build security

// Faz 14 #152 — Reflected XSS sweep across console + API reflected
// response fields. We test only reflected XSS (payload → response)
// because stored XSS requires a DOM render step that belongs in
// the Playwright suite (Faz 14 #148).
package sweep

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

var xssPayloads = []string{
	`<script>alert(1)</script>`,
	`"><script>alert(1)</script>`,
	`'><img src=x onerror=alert(1)>`,
	`javascript:alert(1)`,
	`<svg/onload=alert(1)>`,
	`<iframe src="javascript:alert(1)">`,
	`<body onload=alert(1)>`,
	`<a href="javascript:alert(1)">x</a>`,
	`"><svg onload=alert(1)>`,
	`data:text/html,<script>alert(1)</script>`,
	`&#x3c;script&#x3e;alert(1)&#x3c;/script&#x3e;`,
	`<img src=x onerror="alert('xss')">`,
	`{{constructor.constructor('alert(1)')()}}`, // Angular-style template escape
	`${alert(1)}`,                               // Template literal
	`<math><mtext><option><FAKEFAKE><option></option><mglyph><svg><mtext><style><a title="</style><img src onerror=alert(1)>">`,
}

var xssTargets = []struct {
	Name      string
	Method    string
	Path      string
	BodyField string
}{
	{"dsr-justification", "POST", "/v1/dsr/requests", "justification"},
	{"dsr-subject-id", "POST", "/v1/dsr/requests", "subject_id"},
	{"policy-name", "POST", "/v1/policies", "name"},
	{"tenant-name", "POST", "/v1/tenants", "name"},
	{"endpoint-hostname", "POST", "/v1/endpoints", "hostname"},
	{"liveview-reason", "POST", "/v1/liveview/requests", "reason"},
	{"liveview-justification", "POST", "/v1/liveview/requests", "justification"},
	{"legal-hold-description", "POST", "/v1/legal-holds", "description"},
	{"user-display-name", "POST", "/v1/users", "display_name"},
}

func TestXSS_ReflectedInResponses(t *testing.T) {
	apiURL := getAPIURL(t)
	token := getAdminToken(t)
	client := &http.Client{Timeout: 10 * time.Second}

	for _, target := range xssTargets {
		for _, payload := range xssPayloads {
			t.Run(target.Name+"/"+payloadLabel(payload), func(t *testing.T) {
				body := map[string]any{target.BodyField: payload}
				jb, _ := json.Marshal(body)
				req, err := http.NewRequest(target.Method, apiURL+target.Path, bytes.NewReader(jb))
				if err != nil {
					t.Fatalf("build request: %v", err)
				}
				req.Header.Set("Content-Type", "application/json")
				if token != "" {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				resp, err := client.Do(req)
				if err != nil {
					t.Logf("network error: %v", err)
					return
				}
				defer resp.Body.Close()
				respBody, _ := io.ReadAll(resp.Body)

				// FAIL if the raw payload is echoed back unencoded
				// in the response body (for 2xx/4xx responses alike
				// — even 400 validation error messages must escape).
				if strings.Contains(string(respBody), payload) {
					// Only fail for dangerous tags — benign substrings
					// like "name:" might match harmlessly.
					if containsDangerousXSS(string(respBody)) {
						t.Fatalf("unencoded XSS payload reflected in response: %s",
							truncate(string(respBody), 400))
					}
				}

				// Check Content-Security-Policy header is present on
				// HTML responses.
				ct := resp.Header.Get("Content-Type")
				if strings.HasPrefix(ct, "text/html") {
					csp := resp.Header.Get("Content-Security-Policy")
					if csp == "" {
						t.Errorf("HTML response without Content-Security-Policy header (path %s)",
							target.Path)
					}
				}
			})
		}
	}
}

func containsDangerousXSS(s string) bool {
	lower := strings.ToLower(s)
	for _, needle := range []string{
		"<script", "onerror=", "onload=", "javascript:",
		"<svg/onload", "<iframe", "<body onload",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}
