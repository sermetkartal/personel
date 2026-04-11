// Package dsr — DSR service layer.
package dsr

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"log/slog"

	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/evidence"
)

// Service orchestrates DSR business logic.
type Service struct {
	store    *Store
	recorder *audit.Recorder
	notifier Notifier
	log      *slog.Logger

	// evidenceRecorder is optional — when wired, every successful
	// DSR Respond (KVKK m.11 fulfilment) emits a KindComplianceAttestation
	// evidence item mapped to controls P5.1 (consent) and P7.1 (retention
	// limits). KVKK auditors rely on this stream to verify 30-day SLA
	// adherence across the observation window.
	evidenceRecorder evidence.Recorder
}

// NewService creates the DSR service.
func NewService(store *Store, rec *audit.Recorder, notifier Notifier, log *slog.Logger) *Service {
	return &Service{store: store, recorder: rec, notifier: notifier, log: log}
}

// SetEvidenceRecorder attaches the SOC 2 / KVKK evidence recorder. Optional;
// if never called the service operates without evidence emission.
func (s *Service) SetEvidenceRecorder(r evidence.Recorder) {
	s.evidenceRecorder = r
}

// SubmitInput is the data required to create a DSR.
type SubmitInput struct {
	TenantID       string
	EmployeeUserID string
	RequestType    RequestType
	ScopeJSON      map[string]any
	Justification  string
	ActorIP        string
	ActorUA        string
}

// Submit creates a new DSR and audits the event.
func (s *Service) Submit(ctx context.Context, in SubmitInput) (*Request, error) {
	scopeBytes, err := json.Marshal(in.ScopeJSON)
	if err != nil {
		return nil, fmt.Errorf("dsr: marshal scope: %w", err)
	}

	now := time.Now().UTC()
	req := &Request{
		TenantID:       in.TenantID,
		EmployeeUserID: in.EmployeeUserID,
		RequestType:    in.RequestType,
		ScopeJSON:      scopeBytes,
		Justification:  in.Justification,
		State:          StateOpen,
		CreatedAt:      now,
	}

	// Audit BEFORE the side effect.
	_, err = s.recorder.Append(ctx, audit.Entry{
		Actor:    in.EmployeeUserID,
		ActorUA:  in.ActorUA,
		TenantID: in.TenantID,
		Action:   audit.ActionDSRSubmitted,
		Target:   fmt.Sprintf("employee:%s", in.EmployeeUserID),
		Details: map[string]any{
			"request_type":  string(in.RequestType),
			"justification": in.Justification,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("dsr: audit submit: %w", err)
	}

	id, err := s.store.Create(ctx, req)
	if err != nil {
		return nil, err
	}
	req.ID = id
	req.SLADeadline = now.AddDate(0, 0, 30)

	// Notify DPO.
	_ = s.notifier.NotifyDPO(ctx, in.TenantID, id, "submitted")
	_ = s.notifier.NotifyEmployee(ctx, in.TenantID, id, in.EmployeeUserID, "submitted")

	return req, nil
}

// Assign sets the handler for a DSR.
func (s *Service) Assign(ctx context.Context, tenantID, id, assignerID, assigneeID string) error {
	_, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    assignerID,
		TenantID: tenantID,
		Action:   audit.ActionDSRAssigned,
		Target:   fmt.Sprintf("dsr:%s", id),
		Details:  map[string]any{"assigned_to": assigneeID},
	})
	if err != nil {
		return err
	}
	return s.store.Assign(ctx, id, tenantID, assigneeID)
}

// Respond closes a DSR with a response artifact.
func (s *Service) Respond(ctx context.Context, tenantID, id, actorID, artifactRef string) error {
	req, auditID, err := s.auditAndRespond(ctx, tenantID, id, actorID, artifactRef)
	if err != nil {
		return err
	}
	s.emitRespondEvidence(ctx, req, actorID, artifactRef, auditID)
	return nil
}

// emitRespondEvidence records a KindComplianceAttestation evidence item for
// a successfully fulfilled DSR. This is the primary KVKK m.11 / GDPR Art. 28
// audit trail: for each data subject request, there must be a signed,
// time-bounded record of the response within the 30-day SLA.
//
// Control mapping: the DSR response closes the loop on the P5.1 (choice and
// consent) and P7.1 (use and retention) controls. We emit against P7.1
// because the typical DSR is an erasure/retention-limit request; P5.1 is
// cited in the payload's control_tags so a future evidence pack can filter
// either direction.
func (s *Service) emitRespondEvidence(
	ctx context.Context,
	req *Request,
	actorID, artifactRef string,
	respondAuditID int64,
) {
	if s.evidenceRecorder == nil || req == nil {
		return
	}

	now := time.Now().UTC()
	withinSLA := now.Before(req.SLADeadline) || now.Equal(req.SLADeadline)
	secondsBeforeDeadline := int64(req.SLADeadline.Sub(now).Seconds())

	payload, err := json.Marshal(map[string]any{
		"dsr_id":                   req.ID,
		"request_type":             string(req.RequestType),
		"employee_user_id":         req.EmployeeUserID,
		"scope":                    json.RawMessage(req.ScopeJSON),
		"created_at":               req.CreatedAt.Format(time.RFC3339Nano),
		"sla_deadline":             req.SLADeadline.Format(time.RFC3339Nano),
		"closed_at":                now.Format(time.RFC3339Nano),
		"within_sla":               withinSLA,
		"seconds_before_deadline":  secondsBeforeDeadline,
		"response_artifact_ref":    artifactRef,
		"responded_by":             actorID,
		"extended":                 req.ExtendedAt != nil,
		"control_tags":             []string{"P5.1", "P7.1"},
		"regulation":               "KVKK m.11 / GDPR Art. 28",
	})
	if err != nil {
		s.log.ErrorContext(ctx, "dsr: evidence payload marshal failed",
			slog.String("dsr_id", req.ID),
			slog.String("error", err.Error()),
		)
		return
	}

	slaTag := "within_sla"
	if !withinSLA {
		slaTag = "OVERDUE"
	}

	item := evidence.Item{
		TenantID:   req.TenantID,
		Control:    evidence.CtrlP7_1,
		Kind:       evidence.KindComplianceAttestation,
		RecordedAt: now,
		Actor:      actorID,
		SummaryTR: fmt.Sprintf(
			"KVKK m.11 talebi kapatıldı — DSR %s, tür %s, durum %s",
			req.ID, string(req.RequestType), slaTag,
		),
		SummaryEN: fmt.Sprintf(
			"DSR fulfilled — id=%s type=%s sla=%s",
			req.ID, string(req.RequestType), slaTag,
		),
		Payload:            payload,
		ReferencedAuditIDs: []int64{respondAuditID},
		AttachmentRefs:     []string{artifactRef},
	}

	if _, err := s.evidenceRecorder.Record(ctx, item); err != nil {
		// An overdue DSR that also fails its evidence write is a loud
		// compliance incident — the KVKK 30-day clock is gone AND we
		// cannot prove fulfilment to the auditor. Log at error level
		// regardless of withinSLA so the monitoring pipeline pages on
		// either condition.
		s.log.ErrorContext(ctx, "dsr: SOC 2 / KVKK evidence emission failed",
			slog.String("dsr_id", req.ID),
			slog.Bool("within_sla", withinSLA),
			slog.String("error", err.Error()),
		)
	}
}

func (s *Service) auditAndRespond(ctx context.Context, tenantID, id, actorID, artifactRef string) (*Request, int64, error) {
	auditID, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    actorID,
		TenantID: tenantID,
		Action:   audit.ActionDSRResponded,
		Target:   fmt.Sprintf("dsr:%s", id),
		Details:  map[string]any{"artifact_ref": artifactRef},
	})
	if err != nil {
		return nil, 0, err
	}
	auditRef := fmt.Sprintf("audit:%d", auditID)
	if err := s.store.Respond(ctx, id, tenantID, artifactRef, auditRef); err != nil {
		return nil, 0, err
	}
	req, err := s.store.Get(ctx, id, tenantID)
	return req, auditID, err
}

// Reject closes a DSR with a rejection reason.
func (s *Service) Reject(ctx context.Context, tenantID, id, actorID, reason string) error {
	_, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    actorID,
		TenantID: tenantID,
		Action:   audit.ActionDSRRejected,
		Target:   fmt.Sprintf("dsr:%s", id),
		Details:  map[string]any{"reason": reason},
	})
	if err != nil {
		return err
	}
	return s.store.Reject(ctx, id, tenantID, reason)
}

// Extend adds a 30-day extension.
func (s *Service) Extend(ctx context.Context, tenantID, id, actorID, reason string) error {
	_, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    actorID,
		TenantID: tenantID,
		Action:   audit.ActionDSRExtended,
		Target:   fmt.Sprintf("dsr:%s", id),
		Details:  map[string]any{"reason": reason},
	})
	if err != nil {
		return err
	}
	return s.store.Extend(ctx, id, tenantID, reason)
}

// Get returns a single DSR.
func (s *Service) Get(ctx context.Context, tenantID, id string) (*Request, error) {
	return s.store.Get(ctx, id, tenantID)
}

// GetScoped returns a DSR only if it belongs to the given employeeUserID.
// Returns (nil, nil) when the DSR is not found or belongs to a different user.
// The caller should treat a nil result as a 404 — never expose a 403 distinction
// to prevent information leakage about other users' DSR IDs.
func (s *Service) GetScoped(ctx context.Context, tenantID, employeeUserID, id string) (*Request, error) {
	req, err := s.store.Get(ctx, id, tenantID)
	if err != nil {
		// Treat not-found as nil (caller surfaces 404).
		return nil, nil //nolint:nilerr
	}
	if req == nil || req.EmployeeUserID != employeeUserID {
		return nil, nil
	}

	_, _ = s.recorder.Append(ctx, audit.Entry{
		Actor:    employeeUserID,
		TenantID: tenantID,
		Action:   audit.ActionTransparencyDSRDetailViewed,
		Target:   fmt.Sprintf("dsr:%s", id),
		Details:  map[string]any{"employee_user_id": employeeUserID},
	})

	return req, nil
}

// List returns DSRs, optionally filtered by state.
func (s *Service) List(ctx context.Context, tenantID string, states []State) ([]*Request, error) {
	return s.store.List(ctx, tenantID, states)
}

// Stats returns DPO dashboard aggregates.
func (s *Service) Stats(ctx context.Context, tenantID string) (*DashboardStats, error) {
	return s.store.Stats(ctx, tenantID)
}
