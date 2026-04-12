// Package assertions provides domain-specific test assertions for the Personel
// QA framework, built on top of testify.
package assertions

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AssertNoKeystrokePlaintext asserts that a byte slice contains no unencrypted
// keystroke content. This is the core assertion for the admin-blindness guarantee:
// any API response that could conceivably contain keystroke data must pass this
// check.
//
// Checks performed:
//  1. The body does not contain any known TCKN pattern (11 consecutive digits).
//  2. The body does not contain the test marker "personel-keystroke-plaintext".
//  3. The body does not contain the synthetic TCKN test value "12345678901".
func AssertNoKeystrokePlaintext(t *testing.T, body []byte, msg string) {
	t.Helper()

	// Check 1: no raw 11-digit sequence (TCKN-like).
	if containsTCKNPattern(body) {
		t.Errorf("%s: response body contains TCKN-like plaintext (11 consecutive digits)", msg)
	}

	// Check 2: no test marker.
	if containsBytes(body, []byte("personel-keystroke-plaintext")) {
		t.Errorf("%s: response body contains keystroke plaintext marker", msg)
	}

	// Check 3: no test TCKN.
	if containsBytes(body, []byte("12345678901")) {
		t.Errorf("%s: response body contains test TCKN value '12345678901'", msg)
	}
}

// AuditChainT is the minimal testing interface required by AssertAuditChainIntact.
// *testing.T satisfies this interface, as does any mock testing type that
// implements Helper, Errorf, and FailNow.
type AuditChainT interface {
	Helper()
	Errorf(format string, args ...interface{})
	FailNow()
}

// AssertAuditChainIntact verifies that a sequence of audit records forms a
// valid hash chain. Each record must contain this_hash = SHA256(seq || payload_hash || prev_hash).
func AssertAuditChainIntact(t AuditChainT, records []AuditRecord) {
	t.Helper()
	require.NotEmpty(t, records, "audit chain must not be empty")

	for i, rec := range records {
		if i == 0 {
			// First record: prev_hash should be zero or the chain anchor.
			continue
		}
		prev := records[i-1]
		expectedPrevHash := prev.ThisHash
		assert.Equal(t, expectedPrevHash, rec.PrevHash,
			"audit record %d: prev_hash does not match previous record's this_hash", i)

		// Verify this_hash.
		computed := computeAuditHash(rec.Seq, rec.PayloadHash, rec.PrevHash)
		assert.Equal(t, hex.EncodeToString(computed), hex.EncodeToString(rec.ThisHash),
			"audit record %d: this_hash is invalid", i)
	}
}

// AuditRecord is a row from the audit_records table used for chain verification.
type AuditRecord struct {
	ID          int64
	Seq         int64
	Type        string
	PayloadHash []byte
	PrevHash    []byte
	ThisHash    []byte
	CreatedAt   time.Time
}

// computeAuditHash computes SHA256(seq_bigendian || payload_hash || prev_hash)
// matching the formula in live-view-protocol.md §Audit Hash Chain.
func computeAuditHash(seq int64, payloadHash, prevHash []byte) []byte {
	h := sha256.New()
	// seq as 8-byte big-endian.
	seqBytes := [8]byte{
		byte(seq >> 56), byte(seq >> 48), byte(seq >> 40), byte(seq >> 32),
		byte(seq >> 24), byte(seq >> 16), byte(seq >> 8), byte(seq),
	}
	h.Write(seqBytes[:])
	h.Write(payloadHash)
	h.Write(prevHash)
	return h.Sum(nil)
}

// AssertEventLossRateBelow asserts that the event loss rate is below the
// threshold. sent and received are counts from the simulation.
// Phase 1 exit criterion #6: < 0.01% loss.
func AssertEventLossRateBelow(t *testing.T, sent, received int64, maxLossPct float64) {
	t.Helper()
	require.Greater(t, sent, int64(0), "sent event count must be > 0")

	lossCount := sent - received
	if lossCount < 0 {
		lossCount = 0
	}
	lossPct := float64(lossCount) / float64(sent) * 100

	assert.LessOrEqualf(t, lossPct, maxLossPct,
		"event loss rate %.4f%% exceeds threshold %.4f%% (sent=%d, received=%d, lost=%d)",
		lossPct, maxLossPct, sent, received, lossCount)
}

// AssertP95Below asserts that the 95th percentile of a sample is below the
// threshold duration.
func AssertP95Below(t *testing.T, samples []time.Duration, threshold time.Duration, name string) {
	t.Helper()
	require.NotEmpty(t, samples, "%s: no samples to evaluate", name)

	sorted := make([]time.Duration, len(samples))
	copy(sorted, samples)
	sortDurations(sorted)

	idx := int(float64(len(sorted)) * 0.95)
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	p95 := sorted[idx]

	assert.LessOrEqualf(t, p95, threshold,
		"%s: p95 latency %v exceeds threshold %v", name, p95, threshold)
}

// AssertCPUPercentBelow asserts average CPU is below the threshold.
// Phase 1 exit criterion #2: < 2%.
func AssertCPUPercentBelow(t *testing.T, samples []float64, maxPct float64) {
	t.Helper()
	require.NotEmpty(t, samples, "no CPU samples")

	var sum float64
	for _, s := range samples {
		sum += s
	}
	avg := sum / float64(len(samples))
	assert.LessOrEqualf(t, avg, maxPct,
		"average CPU %.2f%% exceeds Phase 1 target %.2f%%", avg, maxPct)
}

// AssertRSSBelow asserts that peak RSS is below the threshold.
// Phase 1 exit criterion #3: < 150 MB.
func AssertRSSBelow(t *testing.T, peakBytes uint64, maxBytes uint64) {
	t.Helper()
	assert.LessOrEqualf(t, peakBytes, maxBytes,
		"peak RSS %d bytes (%d MB) exceeds Phase 1 target %d bytes (%d MB)",
		peakBytes, peakBytes/1024/1024,
		maxBytes, maxBytes/1024/1024)
}

// AssertLiveViewDualControl asserts that a live-view session was approved by a
// different user than the requester (dual-control requirement from
// live-view-protocol.md).
func AssertLiveViewDualControl(t *testing.T, requesterID, approverID string) {
	t.Helper()
	assert.NotEmpty(t, requesterID, "live-view requester ID must not be empty")
	assert.NotEmpty(t, approverID, "live-view approver ID must not be empty")
	assert.NotEqualf(t, requesterID, approverID,
		"live-view approver (%s) must be different from requester (%s)",
		approverID, requesterID)
}

// AssertDSRWithinSLA asserts that a DSR created at createdAt is not overdue
// given the current time. Phase 1 exit criterion #20: 30-day SLA.
func AssertDSRWithinSLA(t *testing.T, createdAt, now time.Time, slaDays int) {
	t.Helper()
	deadline := createdAt.AddDate(0, 0, slaDays)
	assert.Truef(t, now.Before(deadline),
		"DSR created at %v has exceeded %d-day SLA (deadline: %v, now: %v)",
		createdAt, slaDays, deadline, now)
}

// AssertKeyVersionHandshakeRefusal asserts that a gateway response indicates
// rekey was requested (RotateCert with reason="rekey") when the agent presents
// a stale TMK or PE-DEK version. This validates key-hierarchy.md §Key Version
// Handshake steps 2 and 3.
func AssertKeyVersionHandshakeRefusal(t *testing.T, gotRotateCert bool, gotReason string) {
	t.Helper()
	assert.Truef(t, gotRotateCert,
		"gateway must send RotateCert when agent presents stale key version")
	assert.Equalf(t, "rekey", gotReason,
		"RotateCert reason must be 'rekey' for key version mismatch")
}

// Helper functions.

func containsTCKNPattern(data []byte) bool {
	count := 0
	for _, b := range data {
		if b >= '0' && b <= '9' {
			count++
			if count >= 11 {
				return true
			}
		} else {
			count = 0
		}
	}
	return false
}

func containsBytes(haystack, needle []byte) bool {
	if len(needle) == 0 || len(haystack) < len(needle) {
		return false
	}
	for i := 0; i <= len(haystack)-len(needle); i++ {
		match := true
		for j, b := range needle {
			if haystack[i+j] != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func sortDurations(d []time.Duration) {
	// Simple insertion sort — samples are small in test scenarios.
	for i := 1; i < len(d); i++ {
		for j := i; j > 0 && d[j] < d[j-1]; j-- {
			d[j], d[j-1] = d[j-1], d[j]
		}
	}
}

// FormatPassFail returns a pass/fail string for a threshold check.
func FormatPassFail(name string, value, threshold float64, unit string) string {
	pass := value <= threshold
	status := "PASS"
	if !pass {
		status = "FAIL"
	}
	return fmt.Sprintf("[%s] %s: %.4f%s (threshold: %.4f%s)", status, name, value, unit, threshold, unit)
}
