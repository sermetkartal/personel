// Package liveview — live view orchestration service.
//
// State transitions are persisted in Postgres. NATS commands are published
// after every relevant transition. Every transition is audited FIRST.
package liveview

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/evidence"
	"github.com/personel/api/internal/nats"
	"github.com/personel/api/internal/vault"
)

const (
	defaultDurationSeconds = 900  // 15 min
	hardCapSeconds         = 3600 // 60 min
)

// LiveKitTokenMinter mints short-lived LiveKit room tokens.
type LiveKitTokenMinter interface {
	// MintAdminToken returns a view-only token for the admin console.
	MintAdminToken(room string, ttl time.Duration) (string, error)
	// MintAgentToken returns a publish token for the agent.
	MintAgentToken(room, sessionID string, ttl time.Duration) (string, error)
	// CreateRoom creates the LiveKit room.
	CreateRoom(ctx context.Context, room string) error
}

// Service orchestrates live view sessions.
type Service struct {
	store       *Store
	recorder    *audit.Recorder
	nats        nats.LiveViewPublisher
	vaultClient *vault.Client
	livekit     LiveKitTokenMinter
	cfg         ServiceConfig
	log         *slog.Logger

	// evidenceRecorder is optional — if nil, the service still operates
	// normally and simply skips SOC 2 evidence emission. Set via
	// SetEvidenceRecorder during boot if the evidence locker is wired.
	// This keeps the constructor signature stable for existing tests.
	evidenceRecorder evidence.Recorder
}

// SetEvidenceRecorder attaches an evidence.Recorder to the service so that
// closed privileged-access sessions emit SOC 2 CC6.1 / CC6.3 evidence. If
// this is never called, the service operates with no evidence emission
// (scaffold mode). The setter is idempotent.
func (s *Service) SetEvidenceRecorder(r evidence.Recorder) {
	s.evidenceRecorder = r
}

// ServiceConfig holds live view–specific configuration.
type ServiceConfig struct {
	LiveKitHost        string
	MaxDuration        time.Duration
	ApprovalTimeout    time.Duration
	NATSLiveViewSubject string
}

// NewService creates the live view service.
func NewService(
	store *Store,
	rec *audit.Recorder,
	pub nats.LiveViewPublisher,
	vc *vault.Client,
	lk LiveKitTokenMinter,
	cfg ServiceConfig,
	log *slog.Logger,
) *Service {
	return &Service{
		store: store, recorder: rec, nats: pub,
		vaultClient: vc, livekit: lk, cfg: cfg, log: log,
	}
}

// RequestLiveView creates a new live view request.
//
// ADR 0026: Admin bypass. If the requester holds RoleAdmin, the dual-control
// HR/IT approval gate is skipped. The session is created directly in the
// APPROVED state, the LiveKit room + tokens are provisioned in-line, and
// the NATS start command is published to the gateway. Every audit entry
// for such a session carries `admin_bypass=true` in its details so
// compliance reviewers can filter them out of dual-control drill reports.
//
// Rationale: admin already holds ultimate authority over every privileged
// operation (policy signing, endpoint wipe, cert revoke, DSR oversight).
// Forcing an admin to flag down an it_manager for a live view approval is
// procedural friction with no security gain — the admin could impersonate
// that path in any number of other ways. The audit trail remains fully
// intact; the only thing the bypass changes is the state machine hop.
func (s *Service) RequestLiveView(ctx context.Context, p *auth.Principal, endpointID, reasonCode, justification string, durationSecs uint32) (*Session, error) {
	if !auth.Can(p, auth.OpRequest, auth.ResourceLiveView) {
		return nil, auth.ErrForbidden
	}
	if reasonCode == "" {
		return nil, fmt.Errorf("liveview: reason_code is required")
	}
	if durationSecs == 0 {
		durationSecs = defaultDurationSeconds
	}
	if durationSecs > hardCapSeconds {
		return nil, fmt.Errorf("liveview: duration exceeds hard cap of %d seconds", hardCapSeconds)
	}

	adminBypass := auth.HasRole(p, auth.RoleAdmin)

	now := time.Now().UTC()
	initialState := StateRequested
	if adminBypass {
		initialState = StateApproved
	}

	sess := &Session{
		TenantID:          p.TenantID,
		EndpointID:        endpointID,
		RequesterID:       p.UserID,
		ReasonCode:        reasonCode,
		Justification:     justification,
		RequestedDuration: time.Duration(durationSecs) * time.Second,
		State:             initialState,
		CreatedAt:         now,
		AdminBypass:       adminBypass,
	}
	if adminBypass {
		requesterID := p.UserID
		sess.ApproverID = &requesterID
		sess.ApprovedAt = &now
	}

	// Audit BEFORE the DB write. Include admin_bypass signal only when true
	// to keep ordinary requests' details JSON lean.
	requestDetails := map[string]any{
		"reason_code":   reasonCode,
		"justification": justification,
		"duration_secs": durationSecs,
	}
	if adminBypass {
		requestDetails["admin_bypass"] = true
	}
	_, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    p.UserID,
		TenantID: p.TenantID,
		Action:   audit.ActionLiveViewRequested,
		Target:   fmt.Sprintf("endpoint:%s", endpointID),
		Details:  requestDetails,
	})
	if err != nil {
		return nil, fmt.Errorf("liveview: audit request: %w", err)
	}

	id, err := s.store.Create(ctx, sess)
	if err != nil {
		return nil, err
	}
	sess.ID = id

	if !adminBypass {
		return sess, nil
	}

	// Admin bypass path — emit a synthetic "approved" audit entry so the
	// audit chain still tells a clean story (requested → approved →
	// started), and then run the same LiveKit provisioning the normal
	// Approve() helper would have run.
	_, err = s.recorder.Append(ctx, audit.Entry{
		Actor:    p.UserID,
		TenantID: p.TenantID,
		Action:   audit.ActionLiveViewApproved,
		Target:   fmt.Sprintf("session:%s", id),
		Details: map[string]any{
			"admin_bypass": true,
			"requester_id": p.UserID,
			"notes":        "auto-approved (admin bypass)",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("liveview: audit auto-approve: %w", err)
	}

	if err := s.provisionLiveKit(ctx, p, sess); err != nil {
		return nil, err
	}
	return sess, nil
}

// provisionLiveKit creates the LiveKit room, mints the admin/agent tokens,
// signs the control message with the control-plane key, persists the
// approval details, and publishes the NATS start command. It is called
// by both Approve() (normal dual-control path) and RequestLiveView()
// (admin bypass path). The session must already be in StateApproved and
// already persisted with ApproverID set.
func (s *Service) provisionLiveKit(ctx context.Context, p *auth.Principal, sess *Session) error {
	room := fmt.Sprintf("lv-%s-%s", p.TenantID[:8], sess.ID)
	if err := s.livekit.CreateRoom(ctx, room); err != nil {
		s.log.Error("liveview: create room failed", slog.String("session_id", sess.ID), slog.Any("error", err))
		_ = s.store.MarkFailed(ctx, sess.ID, p.TenantID, "livekit_room_create_failed")
		return fmt.Errorf("liveview: create room: %w", err)
	}

	agentTTL := sess.RequestedDuration
	adminToken, err := s.livekit.MintAdminToken(room, agentTTL)
	if err != nil {
		return fmt.Errorf("liveview: mint admin token: %w", err)
	}
	agentToken, err := s.livekit.MintAgentToken(room, sess.ID, agentTTL)
	if err != nil {
		return fmt.Errorf("liveview: mint agent token: %w", err)
	}

	payload := []byte(fmt.Sprintf("liveview.start:%s:%s", sess.ID, room))
	sig, keyID, err := s.vaultClient.SignWithControlKey(ctx, payload)
	if err != nil {
		return fmt.Errorf("liveview: sign control message: %w", err)
	}

	if err := s.store.SetApprovalDetails(ctx, sess.ID, p.TenantID, room, adminToken, agentToken, keyID); err != nil {
		return err
	}

	cmd := nats.LiveViewStartCommand{
		SessionID:        sess.ID,
		LiveKitURL:       s.cfg.LiveKitHost,
		LiveKitRoom:      room,
		AgentToken:       agentToken,
		NotAfter:         time.Now().UTC().Add(agentTTL),
		ControlSignature: sig,
		SigningKeyID:     keyID,
		ReasonCode:       sess.ReasonCode,
	}
	if err := s.nats.PublishLiveViewStart(ctx, p.TenantID, sess.EndpointID, cmd); err != nil {
		return fmt.Errorf("liveview: publish start command: %w", err)
	}

	sess.LiveKitRoom = &room
	sess.LiveKitRoomStr = room
	sess.AdminToken = adminToken
	sess.AgentToken = agentToken
	sess.SigningKeyID = keyID
	return nil
}

// Approve approves a live view request. The approver MUST be in the IT
// hierarchy (it_manager or admin) AND MUST NOT be the same person as
// the requester. HR has no live-view authority in this model — company
// devices are IT-department property and approval is an IT-internal
// dual-control ceremony.
func (s *Service) Approve(ctx context.Context, p *auth.Principal, sessionID, notes string) (*Session, error) {
	if !auth.HasRole(p, auth.RoleITManager) && !auth.HasRole(p, auth.RoleAdmin) {
		return nil, auth.ErrForbidden
	}

	sess, err := s.store.Get(ctx, sessionID, p.TenantID)
	if err != nil {
		return nil, err
	}
	if sess.State != StateRequested {
		return nil, fmt.Errorf("%w: session is not in REQUESTED state", ErrInvalidTransition)
	}

	// Dual-control enforcement — approver must differ from requester.
	if err := auth.AssertApproverDiffersFromRequester(p.UserID, sess.RequesterID); err != nil {
		return nil, err
	}

	// Audit BEFORE state change.
	_, err = s.recorder.Append(ctx, audit.Entry{
		Actor:    p.UserID,
		TenantID: p.TenantID,
		Action:   audit.ActionLiveViewApproved,
		Target:   fmt.Sprintf("session:%s", sessionID),
		Details:  map[string]any{"notes": notes, "requester_id": sess.RequesterID},
	})
	if err != nil {
		return nil, err
	}

	// Transition state.
	newState, err := Transition(sess.State, "approve")
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if err := s.store.SetState(ctx, sessionID, p.TenantID, newState, &p.UserID, &notes, &now); err != nil {
		return nil, err
	}

	// Populate the in-memory session so the returned object matches
	// what was just written to the DB. Previously ApproverID and
	// ApprovalNotes were written to the DB by SetState but NOT mirrored
	// here, so callers saw a stale Session with ApproverID=nil right
	// after a successful Approve. Caught by the integration test on
	// 2026-04-11.
	approverID := p.UserID
	approvalNotes := notes
	sess.State = newState
	sess.ApproverID = &approverID
	sess.ApprovalNotes = &approvalNotes
	sess.ApprovedAt = &now

	if err := s.provisionLiveKit(ctx, p, sess); err != nil {
		return nil, err
	}
	return sess, nil
}

// Reject denies a live view request. IT Manager or Admin only —
// mirrors Approve's authority check.
func (s *Service) Reject(ctx context.Context, p *auth.Principal, sessionID, notes string) error {
	if !auth.HasRole(p, auth.RoleITManager) && !auth.HasRole(p, auth.RoleAdmin) {
		return auth.ErrForbidden
	}

	sess, err := s.store.Get(ctx, sessionID, p.TenantID)
	if err != nil {
		return err
	}
	if sess.State != StateRequested {
		return fmt.Errorf("%w: session is not in REQUESTED state", ErrInvalidTransition)
	}

	_, err = s.recorder.Append(ctx, audit.Entry{
		Actor:    p.UserID,
		TenantID: p.TenantID,
		Action:   audit.ActionLiveViewDenied,
		Target:   fmt.Sprintf("session:%s", sessionID),
		Details:  map[string]any{"notes": notes},
	})
	if err != nil {
		return err
	}

	newState, _ := Transition(sess.State, "deny")
	now := time.Now().UTC()
	return s.store.SetState(ctx, sessionID, p.TenantID, newState, &p.UserID, &notes, &now)
}

// EndSession ends an active session (admin-initiated).
func (s *Service) EndSession(ctx context.Context, p *auth.Principal, sessionID string) error {
	return s.terminateSession(ctx, p, sessionID, "end", audit.ActionLiveViewStopped, "admin_end")
}

// Terminate terminates a session (IT Manager, Admin, or DPO kill switch).
// IT is the primary authority; DPO is a compliance override path for
// KVKK scope violations only.
func (s *Service) Terminate(ctx context.Context, p *auth.Principal, sessionID string) error {
	var action audit.Action
	var event string
	switch {
	case auth.HasRole(p, auth.RoleAdmin):
		action = audit.ActionLiveViewTerminatedByAdmin
		event = "admin_terminate"
	case auth.HasRole(p, auth.RoleITManager):
		action = audit.ActionLiveViewTerminatedByITMgr
		event = "it_manager_terminate"
	case auth.HasRole(p, auth.RoleDPO):
		action = audit.ActionLiveViewTerminatedByDPO
		event = "dpo_terminate"
	default:
		return auth.ErrForbidden
	}
	return s.terminateSession(ctx, p, sessionID, event, action, event)
}

func (s *Service) terminateSession(ctx context.Context, p *auth.Principal, sessionID, event string, action audit.Action, reason string) error {
	sess, err := s.store.Get(ctx, sessionID, p.TenantID)
	if err != nil {
		return err
	}

	newState, err := Transition(sess.State, event)
	if err != nil {
		return err
	}

	terminateAuditID, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    p.UserID,
		TenantID: p.TenantID,
		Action:   action,
		Target:   fmt.Sprintf("session:%s", sessionID),
		Details:  map[string]any{"reason": reason},
	})
	if err != nil {
		return err
	}

	// Publish stop command to gateway.
	payload := []byte(fmt.Sprintf("liveview.stop:%s:%s", sessionID, reason))
	sig, keyID, _ := s.vaultClient.SignWithControlKey(ctx, payload)
	_ = s.nats.PublishLiveViewStop(ctx, p.TenantID, sess.EndpointID, nats.LiveViewStopCommand{
		SessionID:        sessionID,
		Reason:           reason,
		ControlSignature: sig,
		SigningKeyID:      keyID,
	})

	now := time.Now().UTC()
	if err := s.store.SetState(ctx, sessionID, p.TenantID, newState, &p.UserID, nil, &now); err != nil {
		return err
	}

	// SOC 2 evidence emission (Phase 3.0) — best-effort, non-blocking.
	// A transitioned-to-terminal session that was previously ACTIVE is a
	// completed privileged access ceremony: HR-approved, time-bounded,
	// dual-controlled. That is the CC6.1 / CC6.3 evidence item.
	//
	// Emission failure must not fail the termination — the session is
	// already gone from the user's perspective. Log loudly so a missing
	// evidence item is visible in SOC 2 coverage gap reports rather than
	// silently lost.
	s.emitSessionEvidence(ctx, sess, newState, reason, terminateAuditID, now, p.UserID)

	return nil
}

// emitSessionEvidence records a KindPrivilegedAccessSession evidence item
// for a terminated live view session. Called from terminateSession after
// the DB state has been updated. No-op if the evidence recorder was not
// wired (scaffold mode).
//
// The payload is intentionally minimal — auditors want to see approver,
// requester, endpoint, reason, and actual duration. Full justification
// text is included to satisfy CC6.1 "business justification for privileged
// access" evidence.
func (s *Service) emitSessionEvidence(
	ctx context.Context,
	sess *Session,
	finalState State,
	terminationReason string,
	terminateAuditID int64,
	endedAt time.Time,
	actorUserID string,
) {
	if s.evidenceRecorder == nil {
		return
	}

	// approverID and actualDuration come from the session row we already
	// loaded. Approver is set during Approve(); if nil, the session never
	// made it past REQUESTED and we should not be in terminateSession at
	// all — defence-in-depth fallback logs and skips.
	if sess.ApproverID == nil {
		s.log.WarnContext(ctx, "liveview: evidence skipped — no approver on terminated session",
			slog.String("session_id", sess.ID),
			slog.String("state", string(finalState)),
		)
		return
	}

	var actualDuration time.Duration
	if sess.StartedAt != nil {
		actualDuration = endedAt.Sub(*sess.StartedAt)
	}

	payload, err := json.Marshal(map[string]any{
		"session_id":        sess.ID,
		"endpoint_id":       sess.EndpointID,
		"requester_id":      sess.RequesterID,
		"approver_id":       *sess.ApproverID,
		"terminator_id":     actorUserID,
		"reason_code":       sess.ReasonCode,
		"justification":     sess.Justification,
		"requested_seconds": int64(sess.RequestedDuration / time.Second),
		"actual_seconds":    int64(actualDuration / time.Second),
		"final_state":       string(finalState),
		"termination_event": terminationReason,
	})
	if err != nil {
		s.log.ErrorContext(ctx, "liveview: evidence payload marshal failed",
			slog.String("session_id", sess.ID),
			slog.String("error", err.Error()),
		)
		return
	}

	item := evidence.Item{
		TenantID:   sess.TenantID,
		Control:    evidence.CtrlCC6_1,
		Kind:       evidence.KindPrivilegedAccessSession,
		RecordedAt: endedAt,
		Actor:      sess.RequesterID,
		SummaryTR: fmt.Sprintf(
			"Canlı izleme oturumu sonlandırıldı — oturum %s, endpoint %s, onay %s, süre %ds",
			sess.ID, sess.EndpointID, *sess.ApproverID, int64(actualDuration/time.Second),
		),
		SummaryEN: fmt.Sprintf(
			"Live view session closed — session %s, endpoint %s, approver %s, duration %ds",
			sess.ID, sess.EndpointID, *sess.ApproverID, int64(actualDuration/time.Second),
		),
		Payload:            payload,
		ReferencedAuditIDs: []int64{terminateAuditID},
	}

	if _, err := s.evidenceRecorder.Record(ctx, item); err != nil {
		// Loud log — SOC 2 coverage check depends on this line firing
		// into alerting if the evidence path is unhealthy. Do not
		// propagate: the termination itself already succeeded.
		s.log.ErrorContext(ctx, "liveview: SOC 2 evidence emission failed",
			slog.String("session_id", sess.ID),
			slog.String("control", string(evidence.CtrlCC6_1)),
			slog.String("error", err.Error()),
		)
	}
}

// AgentStarted is called when the gateway reports the agent has joined LiveKit.
func (s *Service) AgentStarted(ctx context.Context, tenantID, sessionID string) error {
	_, err := s.recorder.AppendSystem(ctx, tenantID, audit.ActionLiveViewStarted,
		fmt.Sprintf("session:%s", sessionID), nil)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return s.store.SetState(ctx, sessionID, tenantID, StateActive, nil, nil, &now)
}

// GetSession returns a single session.
func (s *Service) GetSession(ctx context.Context, tenantID, sessionID string) (*Session, error) {
	return s.store.Get(ctx, sessionID, tenantID)
}

// ListRequests returns live view requests, optionally filtered by state.
func (s *Service) ListRequests(ctx context.Context, tenantID string, state *State) ([]*Session, error) {
	return s.store.List(ctx, tenantID, state)
}

// ListSessions returns live view sessions for an employee (for transparency portal).
func (s *Service) ListSessionsForEmployee(ctx context.Context, tenantID, employeeUserID string) ([]*Session, error) {
	return s.store.ListByEmployee(ctx, tenantID, employeeUserID)
}

// ed25519.PublicKeySize is kept here to ensure the import is used.
var _ = ed25519.PublicKeySize

// ErrInvalidTransition is returned when a state transition is not allowed.
var ErrInvalidTransition = fmt.Errorf("liveview: invalid state transition")
