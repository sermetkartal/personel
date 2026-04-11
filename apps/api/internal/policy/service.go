// Package policy — policy service layer.
package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/evidence"
)

// Service orchestrates policy CRUD and publishing.
type Service struct {
	store    *Store
	pub      *Publisher
	recorder *audit.Recorder
	log      *slog.Logger

	// evidenceRecorder is optional — when wired, every successful
	// policy Push emits a KindChangeAuthorization evidence item under
	// controls CC7.1 (configuration management) and CC8.1 (change
	// management). See SetEvidenceRecorder.
	evidenceRecorder evidence.Recorder
}

// NewService creates the policy service.
func NewService(store *Store, pub *Publisher, rec *audit.Recorder, log *slog.Logger) *Service {
	return &Service{store: store, pub: pub, recorder: rec, log: log}
}

// SetEvidenceRecorder attaches an evidence.Recorder so successful Pushes
// emit SOC 2 change-management evidence. Safe to call once at wire-time;
// if never called, the service operates without evidence emission.
func (s *Service) SetEvidenceRecorder(r evidence.Recorder) {
	s.evidenceRecorder = r
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

	pushAuditID, err := s.recorder.Append(ctx, audit.Entry{
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
		if err := s.pub.PublishToAll(ctx, tenantID, id, pol.Rules, pol.Version); err != nil {
			return err
		}
	} else {
		if err := s.pub.PublishToEndpoint(ctx, tenantID, endpointID, id, pol.Rules, pol.Version); err != nil {
			return err
		}
	}

	// SOC 2 CC7.1 / CC8.1 evidence — emitted only after the publisher
	// has accepted the bundle. Emission failures are logged but never
	// propagate; the user's Push already succeeded from their POV.
	s.emitPushEvidence(ctx, p, pol, endpointID, pushAuditID)
	return nil
}

// emitPushEvidence records a KindChangeAuthorization evidence item for a
// successfully pushed policy bundle. The payload captures the actor, the
// target endpoint (or "*" for tenant-wide), the policy version, and whether
// the bundle contained the ADR 0013 DLP invariants. Auditors reviewing
// change management can walk from this item back to the exact rules JSON
// via the WORM-anchored canonical payload.
func (s *Service) emitPushEvidence(
	ctx context.Context,
	p *auth.Principal,
	pol *Policy,
	endpointID string,
	pushAuditID int64,
) {
	if s.evidenceRecorder == nil {
		return
	}

	target := endpointID
	if target == "" {
		target = "*"
	}

	// We record both the full rules payload AND a summary. The rules
	// payload goes inside the signed canonical bytes, so tampering with
	// the deployed config after the fact is detectable by re-signing.
	payload, err := json.Marshal(map[string]any{
		"policy_id":       pol.ID,
		"policy_name":     pol.Name,
		"policy_version":  pol.Version,
		"target_endpoint": target,
		"pushed_by":       p.UserID,
		"rules":           json.RawMessage(pol.Rules),
	})
	if err != nil {
		s.log.ErrorContext(ctx, "policy: evidence payload marshal failed",
			slog.String("policy_id", pol.ID),
			slog.String("error", err.Error()),
		)
		return
	}

	// Control mapping: a policy Push is both configuration management
	// (CC7.1) and change management (CC8.1). We emit ONE item per Push
	// against CC8.1 since CC8.1 is the authorization control; CC7.1 is
	// covered indirectly via the same item's referenced_audit_ids. An
	// alternative would be two items per Push (one per control) — we
	// chose a single item to keep evidence-pack size manageable.
	item := evidence.Item{
		TenantID:   pol.TenantID,
		Control:    evidence.CtrlCC8_1,
		Kind:       evidence.KindChangeAuthorization,
		RecordedAt: time.Now().UTC(),
		Actor:      p.UserID,
		SummaryTR: fmt.Sprintf(
			"Politika yayını — %s (v%d) hedef %s tarafından %s",
			pol.Name, pol.Version, target, p.UserID,
		),
		SummaryEN: fmt.Sprintf(
			"Policy push — %s (v%d) target=%s pushed_by=%s",
			pol.Name, pol.Version, target, p.UserID,
		),
		Payload:            payload,
		ReferencedAuditIDs: []int64{pushAuditID},
	}

	if _, err := s.evidenceRecorder.Record(ctx, item); err != nil {
		s.log.ErrorContext(ctx, "policy: SOC 2 evidence emission failed",
			slog.String("policy_id", pol.ID),
			slog.String("control", string(evidence.CtrlCC8_1)),
			slog.String("error", err.Error()),
		)
	}
}

// Get returns a policy.
func (s *Service) Get(ctx context.Context, tenantID, id string) (*Policy, error) {
	return s.store.Get(ctx, id, tenantID)
}

// List returns policies for a tenant.
func (s *Service) List(ctx context.Context, tenantID string) ([]*Policy, error) {
	return s.store.List(ctx, tenantID)
}
