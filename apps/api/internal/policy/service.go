// Package policy — policy service layer.
package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/auth"
)

// Service orchestrates policy CRUD and publishing.
type Service struct {
	store     *Store
	pub       *Publisher
	recorder  *audit.Recorder
	log       *slog.Logger
}

// NewService creates the policy service.
func NewService(store *Store, pub *Publisher, rec *audit.Recorder, log *slog.Logger) *Service {
	return &Service{store: store, pub: pub, recorder: rec, log: log}
}

// CreateInput is the data required to create a policy.
type CreateInput struct {
	TenantID    string
	Name        string
	Description string
	Rules       json.RawMessage
	CreatedBy   string
	IsDefault   bool
}

// Create creates a new policy.
func (s *Service) Create(ctx context.Context, p *auth.Principal, in CreateInput) (*Policy, error) {
	if !auth.Can(p, auth.OpWrite, auth.ResourcePolicy) {
		return nil, auth.ErrForbidden
	}

	// Validate rules.
	if fieldErrs, err := Validate(in.Rules); err != nil {
		return nil, err
	} else if len(fieldErrs) > 0 {
		return nil, fmt.Errorf("policy: validation failed: %v", fieldErrs)
	}

	pol := &Policy{
		TenantID:    in.TenantID,
		Name:        in.Name,
		Description: in.Description,
		Rules:       in.Rules,
		CreatedBy:   in.CreatedBy,
		IsDefault:   in.IsDefault,
	}

	_, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    in.CreatedBy,
		TenantID: in.TenantID,
		Action:   audit.ActionPolicyCreated,
		Target:   fmt.Sprintf("policy:%s", in.Name),
		Details:  map[string]any{"name": in.Name},
	})
	if err != nil {
		return nil, err
	}

	id, err := s.store.Create(ctx, pol)
	if err != nil {
		return nil, err
	}
	pol.ID = id
	return pol, nil
}

// Update updates a policy's rules.
func (s *Service) Update(ctx context.Context, p *auth.Principal, id, tenantID string, rules json.RawMessage, name, description string) error {
	if !auth.Can(p, auth.OpWrite, auth.ResourcePolicy) {
		return auth.ErrForbidden
	}
	if fieldErrs, err := Validate(rules); err != nil {
		return err
	} else if len(fieldErrs) > 0 {
		return fmt.Errorf("policy: validation: %v", fieldErrs)
	}

	_, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    p.UserID,
		TenantID: tenantID,
		Action:   audit.ActionPolicyUpdated,
		Target:   fmt.Sprintf("policy:%s", id),
		Details:  map[string]any{"name": name},
	})
	if err != nil {
		return err
	}
	return s.store.Update(ctx, id, tenantID, rules, name, description)
}

// Delete removes a policy.
func (s *Service) Delete(ctx context.Context, p *auth.Principal, id, tenantID string) error {
	if !auth.Can(p, auth.OpDelete, auth.ResourcePolicy) {
		return auth.ErrForbidden
	}
	_, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    p.UserID,
		TenantID: tenantID,
		Action:   audit.ActionPolicyDeleted,
		Target:   fmt.Sprintf("policy:%s", id),
		Details:  nil,
	})
	if err != nil {
		return err
	}
	return s.store.Delete(ctx, id, tenantID)
}

// Push validates bundle invariants, signs, and publishes a policy to one or
// all endpoints of the tenant. Rejects with ErrInvalidInvariantDLPKeystroke
// (HTTP 422) when the bundle violates the ADR 0013 A5 structural invariant.
func (s *Service) Push(ctx context.Context, p *auth.Principal, id, tenantID, endpointID string) error {
	pol, err := s.store.Get(ctx, id, tenantID)
	if err != nil {
		return err
	}

	// --- ADR 0013 A5: validate bundle invariants before Vault sign call ---
	var inv BundleInvariants
	if err := json.Unmarshal(pol.Rules, &inv); err != nil {
		return fmt.Errorf("policy: push: unmarshal invariants: %w", err)
	}
	if fieldErrs, err := ValidateBundle(&inv); err != nil {
		return err // typed error (ErrInvalidInvariantDLPKeystroke or similar)
	} else if len(fieldErrs) > 0 {
		return fmt.Errorf("policy: push: invariant violations: %v", fieldErrs)
	}

	_, err = s.recorder.Append(ctx, audit.Entry{
		Actor:    p.UserID,
		TenantID: tenantID,
		Action:   audit.ActionPolicyPushed,
		Target:   fmt.Sprintf("policy:%s", id),
		Details:  map[string]any{"endpoint_id": endpointID, "version": pol.Version},
	})
	if err != nil {
		return err
	}

	if endpointID == "" || endpointID == "*" {
		return s.pub.PublishToAll(ctx, tenantID, id, pol.Rules, pol.Version)
	}
	return s.pub.PublishToEndpoint(ctx, tenantID, endpointID, id, pol.Rules, pol.Version)
}

// Get returns a policy.
func (s *Service) Get(ctx context.Context, tenantID, id string) (*Policy, error) {
	return s.store.Get(ctx, id, tenantID)
}

// List returns policies for a tenant.
func (s *Service) List(ctx context.Context, tenantID string) ([]*Policy, error) {
	return s.store.List(ctx, tenantID)
}
