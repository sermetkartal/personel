// Package silence — Flow 7 agent silence dashboard.
// Reads heartbeat gap events published by the gateway and exposes
// per-endpoint silence timelines to admin dashboards.
package silence

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/auth"
)

// SilenceGap represents a period when no heartbeat was received.
type SilenceGap struct {
	EndpointID string        `json:"endpoint_id"`
	StartedAt  time.Time     `json:"started_at"`
	EndedAt    *time.Time    `json:"ended_at"`
	Duration   time.Duration `json:"duration_seconds"`
	Reason     string        `json:"reason"` // "offline" | "tamper_suspected" | "unknown"
	Acknowledged bool        `json:"acknowledged"`
}

// Service reads silence gaps.
type Service struct {
	ch       clickhouse.Conn
	pg       *pgxpool.Pool
	recorder *audit.Recorder
	log      *slog.Logger
}

// NewService creates the silence service.
func NewService(ch clickhouse.Conn, pg *pgxpool.Pool, rec *audit.Recorder, log *slog.Logger) *Service {
	return &Service{ch: ch, pg: pg, recorder: rec, log: log}
}

// List returns silence gaps for all endpoints of a tenant.
func (s *Service) List(ctx context.Context, tenantID string, from, to time.Time) ([]*SilenceGap, error) {
	rows, err := s.ch.Query(ctx,
		`SELECT
		   toString(endpoint_id) AS endpoint_id,
		   min(occurred_at)      AS started_at,
		   max(occurred_at)      AS ended_at,
		   dateDiff('second', min(occurred_at), max(occurred_at)) AS duration_s,
		   any(payload['reason']) AS reason
		 FROM events
		 WHERE tenant_id = ?
		   AND event_type = 'agent.heartbeat_gap'
		   AND occurred_at BETWEEN ? AND ?
		 GROUP BY endpoint_id
		 ORDER BY started_at DESC`,
		tenantID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("silence: list: %w", err)
	}
	defer rows.Close()

	var out []*SilenceGap
	for rows.Next() {
		var g SilenceGap
		var durSecs int64
		if err := rows.Scan(&g.EndpointID, &g.StartedAt, &g.EndedAt, &durSecs, &g.Reason); err != nil {
			return nil, fmt.Errorf("silence: scan: %w", err)
		}
		g.Duration = time.Duration(durSecs) * time.Second
		out = append(out, &g)
	}
	return out, rows.Err()
}

// Timeline returns the silence gaps for a specific endpoint.
func (s *Service) Timeline(ctx context.Context, tenantID, endpointID string, from, to time.Time) ([]*SilenceGap, error) {
	rows, err := s.ch.Query(ctx,
		`SELECT
		   toString(endpoint_id),
		   occurred_at AS started_at,
		   occurred_at AS ended_at,
		   toInt64(payload['duration_seconds']) AS duration_s,
		   payload['reason'] AS reason
		 FROM events
		 WHERE tenant_id = ?
		   AND endpoint_id = ?
		   AND event_type = 'agent.heartbeat_gap'
		   AND occurred_at BETWEEN ? AND ?
		 ORDER BY occurred_at DESC`,
		tenantID, endpointID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("silence: timeline: %w", err)
	}
	defer rows.Close()

	var out []*SilenceGap
	for rows.Next() {
		var g SilenceGap
		var durSecs int64
		if err := rows.Scan(&g.EndpointID, &g.StartedAt, &g.EndedAt, &durSecs, &g.Reason); err != nil {
			return nil, fmt.Errorf("silence: timeline scan: %w", err)
		}
		g.Duration = time.Duration(durSecs) * time.Second
		out = append(out, &g)
	}
	return out, rows.Err()
}

// Acknowledge marks a silence gap as acknowledged by an admin.
func (s *Service) Acknowledge(ctx context.Context, p *auth.Principal, endpointID string, at time.Time) error {
	_, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    p.UserID,
		TenantID: p.TenantID,
		Action:   audit.ActionAgentSilenceAcknowledged,
		Target:   fmt.Sprintf("endpoint:%s", endpointID),
		Details: map[string]any{
			"silence_at": at.Format(time.RFC3339),
		},
	})
	if err != nil {
		return err
	}

	// Mark in Postgres acknowledgement table.
	_, err = s.pg.Exec(ctx,
		`INSERT INTO silence_acknowledgements (endpoint_id, tenant_id, silence_at, acknowledged_by, acknowledged_at)
		 VALUES ($1::uuid, $2::uuid, $3, $4::uuid, now())
		 ON CONFLICT (endpoint_id, silence_at) DO UPDATE SET acknowledged_by = EXCLUDED.acknowledged_by, acknowledged_at = EXCLUDED.acknowledged_at`,
		endpointID, p.TenantID, at, p.UserID,
	)
	return err
}
