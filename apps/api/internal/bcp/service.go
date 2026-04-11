// Package bcp — evidence collector for BCP / DR drills.
//
// Maps to SOC 2 CC9.1 (business continuity) and anchors the annual live
// drill + quarterly tabletop cadence defined in
// docs/policies/business-continuity-disaster-recovery.md. Recording the
// drill outcome with the actual RTO achieved vs the target RTO is the
// core CC9.1 evidence: auditors care that drills happen on schedule and
// that real-world recovery times stay within commitments.
package bcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/evidence"
)

// DrillType distinguishes live drills from tabletop exercises. Live
// drills are annual, tabletops are quarterly.
type DrillType string

const (
	DrillTypeLive     DrillType = "live"
	DrillTypeTabletop DrillType = "tabletop"
)

// Tier0–3 references the BCDR policy's tiering. A drill usually targets
// one or more tiers; the test result is recorded per tier so a drill
// that exercises Tier 0 + Tier 1 produces one evidence item listing
// both results.
type TierResult struct {
	Tier              int    `json:"tier"`
	Service           string `json:"service"`
	TargetRTOSeconds  int64  `json:"target_rto_seconds"`
	ActualRTOSeconds  int64  `json:"actual_rto_seconds"`
	MetRTO            bool   `json:"met_rto"`
	Notes             string `json:"notes,omitempty"`
}

type DrillReport struct {
	TenantID string `json:"tenant_id"`

	// DrillID is the platform-assigned identifier.
	DrillID string `json:"drill_id"`

	// Type is live vs tabletop.
	Type DrillType `json:"type"`

	// Scenario is a short tag identifying what was simulated
	// ("ransomware", "vault_compromise", "clickhouse_loss", etc.).
	Scenario string `json:"scenario"`

	// StartedAt / CompletedAt frame the drill duration.
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`

	// FacilitatorID is the CTO / Platform Lead who ran the drill.
	FacilitatorID string `json:"facilitator_id"`

	// TierResults lists the per-tier outcomes. At least one required.
	TierResults []TierResult `json:"tier_results"`

	// LessonsLearned is the PIR output — required for CC9.1 evidence.
	LessonsLearned string `json:"lessons_learned"`
}

type Service struct {
	recorder         *audit.Recorder
	evidenceRecorder evidence.Recorder
	log              *slog.Logger
}

func NewService(rec *audit.Recorder, er evidence.Recorder, log *slog.Logger) *Service {
	return &Service{recorder: rec, evidenceRecorder: er, log: log}
}

func (s *Service) RecordDrill(ctx context.Context, r DrillReport) (string, error) {
	if r.TenantID == "" || r.DrillID == "" || r.FacilitatorID == "" {
		return "", fmt.Errorf("bcp: tenant_id, drill_id, facilitator_id required")
	}
	if r.Type != DrillTypeLive && r.Type != DrillTypeTabletop {
		return "", fmt.Errorf("bcp: type must be live or tabletop")
	}
	if len(r.TierResults) == 0 {
		return "", fmt.Errorf("bcp: at least one tier_result required")
	}
	if r.StartedAt.IsZero() || r.CompletedAt.IsZero() {
		return "", fmt.Errorf("bcp: started_at and completed_at required")
	}
	if r.CompletedAt.Before(r.StartedAt) {
		return "", fmt.Errorf("bcp: completed_at must be >= started_at")
	}

	metCount := 0
	for _, tr := range r.TierResults {
		if tr.MetRTO {
			metCount++
		}
	}
	allMet := metCount == len(r.TierResults)

	auditID, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    r.FacilitatorID,
		TenantID: r.TenantID,
		Action:   audit.ActionBCPDrillCompleted,
		Target:   fmt.Sprintf("drill:%s", r.DrillID),
		Details: map[string]any{
			"type":        string(r.Type),
			"scenario":    r.Scenario,
			"tier_count":  len(r.TierResults),
			"rto_all_met": allMet,
		},
	})
	if err != nil {
		return "", fmt.Errorf("bcp: audit: %w", err)
	}

	if s.evidenceRecorder == nil {
		return "", nil
	}

	payload, err := json.Marshal(map[string]any{
		"drill_id":         r.DrillID,
		"type":             string(r.Type),
		"scenario":         r.Scenario,
		"started_at":       r.StartedAt.Format(time.RFC3339Nano),
		"completed_at":     r.CompletedAt.Format(time.RFC3339Nano),
		"duration_seconds": int64(r.CompletedAt.Sub(r.StartedAt).Seconds()),
		"facilitator_id":   r.FacilitatorID,
		"tier_results":     r.TierResults,
		"tiers_met":        metCount,
		"tiers_total":      len(r.TierResults),
		"all_rtos_met":     allMet,
		"lessons_learned":  r.LessonsLearned,
	})
	if err != nil {
		s.log.ErrorContext(ctx, "bcp: evidence payload marshal failed",
			slog.String("error", err.Error()))
		return "", nil
	}

	item := evidence.Item{
		TenantID:   r.TenantID,
		Control:    evidence.CtrlCC9_1,
		Kind:       evidence.KindBackupRestoreTest,
		RecordedAt: r.CompletedAt,
		Actor:      r.FacilitatorID,
		SummaryTR: fmt.Sprintf(
			"BCP tatbikatı tamamlandı — %s, %s senaryosu, %d/%d tier RTO karşılandı",
			r.DrillID, r.Scenario, metCount, len(r.TierResults),
		),
		SummaryEN: fmt.Sprintf(
			"BCP drill completed — %s scenario=%s met=%d/%d tiers",
			r.DrillID, r.Scenario, metCount, len(r.TierResults),
		),
		Payload:            payload,
		ReferencedAuditIDs: []int64{auditID},
	}

	id, err := s.evidenceRecorder.Record(ctx, item)
	if err != nil {
		s.log.ErrorContext(ctx, "bcp: SOC 2 evidence emission failed",
			slog.String("drill_id", r.DrillID),
			slog.String("error", err.Error()))
		return "", nil
	}
	return id, nil
}
