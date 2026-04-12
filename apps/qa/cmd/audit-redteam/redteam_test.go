// redteam_test.go — Phase 1 exit criterion #9 test scaffold.
//
// This file contains Go test functions that correspond to the three key
// red-team scenarios for keystroke admin-blindness:
//
//  1. Admin role scanning: all /v1/events endpoints must never return raw
//     keystroke content in any field.
//  2. DPO role scanning: same guarantees apply to DPO — DPO sees audit/DSR
//     data but not keystroke plaintext.
//  3. Direct DB schema check: the events table schema must not expose a
//     plaintext content column (i.e., kind='keystroke.content_encrypted'
//     rows must have ciphertext, not UTF-8 plaintext).
//
// These tests are pure unit/scaffold tests that do NOT require a running stack.
// They validate the structural properties of the red team runner itself and
// provide compile-time verification that all attack vector names and the
// result shape are correct. The real integration execution is triggered by
// `audit-redteam --api $URL` against a live stack.
//
// Run with:
//
//	go test ./cmd/audit-redteam/...
package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// ── Scenario 1: Admin-role scan ───────────────────────────────────────────────

// TestAdminRoleScan_AllVectorsPresent verifies that the runner covers all
// required attack vectors from the Phase 1 exit criteria specification.
// If a vector is missing, the red team is incomplete.
func TestAdminRoleScan_AllVectorsPresent(t *testing.T) {
	required := []string{
		"AV1: direct event query",
		"AV2: MinIO proxy via API",
		"AV3: Vault transit key export",
		"AV4: Postgres raw data",
		"AV5: decrypt API existence",
		"AV6: DLP match events metadata-only",
		"AV7: search API no keystroke content",
		"AV8: gRPC reflection no decrypt RPC",
		"AV9: content-negotiation bypass",
		"AV10: debug endpoints no keystroke",
	}

	runner := &redTeamRunner{
		apiBase:    "http://localhost:8080",
		tenantID:   "test-tenant",
		endpointID: "test-endpoint",
	}

	// Run against the stub implementation (no live stack needed).
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("runner.Run should not fail in stub mode: %v", err)
	}

	// Build a set of tested vector names.
	tested := make(map[string]bool, len(result.Details))
	for _, vr := range result.Details {
		tested[vr.Name] = true
	}

	// Every required vector must be present.
	for _, req := range required {
		if !tested[req] {
			t.Errorf("required attack vector %q not found in runner output", req)
		}
	}
}

// TestAdminRoleScan_AllVectorsPassInStubMode verifies that the stub
// implementation reports all vectors as PASSED. This is the baseline:
// when no live stack is present, the runner's stub returns "no plaintext
// found" for every vector because it doesn't actually send HTTP requests.
func TestAdminRoleScan_AllVectorsPassInStubMode(t *testing.T) {
	runner := &redTeamRunner{
		apiBase:    "http://localhost:8080",
		tenantID:   "stub-tenant",
		endpointID: "stub-endpoint",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("runner.Run stub: %v", err)
	}

	if !result.Passed {
		t.Errorf("stub mode must pass all vectors, failed: %v", result.FailedVectors)
	}

	if result.TestedVectors == 0 {
		t.Error("runner must test at least one vector")
	}
	if result.PassedVectors != result.TestedVectors {
		t.Errorf("in stub mode all tested vectors must pass: tested=%d passed=%d",
			result.TestedVectors, result.PassedVectors)
	}
}

// TestAdminRoleScan_ResultShape verifies the result structure is well-formed.
func TestAdminRoleScan_ResultShape(t *testing.T) {
	runner := &redTeamRunner{
		apiBase:    "http://localhost:8080",
		tenantID:   "shape-tenant",
		endpointID: "shape-endpoint",
	}

	ctx := context.Background()
	result, err := runner.Run(ctx)
	if err != nil {
		t.Fatalf("runner.Run: %v", err)
	}

	if result.TestedVectors != len(result.Details) {
		t.Errorf("TestedVectors=%d must equal len(Details)=%d",
			result.TestedVectors, len(result.Details))
	}

	passedCount := 0
	for _, vr := range result.Details {
		if vr.Passed {
			passedCount++
		}
		if vr.Name == "" {
			t.Error("every vectorResult must have a non-empty Name")
		}
	}

	if result.PassedVectors != passedCount {
		t.Errorf("PassedVectors=%d does not match count of passed Details=%d",
			result.PassedVectors, passedCount)
	}
}

// ── Scenario 2: DPO-role scan ─────────────────────────────────────────────────

// TestDPORoleScan_SameGuaranteesAsAdmin asserts that a DPO token must produce
// the same admin-blindness guarantees. DPO may see more metadata (DSR data,
// legal holds) than Admin, but must NEVER see keystroke plaintext.
// This test validates the attack vector taxonomy is role-agnostic.
func TestDPORoleScan_SameGuaranteesAsAdmin(t *testing.T) {
	// The runner uses a single token; in a real integration test we would
	// pass a DPO JWT here. The structural check is that the vector set
	// (and thus the guarantees) are identical regardless of which
	// privileged role is being impersonated.

	adminRunner := &redTeamRunner{
		apiBase:    "http://localhost:8080",
		tenantID:   "dpo-tenant",
		endpointID: "dpo-endpoint",
	}
	dpoRunner := &redTeamRunner{
		apiBase:    "http://localhost:8080",
		tenantID:   "dpo-tenant",
		endpointID: "dpo-endpoint",
	}

	ctx := context.Background()
	adminResult, err := adminRunner.Run(ctx)
	if err != nil {
		t.Fatalf("admin runner: %v", err)
	}
	dpoResult, err := dpoRunner.Run(ctx)
	if err != nil {
		t.Fatalf("dpo runner: %v", err)
	}

	// Both must test the same number of vectors.
	if adminResult.TestedVectors != dpoResult.TestedVectors {
		t.Errorf("admin and DPO must test the same number of attack vectors: admin=%d dpo=%d",
			adminResult.TestedVectors, dpoResult.TestedVectors)
	}

	// Build sorted vector name sets.
	adminVectors := vectorNames(adminResult)
	dpoVectors := vectorNames(dpoResult)

	for name := range adminVectors {
		if _, ok := dpoVectors[name]; !ok {
			t.Errorf("DPO scan missing vector %q that admin scan has", name)
		}
	}
}

// TestDPORoleScan_KeystrokeContentFieldAbsent verifies that the response
// schema asserter correctly detects forbidden fields. This is a unit test
// for the assertion helper itself.
func TestDPORoleScan_KeystrokeContentFieldAbsent(t *testing.T) {
	// Simulate a hypothetical API response that contains ONLY allowed fields.
	allowed := map[string]any{
		"event_id":   "evt-001",
		"event_type": "keystroke.content_encrypted",
		"endpoint_id": "ep-001",
		"kind":       "keystroke.content_encrypted",
		// blob reference and nonce are metadata, not content.
		"blob_ref": "minio://keystroke-blobs/tenant/ep/2026/04/10/blob.bin",
		"nonce":    "base64-encoded-nonce==",
	}
	body, _ := json.Marshal(allowed)
	assertNoKeystrokePlaintext(t, body, "allowed-fields-test")

	// Simulate a response that leaks content (this must fail the assertion).
	// We test the assertion function directly rather than t.Run so we can
	// capture failures.
	leaked := map[string]any{
		"event_id":         "evt-002",
		"event_type":       "keystroke.content_encrypted",
		"keystroke_content": "user typed: password123", // FORBIDDEN
	}
	leakedBody, _ := json.Marshal(leaked)

	// Verify our detection catches this.
	if !bodyContainsKeystrokePlaintext(leakedBody) {
		t.Error("asserter must detect keystroke_content field as forbidden plaintext")
	}
}

// ── Scenario 3: Direct DB schema check ────────────────────────────────────────

// TestDBSchema_KeystrokeContentEncryptedRowStructure validates the expected
// Postgres schema constraint: for rows where kind='keystroke.content_encrypted',
// the content column (if it exists) must contain ciphertext, not UTF-8 text.
//
// This is a structural test that validates the SQL query the red team would
// run against a real Postgres instance. The query itself is correct Go code
// that would be executed against a live stack.
func TestDBSchema_KeystrokeContentEncryptedRowStructure(t *testing.T) {
	// The canonical SQL query to execute against a live Postgres:
	//   SELECT id, content
	//   FROM events
	//   WHERE kind = 'keystroke.content_encrypted'
	//   LIMIT 10;
	//
	// The assertion: for every row, content must NOT be valid UTF-8 text
	// that contains readable keyboard sequences. It should be opaque
	// binary (ciphertext) that starts with an AES-GCM nonce.

	// Since we don't have a live stack in unit tests, we validate the
	// assertion logic with synthetic data.

	type EventRow struct {
		ID      string
		Kind    string
		Content []byte // should be ciphertext
	}

	// Simulate a correctly encrypted row.
	encryptedRow := EventRow{
		ID:      "evt-001",
		Kind:    "keystroke.content_encrypted",
		Content: syntheticCiphertext(),
	}

	if isPlaintextContent(encryptedRow.Content) {
		t.Error("encrypted row content must not be detected as plaintext")
	}

	// Simulate a misconfigured row with plaintext content (this should fail).
	plaintextRow := EventRow{
		ID:      "evt-002",
		Kind:    "keystroke.content_encrypted",
		Content: []byte("user typed: TCKN 12345678901 şifre: hunter2"),
	}

	if !isPlaintextContent(plaintextRow.Content) {
		t.Error("plaintext row content must be detected as plaintext (red team catch)")
	}
}

// TestDBSchema_SQLQueryIsCorrectSyntax validates that the red-team DB query
// string is syntactically correct (structural test only — no DB required).
func TestDBSchema_SQLQueryIsCorrectSyntax(t *testing.T) {
	query := redTeamDBQuery()

	// Must reference the correct table and column names.
	if !strings.Contains(query, "events") {
		t.Error("red team DB query must reference 'events' table")
	}
	if !strings.Contains(query, "keystroke.content_encrypted") {
		t.Error("red team DB query must filter on kind='keystroke.content_encrypted'")
	}
	if !strings.Contains(query, "SELECT") {
		t.Error("red team DB query must be a SELECT statement")
	}
	if !strings.Contains(query, "content") {
		t.Error("red team DB query must select the 'content' column to inspect it")
	}
}

// ── Helper assertions (scaffold — would call real code in live tests) ──────────

// assertNoKeystrokePlaintext checks that the response body does not contain
// any of the forbidden plaintext indicators for keystroke content.
// In production this delegates to assertions.AssertNoKeystrokePlaintext.
func assertNoKeystrokePlaintext(t *testing.T, body []byte, ctx string) {
	t.Helper()
	if bodyContainsKeystrokePlaintext(body) {
		t.Errorf("%s: response contains forbidden keystroke plaintext indicators", ctx)
	}
}

// bodyContainsKeystrokePlaintext is the core detection logic.
// It checks for field names and content patterns that would indicate
// that the API leaked decrypted keystroke data.
func bodyContainsKeystrokePlaintext(body []byte) bool {
	// Field name indicators — if these appear in a response JSON, something is wrong.
	forbiddenFields := []string{
		"keystroke_content",
		"content_plaintext",
		"decrypted_content",
		"raw_keystrokes",
		"keystroke_text",
	}
	lowerBody := strings.ToLower(string(body))
	for _, field := range forbiddenFields {
		if strings.Contains(lowerBody, field) {
			return true
		}
	}
	return false
}

// isPlaintextContent returns true if the byte slice looks like human-readable
// UTF-8 text (and therefore is NOT ciphertext). AES-GCM ciphertext is
// essentially random bytes and will fail UTF-8 validation at high rates.
func isPlaintextContent(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	// A valid UTF-8 string with many printable characters is plaintext.
	printable := 0
	for _, r := range string(b) {
		if r >= 0x20 && r < 0x7F {
			printable++
		}
	}
	// If >70% of bytes are printable ASCII, treat as plaintext.
	return float64(printable)/float64(len(b)) > 0.70
}

// syntheticCiphertext returns a plausible AES-256-GCM ciphertext blob.
// 12 byte nonce + random-looking encrypted content (0x00..0xFF cycle).
func syntheticCiphertext() []byte {
	// 12-byte nonce.
	nonce := []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67}
	// Ciphertext looks like non-repeating binary.
	ct := make([]byte, 64)
	for i := range ct {
		ct[i] = byte((i*7 + 13) % 256)
	}
	return append(nonce, ct...)
}

// redTeamDBQuery returns the canonical SQL to execute against a live Postgres
// instance during the direct DB check. This is the query that would be run by
// a DBA-level user in a controlled red team exercise.
func redTeamDBQuery() string {
	return `
SELECT
    id,
    kind,
    content,
    tenant_id,
    endpoint_id,
    created_at
FROM events
WHERE kind = 'keystroke.content_encrypted'
ORDER BY created_at DESC
LIMIT 10`
}

// vectorNames extracts the set of attack vector names from a result.
func vectorNames(r *redTeamResult) map[string]struct{} {
	out := make(map[string]struct{}, len(r.Details))
	for _, vr := range r.Details {
		out[vr.Name] = struct{}{}
	}
	return out
}

// ── ReportWriter scaffold ─────────────────────────────────────────────────────

// TestReportWriter_WriteDoesNotPanic ensures the writeReport function compiles
// and executes without panicking even when the output directory doesn't exist.
func TestReportWriter_WriteDoesNotPanic(t *testing.T) {
	result := &redTeamResult{
		Passed:        true,
		TestedVectors: 2,
		PassedVectors: 2,
		Details: []vectorResult{
			{Name: "AV1", Passed: true, Details: "ok"},
			{Name: "AV2", Passed: true, Details: "ok"},
		},
	}

	dir := t.TempDir()

	// Should not panic even when Passed=true.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("writeReport panicked: %v", r)
		}
	}()

	_ = writeReport(result, dir)
}

// TestPrintRedTeamResult_DoesNotPanic ensures printRedTeamResult compiles and runs.
func TestPrintRedTeamResult_DoesNotPanic(t *testing.T) {
	result := &redTeamResult{
		Passed:        false,
		FailedVectors: []string{"AV3"},
		TestedVectors: 3,
		PassedVectors: 2,
		Details: []vectorResult{
			{Name: "AV1", Passed: true, Details: "clean"},
			{Name: "AV2", Passed: true, Details: "clean"},
			{Name: "AV3", Passed: false, Details: "leaked plaintext"},
		},
	}

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("printRedTeamResult panicked: %v", r)
		}
	}()

	printRedTeamResult(result)
}
