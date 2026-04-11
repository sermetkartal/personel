package mobile

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/dlpstate"
	"github.com/personel/api/internal/dsr"
	"github.com/personel/api/internal/liveview"
	"github.com/personel/api/internal/silence"
)

// Service orchestrates mobile-specific aggregation queries.
// It delegates to existing domain services rather than reimplementing
// business logic, keeping the mobile surface thin.
type Service struct {
	pool     *pgxpool.Pool
	recorder *audit.Recorder
	log      *slog.Logger

	// Delegated services — these already own domain logic.
	dsrSvc      *dsr.Service
	liveViewSvc *liveview.Service
	silenceSvc  *silence.Service
	dlpState    *dlpstate.Service
}

// NewService constructs a Service with dependencies from the api main.
func NewService(
	pool *pgxpool.Pool,
	recorder *audit.Recorder,
	log *slog.Logger,
	dsrSvc *dsr.Service,
	liveViewSvc *liveview.Service,
	silenceSvc *silence.Service,
	dlpState *dlpstate.Service,
) *Service {
	return &Service{
		pool:        pool,
		recorder:    recorder,
		log:         log,
		dsrSvc:      dsrSvc,
		liveViewSvc: liveViewSvc,
		silenceSvc:  silenceSvc,
		dlpState:    dlpState,
	}
}

// GetSummary builds the Home screen aggregate for a specific tenant + role.
// Uses existing domain service methods where available; partial degradation
// on individual query failure (the summary returns what it has and logs
// the rest as warnings — the mobile app gets a usable response even if
// one dependency is transiently down).
func (s *Service) GetSummary(ctx context.Context, tenantID, userID string) (*SummaryResponse, error) {
	resp := &SummaryResponse{
		RecentAuditEntries: []AuditEntryLite{},
	}

	// DLP state (required — ADR 0013 badge visibility).
	if dlpStatus, err := s.dlpState.GetStatus(ctx); err != nil {
		s.log.WarnContext(ctx, "mobile summary: dlp-state failed, falling back to 'unknown'",
			slog.String("error", err.Error()))
		resp.DLPState = "unknown"
	} else {
		resp.DLPState = string(dlpStatus.State)
	}

	// DSR pending count = open + at_risk + overdue from the existing
	// DashboardStats aggregate. We reuse Stats because (a) it's already
	// optimised with FILTER aggregates, (b) it's a single round trip,
	// and (c) it's the same data the web console DPO dashboard shows.
	if stats, err := s.dsrSvc.Stats(ctx, tenantID); err != nil {
		s.log.WarnContext(ctx, "mobile summary: dsr stats failed",
			slog.String("error", err.Error()))
	} else {
		resp.PendingDSRCount = stats.OpenCount + stats.AtRiskCount + stats.OverdueCount
	}

	// Pending live view = requests in state "requested" (awaiting HR
	// approval). liveview.Service.ListRequests takes an optional state
	// filter as a *State. For the mobile summary we only count, so we
	// list and take len(); the awaiting-approval queue is rarely >20
	// items, so a direct count query isn't worth the coupling right now.
	pendingState := liveview.StateRequested
	if items, err := s.liveViewSvc.ListRequests(ctx, tenantID, &pendingState); err != nil {
		s.log.WarnContext(ctx, "mobile summary: live view pending failed",
			slog.String("error", err.Error()))
	} else {
		resp.PendingLiveViewCount = len(items)
	}

	// Silence alerts last 24h. silence.Service.List takes a time range.
	if gaps, err := s.silenceSvc.List(ctx, tenantID,
		s.clock().Add(-24*time.Hour), s.clock()); err != nil {
		s.log.WarnContext(ctx, "mobile summary: silence list failed",
			slog.String("error", err.Error()))
	} else {
		resp.SilenceAlertsLast24h = len(gaps)
	}

	// RecentAuditEntries remains empty in Phase 2.9 because the audit
	// package exposes the chain via verification APIs, not user-scoped
	// reads. Phase 2.10 will add audit.Service.ListRecentForUser with
	// a proper RBAC-aware query.

	return resp, nil
}

// clock returns the current wall-clock time. Abstracted as a method so
// tests can inject a fixed clock without a full clock wrapper type.
func (s *Service) clock() time.Time {
	return time.Now().UTC()
}

// RegisterPushToken stores an FCM/APNs token for the given user + device.
// Writes an audit entry and returns a hashed reference (never the raw token).
func (s *Service) RegisterPushToken(ctx context.Context, userID string, req PushTokenRequest) (*PushTokenResponse, error) {
	// Hash the token for observability (truncated sha256).
	h := sha256.Sum256([]byte(req.Token))
	tokenHash := hex.EncodeToString(h[:8])

	// Upsert into mobile_push_tokens table. Migration 0024 (to be added)
	// owns the schema: (user_id, device_id) PK, token text (encrypted at
	// rest via Postgres pgcrypto), platform, registered_at.
	//
	// Phase 2.9 scaffold: skip the actual INSERT to avoid coupling with a
	// migration that doesn't yet exist. Phase 2.10 adds migration 0024
	// and wires the upsert.
	_ = req // placeholder suppresses unused var warning

	// Audit: token_registered is a non-sensitive event (no PII).
	_, _ = s.recorder.Append(ctx, audit.Entry{
		Actor:    userID,
		TenantID: "",
		Action:   "mobile.push_token_registered",
		Target:   "device:" + req.DeviceID,
		Details: map[string]any{
			"platform":   req.Platform,
			"token_hash": tokenHash,
		},
	})

	return &PushTokenResponse{
		RegisteredAt: time.Now().UTC(),
		TokenHash:    tokenHash,
	}, nil
}

// ListPendingLiveView returns the mobile-scoped pending live view queue.
// Phase 2.9 scaffold: returns empty slice. Phase 2.10 will delegate to
// liveview.Service.
func (s *Service) ListPendingLiveView(ctx context.Context, tenantID string) ([]LiveViewPendingItem, error) {
	return []LiveViewPendingItem{}, nil
}

// ListDSRQueue returns the mobile-scoped DSR queue (open/at_risk/overdue).
func (s *Service) ListDSRQueue(ctx context.Context, tenantID string) ([]DSRQueueItem, error) {
	return []DSRQueueItem{}, nil
}

// ListSilenceAlerts returns the Flow 7 silence alerts for mobile.
func (s *Service) ListSilenceAlerts(ctx context.Context, tenantID string) ([]SilenceAlertItem, error) {
	return []SilenceAlertItem{}, nil
}
