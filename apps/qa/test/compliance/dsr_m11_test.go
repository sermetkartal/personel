//go:build compliance

// Faz 14 #153 — KVKK Madde 11 DSR lifecycle compliance test.
//
// KVKK 6698 Article 11 grants data subjects the right to:
//   (a) learn whether their personal data is processed,
//   (b) request information about processing,
//   (c) learn the purpose of processing and whether used per purpose,
//   (d) know third parties their data is shared with,
//   (e) request correction of inaccurate data,
//   (f) request erasure or destruction,
//   (g) request notification to third parties of corrections/erasures,
//   (h) object to analysis-based decisions that adversely affect them,
//   (i) claim indemnity for damages arising from unlawful processing.
//
// This test validates the Personel fulfilment path end-to-end:
//
//   1. Data subject submits request (via portal or admin API)
//   2. DPO receives it in the queue within 1 minute
//   3. DPO marks it "accepted" — SLA timer starts (30-day KVKK floor)
//   4. Fulfilment generates the signed response artifact within the
//      SLA
//   5. Artifact is WORM-sealed in audit-worm bucket
//   6. audit_log has signed entries for: submitted, accepted,
//      fulfilled
//   7. evidence_items row is created under P7.1 (Phase 3.0)
//
// Reference: docs/compliance/kvkk-framework.md §3, §7
package compliance

import (
	"context"
	"testing"
	"time"
)

func TestKVKK_M11_AccessRequest_LifecycleWithinSLA(t *testing.T) {
	// KVKK m.11(a)+(b): access + information request.
	ctx := context.Background()
	_ = ctx

	// Scenario matrix (asserted when the compliance runner is
	// wired; scaffold today):
	//
	// Step 1. POST /v1/portal/dsr/requests with kind=access,
	//         subject_id=compliance-test-subject-1,
	//         legal_basis="KVKK m.11(a)".
	// Step 2. Verify DSR row state=submitted, SLA deadline = +30d.
	// Step 3. Impersonate DPO → POST /v1/dsr/requests/{id}/accept.
	// Step 4. Impersonate DPO → POST /v1/dsr/requests/{id}/respond
	//         with an inline response JSON.
	// Step 5. GET /v1/dsr/requests/{id} → state=fulfilled,
	//         closed_at < sla_deadline.
	// Step 6. Audit log chain must contain 3 rows matching the
	//         actions: dsr.submitted, dsr.accepted, dsr.fulfilled.
	// Step 7. evidence_items row under P7.1 must exist with
	//         within_sla=true.
	t.Log("KVKK m.11(a)+(b) access scaffold — asserts in test runner")
}

func TestKVKK_M11_ErasureRequest_CascadesCorrectly(t *testing.T) {
	// KVKK m.11(f): erasure request.
	//
	// Erasure must cascade to:
	//   - Postgres rows (soft delete with tombstone)
	//   - ClickHouse events (partition-level delete)
	//   - MinIO objects (object lock allowing; audit-worm NOT erased)
	//   - OpenSearch indices (document delete by query)
	//   - Keycloak user (delete)
	//
	// Legal hold flag check: if the subject is under legal hold,
	// erasure MUST fail with state=blocked_legal_hold, NOT
	// state=fulfilled. Test both branches.
	t.Log("KVKK m.11(f) erasure scaffold — asserts in test runner")
}

func TestKVKK_M11_ResponseTime_OverdueTriggersAudit(t *testing.T) {
	// KVKK m.11 + Kurul guidance: 30 days max for data controller
	// response. When SLA is breached, an audit_log entry
	// "dsr.overdue" must fire and the P7.1 evidence item records
	// within_sla=false.
	//
	// This test uses a test clock to fast-forward past the SLA
	// without waiting 30 real days.
	//
	// Requires clock injection hook in dsr.Service — Faz 6 #72
	// adjacent work.
	_ = time.Now
	t.Log("KVKK m.11 overdue scaffold — requires clock injection")
}
