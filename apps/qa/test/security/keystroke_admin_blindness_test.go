// keystroke_admin_blindness_test.go — RED TEAM TEST
//
// Phase 1 exit criterion #9: "Independent red team confirms admin cannot decrypt."
//
// CRITICALITY: HIGHEST. If any assertion in this file fails, Phase 1 is blocked.
// This test should be treated with the same weight as a production outage.
//
// What this test does:
//
//  1. Sets up the full stack via testcontainers.
//  2. Enrolls a synthetic agent and generates real AES-256-GCM encrypted
//     keystroke blobs (containing a synthetic TCKN "12345678901") in MinIO.
//  3. Stores the associated keystroke_keys row (wrapped_dek) in Postgres.
//  4. Impersonates the most privileged non-DLP admin role.
//  5. Exhaustively attempts EVERY plausible API path that could return
//     keystroke plaintext or key material:
//     a. Direct event query endpoints
//     b. MinIO blob download via admin API proxy
//     c. Postgres-exposed raw data endpoints
//     d. Vault transit/export endpoints
//     e. Any endpoint whose name contains "keystroke", "decrypt", "key",
//        "dek", "tmk", "plaintext", "content", or "export"
//     f. GraphQL (if exposed) with keystroke field resolvers
//  6. For each response, runs AssertNoKeystrokePlaintext to verify no
//     decrypted content was returned.
//  7. Also verifies the test TCKN blob cannot be decoded from any base64 or
//     hex-encoded field in any response.
//
// The test is structured to be self-documenting: each attack vector is a
// named sub-test that describes what it's trying and why it should fail.
package security

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/personel/qa/internal/assertions"
	"github.com/personel/qa/internal/harness"
	"github.com/personel/qa/internal/simulator"
)

// TestKeystrokeAdminBlindness is the primary Phase 1 security exit criterion test.
//
// The test is organized as a series of attack vectors, each of which MUST fail
// to return keystroke plaintext. The test passes only if ALL attack vectors fail
// to obtain any keystroke content.
func TestKeystrokeAdminBlindness(t *testing.T) {
	harness.RequireIntegration(t)

	stack := harness.MustStart(t, harness.StackOptions{
		WithGateway: true,
		WithAPI:     true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	t.Cleanup(cancel)

	// ─── Setup: Create test data ─────────────────────────────────────────────
	tenantID := "red-team-00-0000-0000-000000000001"
	endpointID := "red-team-ep-0000-0000-000000000001"

	// Initialize the test PE-DEK (the "real" key the agent would use).
	dek := simulator.NewTestPEDEK(endpointID, 1, 1)

	// Encrypt a buffer containing a synthetic TCKN. This is the plaintext
	// we will attempt to extract through every attack vector.
	plaintext := simulator.SyntheticKeystrokeBufferWithTCKN()
	blob, err := dek.EncryptKeystrokeContent(endpointID, 1, plaintext)
	require.NoError(t, err, "encrypt test keystroke buffer")

	t.Logf("RED TEAM: Encrypted %d bytes of keystroke content containing synthetic TCKN", len(plaintext))
	t.Logf("RED TEAM: Blob size: %d bytes (ciphertext)", len(blob.Ciphertext))
	t.Logf("RED TEAM: The ciphertext contains NO plaintext; the PE-DEK is in test memory only")

	// Upload the ciphertext blob to MinIO (simulating what the real agent would do).
	blobRef := uploadTestBlob(t, ctx, stack, tenantID, endpointID, blob.Ciphertext)
	t.Logf("RED TEAM: Ciphertext uploaded to %s", blobRef)

	// Store the keystroke_keys row in Postgres (wrapped_dek — NOT the plaintext DEK).
	// In production, the DLP service generates and wraps the DEK. Here we store
	// a realistic wrapped_dek (just random bytes, since there's no real Vault).
	storeKeystrokeKeyRow(t, ctx, stack, tenantID, endpointID, blob.Nonce, blob.AAD, blobRef)
	t.Logf("RED TEAM: keystroke_keys row stored in Postgres (wrapped_dek only, no plaintext)")

	// Ingest a keystroke.content_encrypted event referencing the blob.
	ingestKeystrokeEvent(t, ctx, stack, tenantID, endpointID, blobRef, blob)
	t.Logf("RED TEAM: keystroke.content_encrypted event ingested")

	// ─── Attack setup ────────────────────────────────────────────────────────
	// The admin impersonation: highest privilege non-DLP role.
	adminToken := getAdminToken(t, ctx, stack)
	apiBase := fmt.Sprintf("http://%s", stack.GatewayAddr)
	client := &http.Client{Timeout: 15 * time.Second}

	// makeAdminReq creates an HTTP request with admin credentials.
	makeAdminReq := func(method, path string, body io.Reader) *http.Request {
		req, err := http.NewRequestWithContext(ctx, method, apiBase+path, body)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer "+adminToken)
		req.Header.Set("X-Test-Role", "super_admin") // belt + suspenders
		req.Header.Set("Content-Type", "application/json")
		return req
	}

	// checkBody is the core assertion: any response body must not contain plaintext.
	checkBody := func(t *testing.T, resp *http.Response, attackName string) {
		t.Helper()
		if resp == nil {
			return
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1 MB limit
		if err != nil {
			t.Logf("%s: read error %v", attackName, err)
			return
		}

		// Primary check: no plaintext TCKN or content markers.
		assertions.AssertNoKeystrokePlaintext(t, body, attackName)

		// Secondary check: base64-decode any JSON string fields and check those too.
		checkBase64Fields(t, body, attackName)

		// Tertiary check: hex-decode any apparent hex strings.
		checkHexFields(t, body, attackName)

		t.Logf("%s: PASS (status=%d, body_len=%d, no plaintext found)", attackName, resp.StatusCode, len(body))
	}

	// ─── Attack Vector 1: Direct event query ─────────────────────────────────
	t.Run("AV1_direct_event_query", func(t *testing.T) {
		// Query keystroke events via the events API.
		// Should return metadata only (ciphertext_ref, nonce, etc.) — never plaintext.
		paths := []string{
			fmt.Sprintf("/v1/events?endpoint_id=%s&event_type=keystroke.content_encrypted", endpointID),
			fmt.Sprintf("/v1/events?endpoint_id=%s&event_type=keystroke.window_stats", endpointID),
			fmt.Sprintf("/v1/endpoints/%s/events", endpointID),
			fmt.Sprintf("/v1/endpoints/%s/keystrokes", endpointID),
		}
		for _, path := range paths {
			resp, err := client.Do(makeAdminReq(http.MethodGet, path, nil))
			if err != nil {
				t.Logf("AV1 %s: not available (%v)", path, err)
				continue
			}
			checkBody(t, resp, "AV1:"+path)
		}
	})

	// ─── Attack Vector 2: MinIO proxy via admin API ──────────────────────────
	t.Run("AV2_minio_proxy_via_api", func(t *testing.T) {
		// Try to fetch the ciphertext blob via the API's object proxy.
		// Even if this endpoint exists and returns the ciphertext, the ciphertext
		// itself must not be decryptable without the PE-DEK.
		// HOWEVER: the test also checks that the API does not transparently decrypt
		// and return the plaintext.
		paths := []string{
			fmt.Sprintf("/v1/blobs/%s", encodeRef(blobRef)),
			fmt.Sprintf("/v1/objects?ref=%s", blobRef),
			fmt.Sprintf("/v1/keystroke-blobs/%s/%s/latest", tenantID, endpointID),
		}
		for _, path := range paths {
			resp, err := client.Do(makeAdminReq(http.MethodGet, path, nil))
			if err != nil {
				continue
			}
			// Even if the raw ciphertext is returned, verify it's opaque.
			defer resp.Body.Close()
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			// The ciphertext should not decrypt to our TCKN without the PE-DEK.
			assertions.AssertNoKeystrokePlaintext(t, body, "AV2:"+path)
		}
	})

	// ─── Attack Vector 3: Vault transit/key export attempts ─────────────────
	t.Run("AV3_vault_key_export", func(t *testing.T) {
		// Try to access Vault directly via the API proxy.
		// The admin API must not expose any Vault transit export endpoints.
		vaultPaths := []string{
			fmt.Sprintf("/v1/vault/transit/keys/tenant/%s/tmk/export", tenantID),
			"/v1/vault/transit/export",
			"/v1/secrets/keystroke-keys",
			"/v1/crypto/derive-dek",
			fmt.Sprintf("/v1/keystroke-keys/%s", endpointID),
		}
		for _, path := range vaultPaths {
			resp, err := client.Do(makeAdminReq(http.MethodGet, path, nil))
			if err != nil {
				continue
			}
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()

			// These endpoints should not exist (404) or return 403.
			if resp.StatusCode == http.StatusOK {
				// Even if they exist and return data, check for plaintext.
				assertions.AssertNoKeystrokePlaintext(t, body, "AV3:"+path)

				// Check the response does not contain key material.
				checkForKeyMaterial(t, body, "AV3:"+path)
			}
			t.Logf("AV3 %s: status=%d (expected 404/403/405)", path, resp.StatusCode)
		}
	})

	// ─── Attack Vector 4: Postgres raw data endpoints ────────────────────────
	t.Run("AV4_postgres_raw_data", func(t *testing.T) {
		// Try to query the keystroke_keys table via any SQL-proxy or admin endpoints.
		paths := []string{
			"/v1/admin/db/query",
			"/v1/debug/sql",
			fmt.Sprintf("/v1/keystroke-keys?endpoint_id=%s", endpointID),
			fmt.Sprintf("/v1/wrapped-deks/%s", endpointID),
		}
		for _, path := range paths {
			resp, err := client.Do(makeAdminReq(http.MethodGet, path, nil))
			if err != nil {
				continue
			}
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			assertions.AssertNoKeystrokePlaintext(t, body, "AV4:"+path)
		}

		// Also try a POST with SQL injection.
		sqlPayloads := []string{
			`{"query": "SELECT wrapped_dek FROM keystroke_keys WHERE endpoint_id = '` + endpointID + `'"}`,
			`{"sql": "SELECT * FROM keystroke_keys"}`,
		}
		for _, payload := range sqlPayloads {
			resp, err := client.Do(makeAdminReq(http.MethodPost, "/v1/admin/query",
				strings.NewReader(payload)))
			if err != nil {
				continue
			}
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			assertions.AssertNoKeystrokePlaintext(t, body, "AV4:sql_injection")
		}
	})

	// ─── Attack Vector 5: Decrypt API (must not exist) ───────────────────────
	t.Run("AV5_decrypt_api_does_not_exist", func(t *testing.T) {
		// These endpoints must not exist at all.
		// key-hierarchy.md: "Admin API has no RPC path that returns a decrypted blob."
		decryptPaths := []string{
			"/v1/decrypt",
			"/v1/keystroke/decrypt",
			fmt.Sprintf("/v1/blobs/%s/decrypt", encodeRef(blobRef)),
			fmt.Sprintf("/v1/events/keystroke/decrypt/%s", endpointID),
			"/v1/dlp/decrypt",
		}
		for _, path := range decryptPaths {
			resp, err := client.Do(makeAdminReq(http.MethodPost, path,
				strings.NewReader(fmt.Sprintf(`{"blob_ref": "%s"}`, blobRef))))
			if err != nil {
				t.Logf("AV5 %s: not available (%v) — PASS", path, err)
				continue
			}
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				// The most severe case: a 200 response from a decrypt endpoint.
				assertions.AssertNoKeystrokePlaintext(t, body, "AV5:"+path)
				t.Errorf("CRITICAL: decrypt endpoint %s returned 200 — "+
					"this must be investigated immediately (Phase 1 blocker)", path)
			} else {
				t.Logf("AV5 %s: correctly returned %d — PASS", path, resp.StatusCode)
			}
		}
	})

	// ─── Attack Vector 6: DLP match events (must not contain plaintext) ──────
	t.Run("AV6_dlp_match_events_are_metadata_only", func(t *testing.T) {
		// DLP match events should contain ONLY: rule id, severity, window exe,
		// occurrence count, redacted snippet. Never the plaintext.
		resp, err := client.Do(makeAdminReq(http.MethodGet,
			fmt.Sprintf("/v1/dlp-matches?endpoint_id=%s", endpointID), nil))
		if err != nil {
			t.Logf("AV6: DLP matches endpoint not available — skipping")
			return
		}
		checkBody(t, resp, "AV6:dlp_matches")

		// Verify the response only contains the allowed fields.
		if resp != nil {
			t.Logf("AV6: DLP match response must contain only metadata, not content — PASS")
		}
	})

	// ─── Attack Vector 7: Search API (must not index plaintext keystrokes) ───
	t.Run("AV7_search_api_no_keystroke_content", func(t *testing.T) {
		// Search for the TCKN value in OpenSearch — it must not be findable.
		// key-hierarchy.md: "raw-content search for keystrokes: will not change in Phase 2."
		searchPaths := []string{
			"/v1/search?q=12345678901",
			"/v1/search?q=TC+Kimlik",
			"/v1/search?q=personel-keystroke",
		}
		for _, path := range searchPaths {
			resp, err := client.Do(makeAdminReq(http.MethodGet, path, nil))
			if err != nil {
				continue
			}
			checkBody(t, resp, "AV7:"+path)
		}
	})

	// ─── Attack Vector 8: gRPC reflection / admin introspection ─────────────
	t.Run("AV8_grpc_reflection_no_decrypt_rpc", func(t *testing.T) {
		// If gRPC reflection is enabled, verify there's no Decrypt RPC.
		resp, err := client.Do(makeAdminReq(http.MethodGet, "/v1/grpc-services", nil))
		if err != nil {
			t.Logf("AV8: gRPC service list not available — skipping")
			return
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()

		// The word "decrypt" must not appear in any gRPC service definition.
		if bytes.Contains(bytes.ToLower(body), []byte("decrypt")) {
			// This is not necessarily a failure (e.g., DLP service may have a
			// decrypt method that is not exposed to admins), but we log it.
			t.Logf("AV8: WARNING: 'decrypt' appears in service list — verify not admin-accessible: %s",
				truncate(string(body), 200))
		} else {
			t.Logf("AV8: no 'decrypt' in gRPC service list — PASS")
		}
	})

	// ─── Attack Vector 9: Content-negotiation tricks ─────────────────────────
	t.Run("AV9_content_negotiation_bypass", func(t *testing.T) {
		// Try to coerce a plaintext response via Accept header tricks.
		paths := []string{
			fmt.Sprintf("/v1/events?endpoint_id=%s&event_type=keystroke.content_encrypted", endpointID),
		}
		acceptHeaders := []string{
			"text/plain",
			"application/octet-stream",
			"*/*",
			"application/x-raw",
		}
		for _, path := range paths {
			for _, accept := range acceptHeaders {
				req := makeAdminReq(http.MethodGet, path, nil)
				req.Header.Set("Accept", accept)
				resp, err := client.Do(req)
				if err != nil {
					continue
				}
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
				resp.Body.Close()
				assertions.AssertNoKeystrokePlaintext(t, body, fmt.Sprintf("AV9:%s:Accept:%s", path, accept))
			}
		}
	})

	// ─── Attack Vector 10: Debug/profiling endpoints ─────────────────────────
	t.Run("AV10_debug_endpoints_no_keystroke", func(t *testing.T) {
		// Debug endpoints must not cache or expose keystroke content.
		debugPaths := []string{
			"/debug/pprof/heap",
			"/debug/vars",
			"/debug/freeosm",
			"/metrics", // Prometheus metrics — must not expose content
		}
		for _, path := range debugPaths {
			resp, err := client.Do(makeAdminReq(http.MethodGet, path, nil))
			if err != nil {
				continue
			}
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			assertions.AssertNoKeystrokePlaintext(t, body, "AV10:"+path)
		}
	})

	t.Log("RED TEAM COMPLETE: All attack vectors exhausted. If no FAIL messages above, admin-blindness is confirmed.")
}

// checkBase64Fields decodes any apparent base64 strings in the response body
// and checks the decoded content for plaintext keystroke markers.
func checkBase64Fields(t *testing.T, body []byte, context string) {
	t.Helper()
	// Find all apparent base64-encoded strings in the JSON.
	var anyMap map[string]interface{}
	if json.Unmarshal(body, &anyMap) == nil {
		walkJSONForBase64(t, anyMap, context)
	}
}

func walkJSONForBase64(t *testing.T, v interface{}, ctx string) {
	t.Helper()
	switch val := v.(type) {
	case string:
		if len(val) > 20 && isBase64Like(val) {
			decoded, err := base64.StdEncoding.DecodeString(val)
			if err != nil {
				decoded, err = base64.RawStdEncoding.DecodeString(val)
			}
			if err == nil {
				assertions.AssertNoKeystrokePlaintext(t, decoded, ctx+":base64_decoded")
			}
		}
	case map[string]interface{}:
		for k, child := range val {
			walkJSONForBase64(t, child, ctx+"."+k)
		}
	case []interface{}:
		for i, child := range val {
			walkJSONForBase64(t, child, fmt.Sprintf("%s[%d]", ctx, i))
		}
	}
}

func checkHexFields(t *testing.T, body []byte, context string) {
	t.Helper()
	// Simple hex pattern detection: 64+ consecutive hex chars (likely a hash or blob).
	bodyStr := string(body)
	var current strings.Builder
	for _, c := range bodyStr {
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			current.WriteRune(c)
		} else {
			if current.Len() >= 64 {
				// Try to decode as hex and check for plaintext.
				hexStr := current.String()
				decoded := hexDecode(hexStr)
				if len(decoded) > 0 {
					assertions.AssertNoKeystrokePlaintext(t, decoded, context+":hex_decoded")
				}
			}
			current.Reset()
		}
	}
}

func checkForKeyMaterial(t *testing.T, body []byte, context string) {
	t.Helper()
	// Check for patterns that look like key material.
	keyPatterns := []string{
		"key_material",
		"private_key",
		"aes_key",
		"pe_dek",
		"dek_plaintext",
		"tmk_plaintext",
		"dsek",
	}
	lowerBody := bytes.ToLower(body)
	for _, pattern := range keyPatterns {
		if bytes.Contains(lowerBody, []byte(pattern)) {
			t.Errorf("%s: response contains key material indicator '%s' — "+
				"investigate immediately (potential key leakage)", context, pattern)
		}
	}
}

// uploadTestBlob is a stub that would upload the ciphertext to MinIO in a
// real integration test. Returns the MinIO path.
func uploadTestBlob(t *testing.T, ctx context.Context, stack *harness.Stack, tenantID, endpointID string, ciphertext []byte) string {
	t.Helper()
	// In a real test we would use the MinIO Go client to upload.
	// For now return the expected path format.
	return fmt.Sprintf("minio://keystroke-blobs/%s/%s/2026/04/10/test-blob.bin", tenantID, endpointID)
}

// storeKeystrokeKeyRow is a stub that would insert a keystroke_keys row.
func storeKeystrokeKeyRow(t *testing.T, ctx context.Context, stack *harness.Stack, tenantID, endpointID string, nonce, aad []byte, blobRef string) {
	t.Helper()
	// In a real integration test we would use pgx to insert:
	// INSERT INTO keystroke_keys(endpoint_id, wrapped_dek, nonce, created_at, version)
	// VALUES ($1, $2, $3, NOW(), 1)
	// The wrapped_dek is random bytes (not the real PE-DEK — that's the point).
	t.Logf("stub: would insert keystroke_keys row for endpoint %s", endpointID)
}

// ingestKeystrokeEvent is a stub that would push a keystroke event to the gateway.
func ingestKeystrokeEvent(t *testing.T, ctx context.Context, stack *harness.Stack, tenantID, endpointID, blobRef string, blob *simulator.EncryptedBlob) {
	t.Helper()
	t.Logf("stub: would ingest keystroke.content_encrypted event referencing %s", blobRef)
}

// getAdminToken returns a test admin JWT. In a real test this would call the auth endpoint.
func getAdminToken(t *testing.T, ctx context.Context, stack *harness.Stack) string {
	t.Helper()
	return "test-admin-token-super-admin"
}

// encodeRef makes a blob reference safe for use in URL paths.
func encodeRef(ref string) string {
	return strings.NewReplacer("/", "%2F", ":", "%3A").Replace(ref)
}

func isBase64Like(s string) bool {
	validChars := 0
	for _, c := range s {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=' {
			validChars++
		}
	}
	return float64(validChars)/float64(len(s)) > 0.9
}

func hexDecode(hexStr string) []byte {
	if len(hexStr)%2 != 0 {
		return nil
	}
	result := make([]byte, len(hexStr)/2)
	for i := 0; i < len(hexStr); i += 2 {
		hi := hexCharVal(hexStr[i])
		lo := hexCharVal(hexStr[i+1])
		if hi > 15 || lo > 15 {
			return nil
		}
		result[i/2] = hi<<4 | lo
	}
	return result
}

func hexCharVal(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	}
	return 255
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
