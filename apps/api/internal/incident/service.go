// Package incident — evidence collector for closed security incidents.
//
// Maps to SOC 2 CC7.3 (incident detection + response) and anchors the
// 5-tier severity model defined in docs/policies/incident-response.md.
// When an incident is closed with a post-incident review, the service
// records the lifecycle artefacts (timeline, classification, actions
// taken, lessons learned) as an evidence item. KVKK 72h and GDPR
// Art. 33 notification timestamps are captured in the payload so a
// compliance auditor can verify both legal clocks were honoured.
package incident

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/evidence"
)

// Severity is the 5-tier classification from docs/policies/incident-response.md.
type Severity string

const (
	SeverityInformational Severity = "informational"
	SeverityLow           Severity = "low"
	SeverityMedium        Severity = "medium"
	SeverityHigh          Severity = "high"
	SeverityCritical      Severity = "critical"
)

// IncidentReport is submitted after a post-incident review is complete.
type IncidentReport struct {
	TenantID string `json:"tenant_id"`

	// IncidentID is the platform-assigned ID (UUID or ticket number).
	IncidentID string `json:"incident_id"`

	// Severity is the final classification agreed during the PIR.
	Severity Severity `json:"severity"`

	// DetectedAt is when the incident was first observed (not reported).
	DetectedAt time.Time `json:"detected_at"`

	// ContainedAt is when the incident stopped spreading. May equal
	// DetectedAt for instant containment.
	ContainedAt time.Time `json:"contained_at"`

	// ClosedAt is when the PIR was signed off.
	ClosedAt time.Time `json:"closed_at"`

	// LeadResponderID is the IR team lead for this incident.
	LeadResponderID string `json:"lead_responder_id"`

	// Summary is a short human-readable description — what happened,
	// one paragraph. Surfaced in the evidence summary.
	Summary string `json:"summary"`

	// KVKKNotifiedAt is when the data subject / Kurul notification was
	// made, if the incident triggered the 72h KVKK clock. Zero otherwise.
	KVKKNotifiedAt time.Time `json:"kvkk_notified_at,omitempty"`

	// GDPRNotifiedAt is when the GDPR Art. 33 notification was filed,
	// if relevant. Zero otherwise.
	GDPRNotifiedAt time.Time `json:"gdpr_notified_at,omitempty"`

	// RootCause is the PIR's root cause finding.
	RootCause string `json:"root_cause"`

	// RemediationActions is a list of concrete follow-up items.
	RemediationActions []string `json:"remediation_actions"`
}

type Service struct {
	recorder         *audit.Recorder
	evidenceRecorder evidence.Recorder
	log              *slog.Logger
}

func NewService(rec *audit.Recorder, er evidence.Recorder, log *slog.Logger) *Service {
	return &Service{recorder: rec, evidenceRecorder: er, log: log}
}

// RecordClosure records the closed-incident evidence item.
func (s *Service) RecordClosure(ctx context.Context, r IncidentReport) (string, error) {
	if r.TenantID == "" || r.IncidentID == "" || r.LeadResponderID == "" {
		return "", fmt.Errorf("incident: tenant_id, incident_id, lead_responder_id required")
	}
	if r.Severity == "" {
		return "", fmt.Errorf("incident: severity required")
	}
	if r.DetectedAt.IsZero() || r.ClosedAt.IsZero() {
		return "", fmt.Errorf("incident: detected_at and closed_at required")
	}
	if r.ClosedAt.Before(r.DetectedAt) {
		return "", fmt.Errorf("incident: closed_at must be >= detected_at")
	}

	// If KVKK notification was triggered, check 72h compliance and
	// include a flag in the payload. A late notification is still
	// recorded — auditors need to see the overdue notification for
	// CC7.3 evidence of incident handling, even if imperfect.
	kvkkWithin72h := true
	if !r.KVKKNotifiedAt.IsZero() {
		kvkkWithin72h = r.KVKKNotifiedAt.Sub(r.DetectedAt) <= 72*time.Hour
	}
	gdprWithin72h := true
	if !r.GDPRNotifiedAt.IsZero() {
		gdprWithin72h = r.GDPRNotifiedAt.Sub(r.DetectedAt) <= 72*time.Hour
	}

	auditID, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    r.LeadResponderID,
		TenantID: r.TenantID,
		Action:   audit.ActionIncidentClosed,
		Target:   fmt.Sprintf("incident:%s", r.IncidentID),
		Details: map[string]any{
			"severity":         string(r.Severity),
			"kvkk_within_72h":  kvkkWithin72h,
			"gdpr_within_72h":  gdprWithin72h,
			"remediation_items": len(r.RemediationActions),
		},
	})
	if err != nil {
		return "", fmt.Errorf("incident: audit: %w", err)
	}

	if s.evidenceRecorder == nil {
		return "", nil
	}

	containmentSeconds := int64(r.ContainedAt.Sub(r.DetectedAt).Seconds())
	resolutionSeconds := int64(r.ClosedAt.Sub(r.DetectedAt).Seconds())

	payload, err := json.Marshal(map[string]any{
		"incident_id":         r.IncidentID,
		"severity":            string(r.Severity),
		"detected_at":         r.DetectedAt.Format(time.RFC3339Nano),
		"contained_at":        r.ContainedAt.Format(time.RFC3339Nano),
		"closed_at":           r.ClosedAt.Format(time.RFC3339Nano),
		"containment_seconds": containmentSeconds,
		"resolution_seconds":  resolutionSeconds,
		"lead_responder_id":   r.LeadResponderID,
		"summary":             r.Summary,
		"root_cause":          r.RootCause,
		"remediation_actions": r.RemediationActions,
		"kvkk_notified_at":    formatOptionalTime(r.KVKKNotifiedAt),
		"kvkk_within_72h":     kvkkWithin72h,
		"gdpr_notified_at":    formatOptionalTime(r.GDPRNotifiedAt),
		"gdpr_within_72h":     gdprWithin72h,
	})
	if err != nil {
		s.log.ErrorContext(ctx, "incident: evidence payload marshal failed",
			slog.String("error", err.Error()))
		return "", nil
	}

	item := evidence.Item{
		TenantID:   r.TenantID,
		Control:    evidence.CtrlCC7_3,
		Kind:       evidence.KindIncidentReport,
		RecordedAt: r.ClosedAt,
		Actor:      r.LeadResponderID,
		SummaryTR: fmt.Sprintf(
			"Olay kapatıldı — %s, şiddet %s, çözüm süresi %ds",
			r.IncidentID, r.Severity, resolutionSeconds,
		),
		SummaryEN: fmt.Sprintf(
			"Incident closed — %s severity=%s resolution=%ds",
			r.IncidentID, r.Severity, resolutionSeconds,
		),
		Payload:            payload,
		ReferencedAuditIDs: []int64{auditID},
	}

	id, err := s.evidenceRecorder.Record(ctx, item)
	if err != nil {
		s.log.ErrorContext(ctx, "incident: SOC 2 evidence emission failed",
			slog.String("incident_id", r.IncidentID),
			slog.String("error", err.Error()))
		return "", nil
	}
	return id, nil
}

func formatOptionalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}
