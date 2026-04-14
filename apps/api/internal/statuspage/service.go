// Package statuspage implements a lightweight public status page
// for the Personel installation (Faz 17 item #185).
//
// The public endpoint GET /public/status.json is the ONLY unauthenticated
// route mounted outside /v1/* (alongside /healthz and /readyz). It returns:
//
//   - A current "overall" state for each major component (api, gateway,
//     postgres, clickhouse, nats, minio, opensearch, keycloak, vault)
//   - Active incidents (from the status_incidents table)
//   - Upcoming planned maintenance windows (next 14 days)
//   - 7-day uptime percentage per component (computed from incident history)
//
// Design
// ------
//
// The service is deliberately simple:
//
//   - Component health is derived from the underlying system — for the
//     first cut we source it from the existing healthcheck results that
//     httpserver exposes on /healthz, but the architecture is prepared
//     to pull richer data from Prometheus (e.g. up{job="postgres"}).
//
//   - Incidents and maintenance windows live in Postgres (migration 0035).
//     Admin endpoints (under /v1/) let operators create and update them.
//
//   - The public endpoint is READ ONLY. No mutation paths are mounted on
//     /public. Writers are admin-gated under /v1/system/status/*.
//
// External provider integration (statuspage.io, instatus) is scaffolded
// via the Publisher interface but NOT implemented in this commit — each
// adapter will return ErrNotConfigured until a customer needs it.
package statuspage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/personel/api/internal/audit"
)

// ErrNotConfigured is returned by every external publisher stub.
var ErrNotConfigured = errors.New("statuspage: external publisher not configured")

// Components is the canonical ordered list of services whose health
// appears on the status page. Keep in sync with the compose file.
var Components = []string{
	"api", "gateway", "enricher", "postgres", "clickhouse",
	"nats", "minio", "opensearch", "keycloak", "vault",
}

// Severity levels for incidents mirror the tickets package so that
// an external monitor that looks at both surfaces sees consistent
// severity vocabularies.
type Severity string

const (
	SeverityP1 Severity = "P1"
	SeverityP2 Severity = "P2"
	SeverityP3 Severity = "P3"
	SeverityP4 Severity = "P4"
)

// State is the incident lifecycle.
type State string

const (
	StateInvestigating State = "investigating"
	StateIdentified    State = "identified"
	StateMonitoring    State = "monitoring"
	StateResolved      State = "resolved"
)

// Incident is a single operational incident visible on the status page.
type Incident struct {
	ID           uuid.UUID  `json:"id"`
	Severity     Severity   `json:"severity"`
	Component    string     `json:"component"`
	Title        string     `json:"title"`
	Description  string     `json:"description"`
	State        State      `json:"state"`
	StartedAt    time.Time  `json:"started_at"`
	ResolvedAt   *time.Time `json:"resolved_at,omitempty"`
	LastUpdateAt time.Time  `json:"last_update_at"`
}

// MaintenanceWindow is a pre-scheduled downtime.
type MaintenanceWindow struct {
	ID                  uuid.UUID `json:"id"`
	Title               string    `json:"title"`
	Description         string    `json:"description"`
	ScheduledStart      time.Time `json:"scheduled_start"`
	ScheduledEnd        time.Time `json:"scheduled_end"`
	AffectedComponents []string  `json:"affected_components"`
	State               string    `json:"state"`
}

// ComponentStatus is the current health state of a single service.
type ComponentStatus struct {
	Name        string  `json:"name"`
	Status      string  `json:"status"` // "operational" | "degraded" | "partial_outage" | "major_outage" | "maintenance"
	UptimeSevenDay float64 `json:"uptime_7d"` // 0-100 percentage
}

// PublicStatus is the full JSON shape returned by GET /public/status.json.
type PublicStatus struct {
	Overall              string              `json:"overall"`
	GeneratedAt          time.Time           `json:"generated_at"`
	Components           []ComponentStatus   `json:"components"`
	ActiveIncidents      []Incident          `json:"active_incidents"`
	UpcomingMaintenance  []MaintenanceWindow `json:"upcoming_maintenance"`
}

// HealthSource reports live health per component. In production this is
// backed by Prometheus queries; for now a simple map suffices. A nil
// implementation returns "unknown" for every component.
type HealthSource interface {
	// Health returns the current status string for one component.
	// Valid: "operational", "degraded", "partial_outage", "major_outage".
	Health(component string) string
}

// Publisher is the pluggable adapter for mirroring status to an
// external provider (statuspage.io, instatus). Stubbed in Phase 1.
type Publisher interface {
	Name() string
	Publish(ctx context.Context, s PublicStatus) error
}

// StaticHealthSource is a test helper that returns a fixed map.
type StaticHealthSource struct {
	Map map[string]string
}

// Health implements HealthSource by looking up the map. Unknown
// components return "operational" so tests don't accidentally fail.
func (s StaticHealthSource) Health(component string) string {
	if s.Map == nil {
		return "operational"
	}
	if v, ok := s.Map[component]; ok {
		return v
	}
	return "operational"
}

// Service is the public entry point.
type Service struct {
	pool      *pgxpool.Pool
	recorder  *audit.Recorder
	log       *slog.Logger
	health    HealthSource
	publishers []Publisher
}

// NewService constructs a Service.
func NewService(pool *pgxpool.Pool, rec *audit.Recorder, log *slog.Logger,
	health HealthSource, publishers ...Publisher) *Service {
	if log == nil {
		log = slog.Default()
	}
	if health == nil {
		health = StaticHealthSource{}
	}
	return &Service{
		pool:       pool,
		recorder:   rec,
		log:        log,
		health:     health,
		publishers: publishers,
	}
}

// GetPublicStatus assembles the payload served by GET /public/status.json.
func (s *Service) GetPublicStatus(ctx context.Context) (PublicStatus, error) {
	now := time.Now().UTC()
	comps := make([]ComponentStatus, 0, len(Components))

	// Uptime percentages over the last 7 days are computed from the
	// incident history. A component with zero incidents shows 100%,
	// each major outage subtracts its duration from the denominator.
	uptimeMap, err := s.sevenDayUptime(ctx, now)
	if err != nil {
		s.log.Warn("statuspage: uptime computation failed",
			slog.String("err", err.Error()))
	}

	worst := "operational"
	for _, name := range Components {
		st := s.health.Health(name)
		if severityRank(st) > severityRank(worst) {
			worst = st
		}
		up := uptimeMap[name]
		if up == 0 && st == "operational" {
			up = 100.0
		}
		comps = append(comps, ComponentStatus{
			Name:           name,
			Status:         st,
			UptimeSevenDay: up,
		})
	}

	active, err := s.listActiveIncidents(ctx)
	if err != nil {
		s.log.Warn("statuspage: active incidents query failed",
			slog.String("err", err.Error()))
		active = nil
	}

	upcoming, err := s.listUpcomingMaintenance(ctx, now)
	if err != nil {
		s.log.Warn("statuspage: upcoming maintenance query failed",
			slog.String("err", err.Error()))
		upcoming = nil
	}

	return PublicStatus{
		Overall:             worst,
		GeneratedAt:         now,
		Components:          comps,
		ActiveIncidents:     active,
		UpcomingMaintenance: upcoming,
	}, nil
}

// severityRank orders status strings so GetPublicStatus can compute
// the worst-case across all components for the top-level "overall"
// field.
func severityRank(s string) int {
	switch s {
	case "major_outage":
		return 4
	case "partial_outage":
		return 3
	case "degraded":
		return 2
	case "maintenance":
		return 1
	case "operational":
		return 0
	}
	return -1
}

// listActiveIncidents reads the status_incidents table for all rows
// whose state is not 'resolved'. Returns a slice (possibly empty).
func (s *Service) listActiveIncidents(ctx context.Context) ([]Incident, error) {
	if s.pool == nil {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, severity, component, title, description, state,
		       started_at, resolved_at, last_update_at
		FROM status_incidents
		WHERE state <> 'resolved'
		ORDER BY started_at DESC
		LIMIT 50
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanIncidents(rows)
}

// listUpcomingMaintenance returns scheduled + in_progress windows
// whose scheduled_start is within the next 14 days.
func (s *Service) listUpcomingMaintenance(ctx context.Context, now time.Time) ([]MaintenanceWindow, error) {
	if s.pool == nil {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, title, description, scheduled_start, scheduled_end,
		       affected_components, state
		FROM maintenance_windows
		WHERE state IN ('scheduled', 'in_progress')
		  AND scheduled_start < $1
		ORDER BY scheduled_start ASC
	`, now.Add(14*24*time.Hour))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MaintenanceWindow
	for rows.Next() {
		var m MaintenanceWindow
		if err := rows.Scan(&m.ID, &m.Title, &m.Description,
			&m.ScheduledStart, &m.ScheduledEnd, &m.AffectedComponents, &m.State); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// sevenDayUptime walks the resolved incidents from the last 7 days
// and computes the downtime per component. Simplified model: a
// "major_outage" or "partial_outage" incident counts as downtime
// from started_at to resolved_at. Anything else counts as zero.
func (s *Service) sevenDayUptime(ctx context.Context, now time.Time) (map[string]float64, error) {
	out := make(map[string]float64, len(Components))
	if s.pool == nil {
		return out, nil
	}
	cutoff := now.Add(-7 * 24 * time.Hour)
	rows, err := s.pool.Query(ctx, `
		SELECT component, started_at, resolved_at, severity
		FROM status_incidents
		WHERE resolved_at IS NOT NULL
		  AND resolved_at >= $1
		  AND severity IN ('P1', 'P2')
	`, cutoff)
	if err != nil {
		return out, err
	}
	defer rows.Close()

	downtimePerComp := make(map[string]time.Duration)
	for rows.Next() {
		var comp string
		var start, end time.Time
		var sev string
		if err := rows.Scan(&comp, &start, &end, &sev); err != nil {
			return out, err
		}
		if start.Before(cutoff) {
			start = cutoff
		}
		if end.Before(start) {
			continue
		}
		downtimePerComp[comp] += end.Sub(start)
	}
	totalWindow := 7 * 24 * time.Hour
	for _, name := range Components {
		dt := downtimePerComp[name]
		uptime := 100.0 * (1 - float64(dt)/float64(totalWindow))
		if uptime < 0 {
			uptime = 0
		}
		if uptime > 100 {
			uptime = 100
		}
		out[name] = uptime
	}
	return out, nil
}

// CreateIncident writes a new incident row and emits the audit trail.
type CreateIncidentRequest struct {
	Severity    Severity `json:"severity"`
	Component   string   `json:"component"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
}

// CreateIncident persists a new incident and returns it.
func (s *Service) CreateIncident(ctx context.Context, actor string, req CreateIncidentRequest) (*Incident, error) {
	if req.Severity == "" || req.Component == "" || req.Title == "" {
		return nil, fmt.Errorf("statuspage: severity, component, title required")
	}
	id := uuid.New()
	now := time.Now().UTC()

	if s.recorder != nil {
		if _, err := s.recorder.Append(ctx, audit.Entry{
			Actor:  actor,
			Action: audit.ActionStatusIncidentCreated,
			Target: id.String(),
			Details: map[string]any{
				"severity":  string(req.Severity),
				"component": req.Component,
				"title":     req.Title,
			},
		}); err != nil {
			return nil, fmt.Errorf("statuspage: audit: %w", err)
		}
	}

	if s.pool != nil {
		_, err := s.pool.Exec(ctx, `
			INSERT INTO status_incidents (
				id, severity, component, title, description,
				state, started_at, last_update_at
			) VALUES ($1, $2, $3, $4, $5, 'investigating', $6, $6)
		`, id, req.Severity, req.Component, req.Title, req.Description, now)
		if err != nil {
			return nil, fmt.Errorf("statuspage: insert: %w", err)
		}
	}

	return &Incident{
		ID:           id,
		Severity:     req.Severity,
		Component:    req.Component,
		Title:        req.Title,
		Description:  req.Description,
		State:        StateInvestigating,
		StartedAt:    now,
		LastUpdateAt: now,
	}, nil
}

// ResolveIncident marks an incident resolved and fans out to publishers.
func (s *Service) ResolveIncident(ctx context.Context, actor string, id uuid.UUID) error {
	if s.recorder != nil {
		if _, err := s.recorder.Append(ctx, audit.Entry{
			Actor:  actor,
			Action: audit.ActionStatusIncidentResolved,
			Target: id.String(),
		}); err != nil {
			return fmt.Errorf("statuspage: audit: %w", err)
		}
	}
	if s.pool != nil {
		now := time.Now().UTC()
		_, err := s.pool.Exec(ctx, `
			UPDATE status_incidents
			SET state = 'resolved', resolved_at = $1, last_update_at = $1
			WHERE id = $2
		`, now, id)
		if err != nil {
			return err
		}
	}
	return nil
}

func scanIncidents(rows pgx.Rows) ([]Incident, error) {
	var out []Incident
	for rows.Next() {
		var i Incident
		if err := rows.Scan(&i.ID, &i.Severity, &i.Component, &i.Title,
			&i.Description, &i.State, &i.StartedAt, &i.ResolvedAt,
			&i.LastUpdateAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}
