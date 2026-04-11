// Package dsr — Postgres store for KVKK m.11 Data Subject Requests.
package dsr

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
)

// RequestType represents the type of a DSR.
type RequestType string

const (
	RequestTypeAccess      RequestType = "access"
	RequestTypeRectify     RequestType = "rectify"
	RequestTypeErase       RequestType = "erase"
	RequestTypeObject      RequestType = "object"
	RequestTypeRestrict    RequestType = "restrict"
	RequestTypePortability RequestType = "portability"
)

// State is the lifecycle state of a DSR.
type State string

const (
	StateOpen     State = "open"
	StateAtRisk   State = "at_risk"   // day 20+
	StateOverdue  State = "overdue"   // day 30+
	StateResolved State = "resolved"
	StateRejected State = "rejected"
)

// Request is the DSR aggregate.
type Request struct {
	ID                  string      `json:"id"`
	TenantID            string      `json:"tenant_id"`
	EmployeeUserID      string      `json:"employee_user_id"`
	RequestType         RequestType `json:"request_type"`
	ScopeJSON           []byte      `json:"scope_json"`
	Justification       string      `json:"justification"`
	State               State       `json:"state"`
	CreatedAt           time.Time   `json:"created_at"`
	SLADeadline         time.Time   `json:"sla_deadline"`
	AssignedTo          *string     `json:"assigned_to,omitempty"`
	ResponseArtifactRef *string     `json:"response_artifact_ref,omitempty"`
	AuditChainRef       *string     `json:"audit_chain_ref,omitempty"`
	ExtendedAt          *time.Time  `json:"extended_at,omitempty"`
	ExtensionReason     *string     `json:"extension_reason,omitempty"`
	ClosedAt            *time.Time  `json:"closed_at,omitempty"`
}

// Store handles all DSR persistence.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a Store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Create inserts a new DSR record. Returns the assigned ID.
func (s *Store) Create(ctx context.Context, r *Request) (string, error) {
	id := ulid.Make().String()
	sla := r.CreatedAt.AddDate(0, 0, 30)
	_, err := s.pool.Exec(ctx,
		`INSERT INTO dsr_requests
		 (id, tenant_id, employee_user_id, request_type, scope_json, justification, state, created_at, sla_deadline)
		 VALUES ($1, $2::uuid, $3::uuid, $4, $5::jsonb, $6, $7, $8, $9)`,
		id, r.TenantID, r.EmployeeUserID, string(r.RequestType),
		r.ScopeJSON, r.Justification, string(StateOpen), r.CreatedAt, sla,
	)
	if err != nil {
		return "", fmt.Errorf("dsr: create: %w", err)
	}
	return id, nil
}

// Get retrieves a DSR by ID.
func (s *Store) Get(ctx context.Context, id, tenantID string) (*Request, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id::text, employee_user_id::text, request_type,
		        scope_json, justification, state, created_at, sla_deadline,
		        assigned_to::text, response_artifact_ref, audit_chain_ref,
		        extended_at, extension_reason, closed_at
		 FROM dsr_requests
		 WHERE id = $1 AND tenant_id = $2::uuid`,
		id, tenantID,
	)
	return scanRequest(row)
}

// List returns DSRs filtered by state.
func (s *Store) List(ctx context.Context, tenantID string, states []State) ([]*Request, error) {
	// states == nil ⇒ caller wants every state. Pass nil (not an empty
	// slice) so the SQL "$2::text[] IS NULL" branch matches; an empty
	// slice would make state = ANY(ARRAY[]) which returns zero rows.
	// Caught by the DSR integration test on 2026-04-11 — the nil case
	// was silently returning no results.
	var stateStrings []string
	if states != nil {
		stateStrings = make([]string, len(states))
		for i, s := range states {
			stateStrings[i] = string(s)
		}
	}

	rows, err := s.pool.Query(ctx,
		`SELECT id, tenant_id::text, employee_user_id::text, request_type,
		        scope_json, justification, state, created_at, sla_deadline,
		        assigned_to::text, response_artifact_ref, audit_chain_ref,
		        extended_at, extension_reason, closed_at
		 FROM dsr_requests
		 WHERE tenant_id = $1::uuid
		   AND ($2::text[] IS NULL OR state = ANY($2::text[]))
		 ORDER BY created_at DESC`,
		tenantID, stateStrings,
	)
	if err != nil {
		return nil, fmt.Errorf("dsr: list: %w", err)
	}
	defer rows.Close()

	var out []*Request
	for rows.Next() {
		req, err := scanRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, req)
	}
	return out, rows.Err()
}

// Assign updates the assigned_to field.
func (s *Store) Assign(ctx context.Context, id, tenantID, assignedTo string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE dsr_requests SET assigned_to = $1::uuid WHERE id = $2 AND tenant_id = $3::uuid`,
		assignedTo, id, tenantID,
	)
	return wrapErr("dsr: assign", err)
}

// Respond closes the DSR with an artifact reference.
func (s *Store) Respond(ctx context.Context, id, tenantID, artifactRef, auditRef string) error {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx,
		`UPDATE dsr_requests
		 SET state = $1, response_artifact_ref = $2, audit_chain_ref = $3, closed_at = $4
		 WHERE id = $5 AND tenant_id = $6::uuid`,
		string(StateResolved), artifactRef, auditRef, now, id, tenantID,
	)
	return wrapErr("dsr: respond", err)
}

// Reject closes the DSR with rejected state.
func (s *Store) Reject(ctx context.Context, id, tenantID, reason string) error {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx,
		`UPDATE dsr_requests
		 SET state = $1, extension_reason = $2, closed_at = $3
		 WHERE id = $4 AND tenant_id = $5::uuid`,
		string(StateRejected), reason, now, id, tenantID,
	)
	return wrapErr("dsr: reject", err)
}

// Extend adds 30 days to the SLA deadline.
func (s *Store) Extend(ctx context.Context, id, tenantID, reason string) error {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx,
		`UPDATE dsr_requests
		 SET sla_deadline = sla_deadline + INTERVAL '30 days',
		     extended_at = $1, extension_reason = $2
		 WHERE id = $3 AND tenant_id = $4::uuid
		   AND state IN ('open', 'at_risk')`,
		now, reason, id, tenantID,
	)
	return wrapErr("dsr: extend", err)
}

// TickSLAs transitions open/at_risk requests that have crossed day thresholds.
// Called nightly by the SLA job.
func (s *Store) TickSLAs(ctx context.Context, tenantID string) error {
	now := time.Now().UTC()
	// open → at_risk at day 20
	_, err := s.pool.Exec(ctx,
		`UPDATE dsr_requests
		 SET state = 'at_risk'
		 WHERE tenant_id = $1::uuid
		   AND state = 'open'
		   AND sla_deadline - now() <= INTERVAL '10 days'`,
		tenantID,
	)
	if err != nil {
		return fmt.Errorf("dsr: tick sla at_risk: %w", err)
	}
	// at_risk → overdue at sla_deadline
	_, err = s.pool.Exec(ctx,
		`UPDATE dsr_requests
		 SET state = 'overdue'
		 WHERE tenant_id = $1::uuid
		   AND state IN ('open', 'at_risk')
		   AND sla_deadline <= $2`,
		tenantID, now,
	)
	return wrapErr("dsr: tick sla overdue", err)
}

// DashboardStats returns open/at_risk/overdue counts and median response time.
type DashboardStats struct {
	OpenCount      int
	AtRiskCount    int
	OverdueCount   int
	MedianResponseSeconds *float64 // nil if no closed requests
}

// Stats returns DPO dashboard aggregates.
func (s *Store) Stats(ctx context.Context, tenantID string) (*DashboardStats, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT
		   COUNT(*) FILTER (WHERE state = 'open') AS open_count,
		   COUNT(*) FILTER (WHERE state = 'at_risk') AS at_risk_count,
		   COUNT(*) FILTER (WHERE state = 'overdue') AS overdue_count,
		   PERCENTILE_CONT(0.5) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (closed_at - created_at)))
		     FILTER (WHERE closed_at IS NOT NULL AND created_at >= now() - INTERVAL '90 days') AS median_secs
		 FROM dsr_requests
		 WHERE tenant_id = $1::uuid`,
		tenantID,
	)
	var st DashboardStats
	err := row.Scan(&st.OpenCount, &st.AtRiskCount, &st.OverdueCount, &st.MedianResponseSeconds)
	return &st, wrapErr("dsr: stats", err)
}

// scanRequest is a helper to scan a request row.
type scanner interface {
	Scan(dest ...any) error
}

func scanRequest(row scanner) (*Request, error) {
	var r Request
	err := row.Scan(
		&r.ID, &r.TenantID, &r.EmployeeUserID, &r.RequestType,
		&r.ScopeJSON, &r.Justification, &r.State,
		&r.CreatedAt, &r.SLADeadline,
		&r.AssignedTo, &r.ResponseArtifactRef, &r.AuditChainRef,
		&r.ExtendedAt, &r.ExtensionReason, &r.ClosedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("dsr: scan: %w", err)
	}
	return &r, nil
}

func wrapErr(prefix string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", prefix, err)
}
