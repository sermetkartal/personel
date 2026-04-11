// Package dsr — DSR service layer.
package dsr

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"log/slog"

	"github.com/personel/api/internal/audit"
)

// Service orchestrates DSR business logic.
type Service struct {
	store    *Store
	recorder *audit.Recorder
	notifier Notifier
	log      *slog.Logger
}

// NewService creates the DSR service.
func NewService(store *Store, rec *audit.Recorder, notifier Notifier, log *slog.Logger) *Service {
	return &Service{store: store, recorder: rec, notifier: notifier, log: log}
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
	_, _, err := s.auditAndRespond(ctx, tenantID, id, actorID, artifactRef)
	return err
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

// List returns DSRs, optionally filtered by state.
func (s *Service) List(ctx context.Context, tenantID string, states []State) ([]*Request, error) {
	return s.store.List(ctx, tenantID, states)
}

// Stats returns DPO dashboard aggregates.
func (s *Service) Stats(ctx context.Context, tenantID string) (*DashboardStats, error) {
	return s.store.Stats(ctx, tenantID)
}
