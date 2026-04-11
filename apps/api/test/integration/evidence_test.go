//go:build integration

package integration

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/personel/api/internal/evidence"
)

// fakeWORM is an in-memory EvidenceWORM used so the integration test can
// exercise the full Phase 3.0 data plane (migration 0025 + RLS + dual-write
// + CountByControl + ListByPeriod + PackBuilder) against a real Postgres
// testcontainer without also spinning up MinIO + Object Lock semantics.
// Object Lock behaviour is covered by a separate MinIO integration suite.
type fakeWORM struct {
	puts map[string][]byte
}

func newFakeWORM() *fakeWORM {
	return &fakeWORM{puts: make(map[string][]byte)}
}

func (f *fakeWORM) PutEvidence(_ context.Context, tenantID, period, id string, canonical []byte) (string, error) {
	key := "evidence/" + tenantID + "/" + period + "/" + id + ".bin"
	f.puts[key] = append([]byte(nil), canonical...)
	return key, nil
}

// fakeSigner produces SHA-256-based deterministic signatures so manifest
// and item signatures can be asserted byte-for-byte across runs AND the
// companion fakeVerifier can detect payload drift. Real Vault returns
// Ed25519 but the structural invariant (payload → signature → verify) is
// the same.
type fakeSigner struct{}

func (fakeSigner) Sign(_ context.Context, payload []byte) ([]byte, string, error) {
	h := sha256.Sum256(payload)
	out := append([]byte("sig:control-plane-signing:v1:"), h[:]...)
	return out, "control-plane-signing:v1", nil
}

// Verify lets the integration test check that every stored item's
// signature can be recomputed from its canonical form. Guards against
// the CollectionPeriod-at-sign-time bug discovered 2026-04-11.
func (fakeSigner) Verify(_ context.Context, payload, signature []byte, keyVersion string) error {
	if keyVersion != "control-plane-signing:v1" {
		return fmt.Errorf("unknown key version: %s", keyVersion)
	}
	h := sha256.Sum256(payload)
	expect := append([]byte("sig:control-plane-signing:v1:"), h[:]...)
	if !bytes.Equal(signature, expect) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

func TestEvidence_EndToEnd(t *testing.T) {
	pool := testDB(t)
	log := testLogger(t)
	ctx := context.Background()

	tenantID := seedTenant(t, pool, "soc2-e2e")

	// RLS requires personel.tenant_id to be set on the session for every
	// evidence read. For inserts we rely on the explicit tenant_id column.
	_, err := pool.Exec(ctx,
		"SELECT set_config('personel.tenant_id', $1, false)", tenantID)
	require.NoError(t, err, "set RLS tenant")

	worm := newFakeWORM()
	store := evidence.NewStore(pool, worm)
	rec := evidence.NewRecorder(store, fakeSigner{}, log)

	// ── Act 1: emit three evidence items covering three controls ──────────
	// Mirrors what lv/policy/dsr collectors would do but exercises the
	// data plane directly so this test stays focused on evidence + store.
	now := time.Now().UTC()
	items := []evidence.Item{
		{
			TenantID:   tenantID,
			Control:    evidence.CtrlCC6_1,
			Kind:       evidence.KindPrivilegedAccessSession,
			RecordedAt: now,
			Actor:      "admin-7",
			SummaryTR:  "Canlı izleme kapatıldı — test",
			SummaryEN:  "Live view closed — test",
			Payload:    json.RawMessage(`{"session_id":"sess-1","actual_seconds":720}`),
		},
		{
			TenantID:   tenantID,
			Control:    evidence.CtrlCC8_1,
			Kind:       evidence.KindChangeAuthorization,
			RecordedAt: now.Add(1 * time.Second),
			Actor:      "admin-7",
			SummaryTR:  "Politika yayını — test",
			SummaryEN:  "Policy push — test",
			Payload:    json.RawMessage(`{"policy_id":"pol-1","target_endpoint":"*"}`),
		},
		{
			TenantID:   tenantID,
			Control:    evidence.CtrlP7_1,
			Kind:       evidence.KindComplianceAttestation,
			RecordedAt: now.Add(2 * time.Second),
			Actor:      "dpo-3",
			SummaryTR:  "KVKK m.11 talebi kapatıldı — test",
			SummaryEN:  "DSR fulfilled — test",
			Payload:    json.RawMessage(`{"dsr_id":"dsr-1","within_sla":true}`),
		},
	}
	var recordedIDs []string
	for i, it := range items {
		id, err := rec.Record(ctx, it)
		require.NoErrorf(t, err, "record item %d", i)
		assert.NotEmpty(t, id, "item %d ID", i)
		recordedIDs = append(recordedIDs, id)
	}

	// ── Act 2: WORM fake received every canonical payload ─────────────────
	assert.Len(t, worm.puts, 3, "WORM should have 3 evidence objects")
	for _, id := range recordedIDs {
		found := false
		for key := range worm.puts {
			if bytes.Contains([]byte(key), []byte(id)) {
				found = true
				break
			}
		}
		assert.Truef(t, found, "item %s missing from WORM bucket", id)
	}

	// ── Act 3: Postgres evidence_items rows exist with WORM anchor ─────────
	var pgCount int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM evidence_items WHERE tenant_id = $1",
		tenantID).Scan(&pgCount))
	assert.Equal(t, 3, pgCount)

	var nullWormKeys int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM evidence_items WHERE tenant_id = $1 AND worm_key IS NULL",
		tenantID).Scan(&nullWormKeys))
	assert.Equal(t, 0, nullWormKeys, "worm_key must be NOT NULL for every row")

	// ── Act 4: CountByControl returns the coverage matrix ─────────────────
	period := now.Format("2006-01")
	counts, err := store.CountByControl(ctx, tenantID, period)
	require.NoError(t, err)
	assert.Equal(t, 1, counts[evidence.CtrlCC6_1])
	assert.Equal(t, 1, counts[evidence.CtrlCC8_1])
	assert.Equal(t, 1, counts[evidence.CtrlP7_1])

	// ── Act 5: ListByPeriod returns all items and filter works ────────────
	all, err := store.ListByPeriod(ctx, tenantID, period, nil)
	require.NoError(t, err)
	assert.Len(t, all, 3)

	// ── Round-trip signature verification — regression guard for the
	// CollectionPeriod-at-sign-time bug found 2026-04-11. Every stored
	// item must re-canonicalise to the same bytes that were signed, so
	// a Verify call against the stored Signature must succeed. If this
	// ever fails, the signature integrity invariant is broken and the
	// SOC 2 chain is unusable — treat as a blocking bug.
	for _, stored := range all {
		err := evidence.VerifyItem(ctx, fakeSigner{}, stored)
		assert.NoErrorf(t, err, "round-trip verify failed for item %s (control=%s): %v",
			stored.ID, stored.Control, err)
		// Every stored item must have CollectionPeriod populated —
		// empty value would mean Store.Insert used the default path
		// without the Recorder setting it first.
		assert.NotEmpty(t, stored.CollectionPeriod, "stored item %s has empty collection_period", stored.ID)
	}

	cc61Only, err := store.ListByPeriod(ctx, tenantID, period, []evidence.ControlID{evidence.CtrlCC6_1})
	require.NoError(t, err)
	assert.Len(t, cc61Only, 1)
	assert.Equal(t, evidence.CtrlCC6_1, cc61Only[0].Control)
	assert.Equal(t, evidence.KindPrivilegedAccessSession, cc61Only[0].Kind)

	// ── Act 6: PackBuilder streams a valid ZIP with signed manifest ────────
	builder := evidence.NewPackBuilder(store, fakeSigner{})
	var zipBuf bytes.Buffer
	manifest, err := builder.Build(ctx, &zipBuf, evidence.PackRequest{
		TenantID:    tenantID,
		PeriodStart: mustParsePeriod(t, period),
		PeriodEnd:   mustParsePeriod(t, period).AddDate(0, 1, 0),
	}, "dpo-7")
	require.NoError(t, err)
	assert.Equal(t, 3, manifest.ItemCount)
	assert.Equal(t, "dpo-7", manifest.GeneratedBy)
	assert.ElementsMatch(t,
		[]string{"CC6.1", "CC8.1", "P7.1"},
		manifest.ControlsCovered,
	)

	zr, err := zip.NewReader(bytes.NewReader(zipBuf.Bytes()), int64(zipBuf.Len()))
	require.NoError(t, err)

	wantFiles := []string{
		"manifest.json",
		"manifest.signature",
		"manifest.key_version.txt",
	}
	for _, id := range recordedIDs {
		wantFiles = append(wantFiles,
			"items/"+id+".json",
			"items/"+id+".signature",
		)
	}

	have := map[string]bool{}
	for _, f := range zr.File {
		have[f.Name] = true
	}
	for _, w := range wantFiles {
		assert.Truef(t, have[w], "zip missing %s", w)
	}

	// Parse manifest.json from the ZIP and verify its contents match
	// what Build returned — the two must be identical since Build
	// writes the manifest after constructing it.
	mf, err := zr.Open("manifest.json")
	require.NoError(t, err)
	defer mf.Close()
	mBytes, err := io.ReadAll(mf)
	require.NoError(t, err)

	var parsed evidence.PackManifest
	require.NoError(t, json.Unmarshal(mBytes, &parsed))
	assert.Equal(t, manifest.ItemCount, parsed.ItemCount)
	assert.Equal(t, manifest.TenantID, parsed.TenantID)
	for _, row := range parsed.Items {
		assert.Contains(t, row.WORMObjectKey, "evidence/"+tenantID+"/")
		assert.Contains(t, row.WORMObjectKey, ".bin")
	}

	// Key version file must hold the signer's key version.
	kf, err := zr.Open("manifest.key_version.txt")
	require.NoError(t, err)
	defer kf.Close()
	kBytes, err := io.ReadAll(kf)
	require.NoError(t, err)
	assert.Equal(t, "control-plane-signing:v1", string(kBytes))
}

// TestEvidence_RejectsNilWORM proves the Store refuses to write when the
// WORM sink is nil — Phase 3.0 invariant that no evidence lands in
// Postgres without a tamper-evident anchor.
func TestEvidence_RejectsNilWORM(t *testing.T) {
	pool := testDB(t)
	log := testLogger(t)
	ctx := context.Background()
	tenantID := seedTenant(t, pool, "soc2-nil-worm")
	_, _ = pool.Exec(ctx, "SELECT set_config('personel.tenant_id', $1, false)", tenantID)

	store := evidence.NewStore(pool, nil) // nil WORM
	rec := evidence.NewRecorder(store, fakeSigner{}, log)

	_, err := rec.Record(ctx, evidence.Item{
		TenantID: tenantID,
		Control:  evidence.CtrlCC6_1,
		Kind:     evidence.KindPrivilegedAccessSession,
		Actor:    "admin",
	})
	assert.Error(t, err, "nil WORM must reject writes")

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT COUNT(*) FROM evidence_items WHERE tenant_id = $1",
		tenantID).Scan(&count))
	assert.Equal(t, 0, count, "no row must exist when WORM rejected")
}

// TestEvidence_RLSIsolatesTenants verifies migration 0025's RLS policy
// prevents tenant A from reading tenant B's evidence even via the same
// pool connection — as long as personel.tenant_id is set correctly.
func TestEvidence_RLSIsolatesTenants(t *testing.T) {
	pool := testDB(t)
	log := testLogger(t)
	ctx := context.Background()

	tenantA := seedTenant(t, pool, "soc2-rls-a")
	tenantB := seedTenant(t, pool, "soc2-rls-b")

	worm := newFakeWORM()
	store := evidence.NewStore(pool, worm)
	rec := evidence.NewRecorder(store, fakeSigner{}, log)

	// Insert one item per tenant.
	for _, tid := range []string{tenantA, tenantB} {
		_, err := rec.Record(ctx, evidence.Item{
			TenantID:   tid,
			Control:    evidence.CtrlCC6_1,
			Kind:       evidence.KindPrivilegedAccessSession,
			RecordedAt: time.Now().UTC(),
			Actor:      "admin",
			SummaryTR:  "test",
			SummaryEN:  "test",
		})
		require.NoError(t, err)
	}

	period := time.Now().UTC().Format("2006-01")

	// Session scoped to tenantA → should see only 1 item.
	_, err := pool.Exec(ctx, "SELECT set_config('personel.tenant_id', $1, false)", tenantA)
	require.NoError(t, err)
	aItems, err := store.ListByPeriod(ctx, tenantA, period, nil)
	require.NoError(t, err)
	assert.Len(t, aItems, 1)

	// Session scoped to tenantB → should see only 1 item (its own).
	_, err = pool.Exec(ctx, "SELECT set_config('personel.tenant_id', $1, false)", tenantB)
	require.NoError(t, err)
	bItems, err := store.ListByPeriod(ctx, tenantB, period, nil)
	require.NoError(t, err)
	assert.Len(t, bItems, 1)

	// And they must be distinct items.
	assert.NotEqual(t, aItems[0].ID, bItems[0].ID)
}

func mustParsePeriod(t *testing.T, period string) time.Time {
	t.Helper()
	ts, err := time.Parse("2006-01", period)
	require.NoError(t, err)
	return ts
}
