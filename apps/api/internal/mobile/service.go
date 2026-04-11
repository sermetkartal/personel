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
// It performs the minimum number of database queries: one per underlying
// service, in parallel where independent.
func (s *Service) GetSummary(ctx context.Context, tenantID, userID string) (*SummaryResponse, error) {
	// Phase 2.9 scaffold: individual service method calls are Phase 2.10
	// work. For now, we surface zero counts and an empty audit list so
	// the mobile app can render its Home screen against a real 200
	// response instead of a 404.
	dlpStatus, err := s.dlpState.GetStatus(ctx)
	if err != nil {
		// Degrade: state unknown doesn't block the summary.
		s.log.WarnContext(ctx, "mobile summary: dlp-state failed, falling back to 'unknown'",
			slog.String("error", err.Error()))
		dlpStatus = &dlpstate.DLPStatus{State: dlpstate.DLPStateValue("unknown")}
	}

	return &SummaryResponse{
		PendingLiveViewCount: 0, // Phase 2.10: s.liveViewSvc.CountPending(ctx, tenantID)
		PendingDSRCount:      0, // Phase 2.10: s.dsrSvc.CountByStates(ctx, tenantID, openStates)
		SilenceAlertsLast24h: 0, // Phase 2.10: s.silenceSvc.CountLast24h(ctx, tenantID)
		RecentAuditEntries:   []AuditEntryLite{},
		DLPState:             string(dlpStatus.State),
	}, nil
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
