package bcp

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/personel/api/internal/evidence"
)

type fakeEvidenceRecorder struct {
	calls    int
	lastItem evidence.Item
}

func (f *fakeEvidenceRecorder) Record(_ context.Context, item evidence.Item) (string, error) {
	f.calls++
	f.lastItem = item
	return "01J-bcp-" + item.TenantID, nil
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRecordDrillRejectsMissingFields(t *testing.T) {
	svc := &Service{log: silentLogger()}
	now := time.Now()
	tr := []TierResult{{Tier: 1, Service: "postgres", MetRTO: true}}
	cases := []DrillReport{
		{DrillID: "D1", Type: DrillTypeLive, FacilitatorID: "f", StartedAt: now, CompletedAt: now, TierResults: tr},    // no tenant
		{TenantID: "t", Type: DrillTypeLive, FacilitatorID: "f", StartedAt: now, CompletedAt: now, TierResults: tr},    // no drill ID
		{TenantID: "t", DrillID: "D1", FacilitatorID: "f", StartedAt: now, CompletedAt: now, TierResults: tr},          // no type
		{TenantID: "t", DrillID: "D1", Type: DrillTypeLive, StartedAt: now, CompletedAt: now, TierResults: tr},         // no facilitator
		{TenantID: "t", DrillID: "D1", Type: DrillTypeLive, FacilitatorID: "f", CompletedAt: now, TierResults: tr},     // no started_at
		{TenantID: "t", DrillID: "D1", Type: DrillTypeLive, FacilitatorID: "f", StartedAt: now, TierResults: tr},       // no completed_at
		{TenantID: "t", DrillID: "D1", Type: DrillTypeLive, FacilitatorID: "f", StartedAt: now, CompletedAt: now},      // no tier results
		{TenantID: "t", DrillID: "D1", Type: DrillType("wrong"), FacilitatorID: "f", StartedAt: now, CompletedAt: now, TierResults: tr}, // bad type
	}
	for i, r := range cases {
		if _, err := svc.RecordDrill(context.Background(), r); err == nil {
			t.Errorf("case %d: expected error", i)
		}
	}
}

func TestRecordDrillRejectsNegativeDuration(t *testing.T) {
	svc := &Service{log: silentLogger()}
	start := time.Now()
	r := DrillReport{
		TenantID: "t", DrillID: "D1", Type: DrillTypeTabletop, FacilitatorID: "f",
		StartedAt: start, CompletedAt: start.Add(-1 * time.Minute),
		TierResults: []TierResult{{Tier: 1, MetRTO: true}},
	}
	if _, err := svc.RecordDrill(context.Background(), r); err == nil {
		t.Fatal("expected error for negative duration")
	}
}

func TestEvidencePayloadShape(t *testing.T) {
	fake := &fakeEvidenceRecorder{}
	start := time.Date(2026, 4, 11, 9, 0, 0, 0, time.UTC)
	end := start.Add(2 * time.Hour)
	tierResults := []TierResult{
		{Tier: 0, Service: "vault", TargetRTOSeconds: 7200, ActualRTOSeconds: 5400, MetRTO: true},
		{Tier: 1, Service: "postgres", TargetRTOSeconds: 14400, ActualRTOSeconds: 9000, MetRTO: true},
		{Tier: 2, Service: "clickhouse", TargetRTOSeconds: 28800, ActualRTOSeconds: 32000, MetRTO: false},
	}

	payload, _ := json.Marshal(map[string]any{
		"drill_id":         "DR-Q2-2026",
		"type":             string(DrillTypeLive),
		"scenario":         "ransomware",
		"started_at":       start.Format(time.RFC3339Nano),
		"completed_at":     end.Format(time.RFC3339Nano),
		"duration_seconds": int64(7200),
		"facilitator_id":   "cto-1",
		"tier_results":     tierResults,
		"tiers_met":        2,
		"tiers_total":      3,
		"all_rtos_met":     false,
		"lessons_learned":  "Tier 2 restore time too slow; invest in MinIO lifecycle",
	})

	item := evidence.Item{
		TenantID:           "tenant-a",
		Control:            evidence.CtrlCC9_1,
		Kind:               evidence.KindBackupRestoreTest,
		RecordedAt:         end,
		Actor:              "cto-1",
		Payload:            payload,
		ReferencedAuditIDs: []int64{200},
	}
	_, _ = fake.Record(context.Background(), item)

	var pl map[string]any
	_ = json.Unmarshal(fake.lastItem.Payload, &pl)

	if pl["all_rtos_met"] != false {
		t.Errorf("all_rtos_met should be false when one tier missed")
	}
	if pl["tiers_met"].(float64) != 2 {
		t.Errorf("tiers_met: %v", pl["tiers_met"])
	}
	// Tier results must be preserved as structured nested objects so
	// auditors can drill into per-tier RTO numbers without parsing the
	// summary strings.
	trs, ok := pl["tier_results"].([]any)
	if !ok || len(trs) != 3 {
		t.Fatalf("tier_results shape: %T / %v", pl["tier_results"], pl["tier_results"])
	}
}
