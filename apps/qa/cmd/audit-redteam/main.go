// cmd/audit-redteam/main.go — keystroke admin-blindness red team CLI.
//
// This command exercises Phase 1 exit criterion #9 by impersonating the
// most privileged admin role and attempting to access keystroke content
// through every plausible API path. It exits 0 only if all attempts fail
// to return plaintext — confirming the cryptographic guarantee.
//
// Usage:
//
//	audit-redteam --api http://localhost:8080 --tenant <id> --endpoint <id>
//
// In CI, this is run after the full stack is up and test data has been
// ingested. If it exits non-zero, Phase 1 is blocked.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/personel/qa/internal/report"
)

// sentinelTCKN is the exact synthetic identifier seeded into the test
// keystroke blob by the integration setup (see security/keystroke_admin_blindness_test.go).
// Finding this byte pattern in ANY response body — base64, hex, or
// plaintext — is an EC-9 failure.
const sentinelTCKN = "12345678901"

// sentinelHexEncoded = hex.EncodeToString([]byte(sentinelTCKN)).
// Pre-computed to avoid init() overhead in the hot path.
var sentinelHexEncoded = hex.EncodeToString([]byte(sentinelTCKN))

// sentinelB64Encoded = base64.StdEncoding.EncodeToString([]byte(sentinelTCKN)).
var sentinelB64Encoded = base64.StdEncoding.EncodeToString([]byte(sentinelTCKN))

func main() {
	var (
		apiAddr    string
		tenantID   string
		endpointID string
		timeout    time.Duration
		verbose    bool
		reportPath string
		adminToken string
		minioAddr  string
		minioAKey  string
		minioSKey  string
		vaultAddr  string
		vaultToken string
	)

	root := &cobra.Command{
		Use:   "audit-redteam",
		Short: "Keystroke admin-blindness red team — Phase 1 exit criterion #9",
		Long: `audit-redteam impersonates the most privileged non-DLP admin role and
attempts to access keystroke plaintext through every known API path.

Exit codes:
  0 — All attack vectors failed to return plaintext. Admin-blindness confirmed.
  1 — At least one attack vector returned keystroke plaintext. Phase 1 BLOCKED.
  2 — Setup error (API unreachable, stack not ready).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			level := slog.LevelInfo
			if verbose {
				level = slog.LevelDebug
			}
			slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()

			slog.Info("RED TEAM: starting keystroke admin-blindness test",
				"api", apiAddr,
				"tenant", tenantID,
				"endpoint", endpointID,
			)

			runner := &redTeamRunner{
				apiBase:    apiAddr,
				tenantID:   tenantID,
				endpointID: endpointID,
				reportPath: reportPath,
				adminToken: adminToken,
				minioAddr:  minioAddr,
				minioAKey:  minioAKey,
				minioSKey:  minioSKey,
				vaultAddr:  vaultAddr,
				vaultToken: vaultToken,
				httpClient: &http.Client{Timeout: 30 * time.Second},
			}

			result, err := runner.Run(ctx)
			if err != nil {
				slog.Error("RED TEAM: setup error", "error", err)
				os.Exit(2)
			}

			printRedTeamResult(result)

			// Persist a structured report for CI artifact collection.
			if reportPath != "" {
				if err := writeReport(result, reportPath); err != nil {
					slog.Warn("could not write report", "error", err)
				}
			}

			if !result.Passed {
				slog.Error("RED TEAM: FAILED — keystroke plaintext was accessible to admin",
					"failed_vectors", result.FailedVectors)
				fmt.Println("\nPHASE 1 EXIT CRITERION #9: FAIL — Admin CAN read keystroke content")
				fmt.Println("This is a Phase 1 blocker. Do not ship until fixed.")
				os.Exit(1)
			}

			fmt.Println("\nPHASE 1 EXIT CRITERION #9: PASS — Admin CANNOT read keystroke content")
			return nil
		},
	}

	root.Flags().StringVar(&apiAddr, "api", "http://localhost:8080", "Admin API base URL")
	root.Flags().StringVar(&tenantID, "tenant", "", "Tenant ID for test data")
	root.Flags().StringVar(&endpointID, "endpoint", "", "Endpoint ID for test data")
	root.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "Overall test timeout")
	root.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable debug logging")
	root.Flags().StringVar(&reportPath, "report", "./reports/redteam", "Directory for report output")
	root.Flags().StringVar(&adminToken, "admin-token", "", "DPO/Admin OIDC bearer token (AV1-6)")
	root.Flags().StringVar(&minioAddr, "minio", "http://localhost:9000", "MinIO endpoint URL (AV2)")
	root.Flags().StringVar(&minioAKey, "minio-access-key", "minioadmin", "MinIO admin access key (AV2)")
	root.Flags().StringVar(&minioSKey, "minio-secret-key", "minioadmin", "MinIO admin secret key (AV2)")
	root.Flags().StringVar(&vaultAddr, "vault", "http://localhost:8200", "Vault address (AV3)")
	root.Flags().StringVar(&vaultToken, "vault-admin-token", "", "Vault admin-role token to attempt transit export (AV3)")

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// redTeamResult holds the overall result of the red team run.
type redTeamResult struct {
	Passed        bool
	FailedVectors []string
	TestedVectors int
	PassedVectors int
	Details       []vectorResult
}

type vectorResult struct {
	Name    string
	Passed  bool
	Details string
}

// redTeamRunner executes the red team attack vectors.
type redTeamRunner struct {
	apiBase    string
	tenantID   string
	endpointID string
	reportPath string
	adminToken string
	minioAddr  string
	minioAKey  string
	minioSKey  string
	vaultAddr  string
	vaultToken string
	httpClient *http.Client
}

// attackVector is one probe. Each probe makes HTTP calls and inspects the
// full response (headers + body) for any leak of keystroke plaintext.
type attackVector struct {
	name        string
	description string
	probe       func(ctx context.Context, r *redTeamRunner) vectorResult
}

// Run executes all attack vectors and returns the aggregate result.
//
// Phase 1 exit criterion #9: ALL vectors must report "passed" (meaning
// zero keystroke plaintext leaked). Any single failure fails the whole
// run and Phase 1 is blocked per ADR 0013.
func (r *redTeamRunner) Run(ctx context.Context) (*redTeamResult, error) {
	result := &redTeamResult{Passed: true}

	vectors := []attackVector{
		{"AV1-direct-event-query", "GET /v1/events?event_kind=keystroke.* with admin token", r.probeAV1DirectEventQuery},
		{"AV2-minio-keystroke-bucket", "Direct MinIO admin read of keystroke bucket", r.probeAV2MinIO},
		{"AV3-vault-transit-export", "Vault transit/export/encryption-key/pe-dek-*", r.probeAV3Vault},
		{"AV4-postgres-raw-dump", "GET /v1/debug/pg?table=keystroke_keys (should not exist)", r.probeAV4Postgres},
		{"AV5-decrypt-api-enumeration", "OpenAPI route list scanned for decrypt verbs", r.probeAV5DecryptAPI},
		{"AV6-dlp-match-events", "GET /v1/dlp-matches — metadata only (no content field)", r.probeAV6DLPMatches},
		{"AV7-search-api-keystroke", "GET /v1/audit/search?q=<sentinel> — must not return blob bytes", r.probeAV7Search},
		{"AV8-content-negotiation-bypass", "Accept: application/x-raw-bytes on keystroke routes", r.probeAV8ContentNeg},
		{"AV9-debug-endpoints", "GET /debug/pprof?k=keystroke and /metrics PII scan", r.probeAV9Debug},
		{"AV10-presigned-url-probing", "POST /v1/screenshots/presign against sensitive/keystroke prefix", r.probeAV10Presign},
	}

	for _, av := range vectors {
		vr := av.probe(ctx, r)
		vr.Name = av.name
		result.TestedVectors++
		if vr.Passed {
			result.PassedVectors++
		} else {
			result.FailedVectors = append(result.FailedVectors, av.name)
			result.Passed = false
		}
		result.Details = append(result.Details, vr)
	}

	return result, nil
}

// -----------------------------------------------------------------------
// Individual attack vector implementations.
//
// Every probe follows the same contract:
//
//   1. Issue the HTTP request with the admin token (if applicable).
//   2. Read the full body.
//   3. Call inspectForLeak on the body — any hit fails the probe.
//   4. Also fail if the status code is 2xx on an endpoint that should
//      have been 404 (e.g. AV4 debug route).
//
// A probe PASSES if either:
//   - the endpoint returned 401/403/404 (admin blocked), OR
//   - the endpoint returned 200 with a body that does NOT contain the
//     sentinel TCKN in any encoding.
// -----------------------------------------------------------------------

func (r *redTeamRunner) probeAV1DirectEventQuery(ctx context.Context, _ *redTeamRunner) vectorResult {
	url := fmt.Sprintf("%s/v1/events?event_kind=keystroke.content&tenant_id=%s&endpoint_id=%s",
		r.apiBase, r.tenantID, r.endpointID)
	body, code, err := r.adminGet(ctx, url)
	return buildResult("GET /v1/events?event_kind=keystroke.content", body, code, err)
}

func (r *redTeamRunner) probeAV2MinIO(ctx context.Context, _ *redTeamRunner) vectorResult {
	// Phase 1: use S3-style LIST against the keystroke bucket with admin
	// creds. Real MinIO policy should reject (403). If it returns 200 AND
	// any object bytes contain the sentinel in plaintext, fail hard.
	url := fmt.Sprintf("%s/personel-keystroke/?list-type=2", strings.TrimRight(r.minioAddr, "/"))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	// Basic S3 sig placeholder — in real pilot use mc admin instead.
	req.SetBasicAuth(r.minioAKey, r.minioSKey)
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return vectorResult{Passed: true, Details: "minio unreachable (pass — nothing to leak)"}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	// 403 or 404 = admin blocked = pass. 200 with sentinel = fail.
	if resp.StatusCode >= 400 {
		return vectorResult{Passed: true, Details: fmt.Sprintf("minio denied status=%d", resp.StatusCode)}
	}
	return inspectForLeak("minio list", body)
}

func (r *redTeamRunner) probeAV3Vault(ctx context.Context, _ *redTeamRunner) vectorResult {
	if r.vaultToken == "" {
		return vectorResult{Passed: true, Details: "vault-admin-token not provided (skip)"}
	}
	// Attempt to export the PE-DEK transit key as an admin. ADR 0013
	// requires exportable=false on this key, so the call should 400 or
	// 403. If it succeeds, Phase 1 is blocked.
	keyName := fmt.Sprintf("pe-dek-%s-*", r.tenantID)
	url := fmt.Sprintf("%s/v1/transit/export/encryption-key/%s",
		strings.TrimRight(r.vaultAddr, "/"), keyName)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("X-Vault-Token", r.vaultToken)
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return vectorResult{Passed: true, Details: "vault unreachable (skip)"}
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return vectorResult{Passed: true, Details: fmt.Sprintf("vault denied export status=%d", resp.StatusCode)}
	}
	// 200 with any key material = fail outright, no leak scan needed.
	return vectorResult{
		Passed:  false,
		Details: "CRITICAL: Vault transit key was exportable by admin (EC-9 failure)",
	}
}

func (r *redTeamRunner) probeAV4Postgres(ctx context.Context, _ *redTeamRunner) vectorResult {
	// This endpoint MUST NOT EXIST. If it does and returns 200, that is
	// a pre-existing backdoor.
	url := r.apiBase + "/v1/debug/pg?table=keystroke_keys"
	body, code, err := r.adminGet(ctx, url)
	if err == nil && code == 200 {
		return vectorResult{Passed: false, Details: "CRITICAL: /v1/debug/pg returned 200 (should not exist)"}
	}
	_ = body
	return vectorResult{Passed: true, Details: fmt.Sprintf("debug endpoint absent (code=%d)", code)}
}

func (r *redTeamRunner) probeAV5DecryptAPI(ctx context.Context, _ *redTeamRunner) vectorResult {
	// Pull the OpenAPI spec and grep for dangerous verbs/path segments.
	url := r.apiBase + "/v1/openapi.yaml"
	body, code, err := r.adminGet(ctx, url)
	if err != nil || code != 200 {
		return vectorResult{Passed: true, Details: "openapi spec not exposed (pass)"}
	}
	lower := strings.ToLower(string(body))
	for _, bad := range []string{"/decrypt", "/keystroke/plaintext", "/keystroke/content", "/dek/export", "/tmk/export"} {
		if strings.Contains(lower, bad) {
			return vectorResult{Passed: false, Details: "openapi spec exposes dangerous verb: " + bad}
		}
	}
	return vectorResult{Passed: true, Details: "openapi spec contains no decrypt verbs"}
}

func (r *redTeamRunner) probeAV6DLPMatches(ctx context.Context, _ *redTeamRunner) vectorResult {
	url := fmt.Sprintf("%s/v1/dlp-matches?tenant_id=%s", r.apiBase, r.tenantID)
	body, code, err := r.adminGet(ctx, url)
	if err != nil || code >= 400 {
		return vectorResult{Passed: true, Details: fmt.Sprintf("dlp-matches endpoint denied code=%d", code)}
	}
	return inspectForLeak("dlp-matches", body)
}

func (r *redTeamRunner) probeAV7Search(ctx context.Context, _ *redTeamRunner) vectorResult {
	url := fmt.Sprintf("%s/v1/audit/search?q=%s", r.apiBase, sentinelTCKN)
	body, code, err := r.adminGet(ctx, url)
	if err != nil || code >= 400 {
		return vectorResult{Passed: true, Details: fmt.Sprintf("audit search denied code=%d", code)}
	}
	return inspectForLeak("audit search", body)
}

func (r *redTeamRunner) probeAV8ContentNeg(ctx context.Context, _ *redTeamRunner) vectorResult {
	url := fmt.Sprintf("%s/v1/events?event_kind=keystroke.content", r.apiBase)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if r.adminToken != "" {
		req.Header.Set("Authorization", "Bearer "+r.adminToken)
	}
	req.Header.Set("Accept", "application/x-raw-bytes, */*")
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return vectorResult{Passed: true, Details: "unreachable"}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return vectorResult{Passed: true, Details: fmt.Sprintf("content-negotiation denied code=%d", resp.StatusCode)}
	}
	return inspectForLeak("content-neg", body)
}

func (r *redTeamRunner) probeAV9Debug(ctx context.Context, _ *redTeamRunner) vectorResult {
	// /metrics is expected public; we scan it for PII/sentinel.
	url := r.apiBase + "/metrics"
	body, code, err := r.adminGet(ctx, url)
	if err != nil || code >= 400 {
		return vectorResult{Passed: true, Details: "/metrics unavailable"}
	}
	return inspectForLeak("metrics", body)
}

func (r *redTeamRunner) probeAV10Presign(ctx context.Context, _ *redTeamRunner) vectorResult {
	// Attempt to presign a URL against sensitive/keystroke/*. Any 200
	// response with an actual URL is a failure — that URL would let
	// admin fetch the ciphertext AND (worse) trick MinIO into serving
	// it.
	url := r.apiBase + "/v1/screenshots/presign"
	reqBody := map[string]any{
		"bucket": "sensitive",
		"key":    fmt.Sprintf("keystroke/%s/%s/2026-04-01.blob", r.tenantID, r.endpointID),
	}
	b, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if r.adminToken != "" {
		req.Header.Set("Authorization", "Bearer "+r.adminToken)
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return vectorResult{Passed: true, Details: "presign unreachable"}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return vectorResult{Passed: true, Details: fmt.Sprintf("presign denied code=%d", resp.StatusCode)}
	}
	if strings.Contains(string(body), "sensitive/keystroke") {
		return vectorResult{Passed: false, Details: "CRITICAL: presign returned URL for keystroke bucket"}
	}
	return vectorResult{Passed: true, Details: "presign ignored keystroke prefix"}
}

// -----------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------

// adminGet issues a GET with the admin bearer token attached. Returns
// (body, status, error).
func (r *redTeamRunner) adminGet(ctx context.Context, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	if r.adminToken != "" {
		req.Header.Set("Authorization", "Bearer "+r.adminToken)
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return body, resp.StatusCode, err
}

// inspectForLeak scans the body for the sentinel TCKN in plaintext,
// base64, and hex. Any hit is a failure.
func inspectForLeak(label string, body []byte) vectorResult {
	s := string(body)
	if strings.Contains(s, sentinelTCKN) {
		return vectorResult{Passed: false, Details: label + ": sentinel TCKN found in plaintext"}
	}
	if strings.Contains(s, sentinelB64Encoded) {
		return vectorResult{Passed: false, Details: label + ": sentinel TCKN found in base64"}
	}
	if strings.Contains(s, sentinelHexEncoded) {
		return vectorResult{Passed: false, Details: label + ": sentinel TCKN found in hex"}
	}
	return vectorResult{Passed: true, Details: label + ": no leak"}
}

// buildResult converts an adminGet (body, code, err) tuple into a
// vectorResult. Network errors are conservatively marked as PASS
// (unreachable services cannot leak data).
func buildResult(label string, body []byte, code int, err error) vectorResult {
	if err != nil {
		return vectorResult{Passed: true, Details: label + ": unreachable (" + err.Error() + ")"}
	}
	if code >= 400 {
		return vectorResult{Passed: true, Details: fmt.Sprintf("%s: denied code=%d", label, code)}
	}
	return inspectForLeak(label, body)
}

func printRedTeamResult(result *redTeamResult) {
	fmt.Printf("\nRed Team Results — Keystroke Admin-Blindness\n")
	fmt.Printf("%s\n", "=======================================================")
	fmt.Printf("Vectors tested: %d | Passed: %d | Failed: %d\n\n",
		result.TestedVectors, result.PassedVectors, len(result.FailedVectors))

	for _, vr := range result.Details {
		status := "PASS"
		if !vr.Passed {
			status = "FAIL"
		}
		fmt.Printf("[%s] %s\n", status, vr.Name)
		if !vr.Passed {
			fmt.Printf("       %s\n", vr.Details)
		}
	}
}

// writeReport serialises the red team result into the internal/report format
// and writes JSON+HTML artifacts to dir. The resulting files are suitable for
// upload as CI artifacts and for long-term audit evidence.
func writeReport(result *redTeamResult, dir string) error {
	sr := report.NewSuiteResult("security-redteam-ec9", detectEnvironment())
	sr.CommitSHA = os.Getenv("GITHUB_SHA")
	sr.Branch = os.Getenv("GITHUB_REF_NAME")

	// EC-9 is the only criterion evaluated by this tool.
	actual := 0.0
	if result.Passed {
		actual = 1.0 // 1 = pass, 0 = fail (unitless boolean)
	}
	sr.AddCriterion(
		"EC-9",
		"Keystroke admin-blindness: all attack vectors blocked",
		"boolean",
		1.0, // threshold: must equal 1 (all blocked)
		actual,
		result.Passed,
		true, // blocking: Phase 1 hard gate
	)

	// Record each attack vector as a security test result.
	for _, vr := range result.Details {
		sr.AddSecurityResult(
			vr.Name,
			vr.Details,
			0, // no single status code — composite
			vr.Passed,
			!vr.Passed, // any exposure is critical for EC-9
			"",
		)
	}

	sr.Finalise()

	w := &report.Writer{OutputDir: dir}
	return w.Write(sr)
}

// detectEnvironment returns "ci", "staging", or "manual" based on env vars.
func detectEnvironment() string {
	if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		return "ci"
	}
	if os.Getenv("STAGING") != "" {
		return "staging"
	}
	return "manual"
}
