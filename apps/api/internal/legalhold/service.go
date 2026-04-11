// Package legalhold — DPO-only legal hold service.
package legalhold

import (
	"context"
	"fmt"
	"time"

	"log/slog"

	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/auth"
)

const maxDuration = 2 * 365 * 24 * time.Hour // 2 years

// Service orchestrates legal hold operations.
type Service struct {
	store    *Store
	recorder *audit.Recorder
	log      *slog.Logger
}

// NewService creates the legal hold service.
func NewService(store *Store, rec *audit.Recorder, log *slog.Logger) *Service {
	return &Service{store: store, recorder: rec, log: log}
}

// PlaceInput is the data required to place a legal hold.
type PlaceInput struct {
	TenantID      string
	DPOUserID     string
	ReasonCode    string
	TicketID      string
	Justification string
	EndpointID    *string
	UserSID       *string
	DateRangeFrom *time.Time
	DateRangeTo   *time.Time
	EventTypes    []string
	Duration      time.Duration // max 2 years
}

// Place creates a legal hold. Only DPO role is permitted (enforced in handler).
func (s *Service) Place(ctx context.Context, p *auth.Principal, in PlaceInput) (*Hold, error) {
	if !auth.HasRole(p, auth.RoleDPO) {
		return nil, auth.ErrForbidden
	}
	if in.ReasonCode == "" || in.TicketID == "" || in.Justification == "" {
		return nil, fmt.Errorf("legalhold: reason_code, ticket_id, and justification are required")
	}
	if in.Duration == 0 {
		in.Duration = maxDuration
	}
	if in.Duration > maxDuration {
		return nil, fmt.Errorf("legalhold: duration exceeds maximum of 2 years")
	}

	now := time.Now().UTC()
	hold := &Hold{
		TenantID:      in.TenantID,
		DPOUserID:     in.DPOUserID,
		ReasonCode:    in.ReasonCode,
		TicketID:      in.TicketID,
		Justification: in.Justification,
		EndpointID:    in.EndpointID,
		UserSID:       in.UserSID,
		DateRangeFrom: in.DateRangeFrom,
		DateRangeTo:   in.DateRangeTo,
		EventTypes:    in.EventTypes,
		PlacedAt:      now,
		ExpiresAt:     now.Add(in.Duration),
		IsActive:      true,
	}

	// Audit BEFORE the DB write.
	_, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    in.DPOUserID,
		TenantID: in.TenantID,
		Action:   audit.ActionLegalHoldPlaced,
		Target:   fmt.Sprintf("ticket:%s", in.TicketID),
		Details: map[string]any{
			"reason_code":   in.ReasonCode,
			"ticket_id":     in.TicketID,
			"justification": in.Justification,
			"endpoint_id":   in.EndpointID,
			"user_sid":      in.UserSID,
			"duration_days": int(in.Duration.Hours() / 24),
			"event_types":   in.EventTypes,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("legalhold: audit place: %w", err)
	}

	id, err := s.store.Place(ctx, hold)
	if err != nil {
		return nil, err
	}
	hold.ID = id
	return hold, nil
}

// Release removes a legal hold.
func (s *Service) Release(ctx context.Context, p *auth.Principal, holdID, reason string) error {
	if !auth.HasRole(p, auth.RoleDPO) {
		return auth.ErrForbidden
	}
	if reason == "" {
		return fmt.Errorf("legalhold: release reason is required")
	}

	_, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    p.UserID,
		TenantID: p.TenantID,
		Action:   audit.ActionLegalHoldReleased,
		Target:   fmt.Sprintf("hold:%s", holdID),
		Details:  map[string]any{"reason": reason},
	})
	if err != nil {
		return err
	}
	return s.store.Release(ctx, holdID, p.TenantID, reason)
}

// Get returns a single hold.
func (s *Service) Get(ctx context.Context, tenantID, id string) (*Hold, error) {
	return s.store.Get(ctx, id, tenantID)
}

// List returns legal holds.
func (s *Service) List(ctx context.Context, tenantID string, activeOnly bool) ([]*Hold, error) {
	return s.store.List(ctx, tenantID, activeOnly)
}
