// Package transparency — employee transparency portal backend.
//
// All endpoints are scoped to the caller's own data only.
// The RBAC ScopeToOwnData check is applied before any query.
package transparency

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/liveview"
)

// MyDataSummary is the employee-visible data summary.
type MyDataSummary struct {
	UserID           string    `json:"user_id"`
	DataCategories   []string  `json:"data_categories"`
	RetentionPeriods map[string]string `json:"retention_periods"`
	LastUpdated      time.Time `json:"last_updated"`
}

// Service provides employee transparency data.
type Service struct {
	pg          *pgxpool.Pool
	liveViewSvc *liveview.Service
	log         *slog.Logger
}

// NewService creates the transparency service.
func NewService(pg *pgxpool.Pool, lv *liveview.Service, log *slog.Logger) *Service {
	return &Service{pg: pg, liveViewSvc: lv, log: log}
}

// MyData returns the data summary for the calling employee.
func (s *Service) MyData(ctx context.Context, p *auth.Principal, targetUserID string) (*MyDataSummary, error) {
	if err := auth.ScopeToOwnData(p, targetUserID); err != nil {
		return nil, err
	}

	return &MyDataSummary{
		UserID: targetUserID,
		DataCategories: []string{
			"process_events", "window_title", "screenshots", "idle_active",
			"file_events", "usb_events", "network_flow_summary",
			"keystroke_statistics",
		},
		RetentionPeriods: map[string]string{
			"process_events":        "90 gün",
			"window_title":          "90 gün",
			"screenshots":           "30 gün",
			"idle_active":           "90 gün",
			"file_events":           "180 gün",
			"usb_events":            "365 gün",
			"network_flow_summary":  "60 gün",
			"keystroke_statistics":  "90 gün",
		},
		LastUpdated: time.Now().UTC(),
	}, nil
}

// MyLiveViewHistory returns live view sessions that targeted the calling employee.
// Default: visibility ON per live-view-protocol.md §Employee Notification Semantics.
func (s *Service) MyLiveViewHistory(ctx context.Context, p *auth.Principal, targetUserID string) ([]*liveview.Session, error) {
	if err := auth.ScopeToOwnData(p, targetUserID); err != nil {
		return nil, auth.ErrForbidden
	}

	// Check if history visibility has been restricted by DPO.
	restricted, err := s.isHistoryVisibilityRestricted(ctx, p.TenantID)
	if err != nil {
		return nil, err
	}
	if restricted {
		// Return empty list with a visibility-restricted notice.
		return []*liveview.Session{}, nil
	}

	return s.liveViewSvc.ListSessionsForEmployee(ctx, p.TenantID, targetUserID)
}

// isHistoryVisibilityRestricted checks if the DPO has restricted visibility.
func (s *Service) isHistoryVisibilityRestricted(ctx context.Context, tenantID string) (bool, error) {
	var restricted bool
	err := s.pg.QueryRow(ctx,
		`SELECT COALESCE(
		   (SELECT (settings->>'live_view_history_restricted')::boolean
		    FROM tenant_settings WHERE tenant_id = $1::uuid),
		   false
		 )`,
		tenantID,
	).Scan(&restricted)
	if err != nil {
		s.log.Error("transparency: check history visibility", slog.Any("error", err))
		return false, fmt.Errorf("transparency: check visibility: %w", err)
	}
	return restricted, nil
}
