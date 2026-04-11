// Package liveview — Postgres persistence for live view sessions.
package liveview

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"
)

// Session is the persisted aggregate for a live view session/request.
type Session struct {
	ID                string
	TenantID          string
	EndpointID        string
	RequesterID       string
	ApproverID        *string
	ApprovalNotes     *string
	ReasonCode        string
	Justification     string
	RequestedDuration time.Duration
	State             State
	LiveKitRoom       *string
	LiveKitRoomStr    string // convenience accessor
	AdminToken        string
	AgentToken        string
	SigningKeyID       string
	CreatedAt         time.Time
	ApprovedAt        *time.Time
	StartedAt         *time.Time
	EndedAt           *time.Time
	FailureReason     *string
}

// Store handles all live view session persistence.
type Store struct {
	pool *pgxpool.Pool
}

// NewStore creates a Store.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Create inserts a new session record.
func (s *Store) Create(ctx context.Context, sess *Session) (string, error) {
	id := ulid.Make().String()
	_, err := s.pool.Exec(ctx,
		`INSERT INTO live_view_sessions
		 (id, tenant_id, endpoint_id, requester_id, reason_code, justification,
		  requested_duration_seconds, state, created_at)
		 VALUES ($1, $2::uuid, $3::uuid, $4::uuid, $5, $6, $7, $8, $9)`,
		id, sess.TenantID, sess.EndpointID, sess.RequesterID,
		sess.ReasonCode, sess.Justification,
		int64(sess.RequestedDuration.Seconds()),
		string(sess.State), sess.CreatedAt,
	)
	if err != nil {
		return "", fmt.Errorf("liveview: create: %w", err)
	}
	return id, nil
}

// Get retrieves a session by ID.
func (s *Store) Get(ctx context.Context, id, tenantID string) (*Session, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, tenant_id::text, endpoint_id::text, requester_id::text,
		        approver_id::text, approval_notes, reason_code, justification,
		        requested_duration_seconds, state, livekit_room,
		        created_at, approved_at, started_at, ended_at, failure_reason
		 FROM live_view_sessions
		 WHERE id = $1 AND tenant_id = $2::uuid`,
		id, tenantID,
	)
	return scanSession(row)
}

// List returns sessions optionally filtered by state.
func (s *Store) List(ctx context.Context, tenantID string, state *State) ([]*Session, error) {
	var rows interface{ Next() bool; Scan(...any) error; Close(); Err() error }
	var err error

	if state != nil {
		rows, err = s.pool.Query(ctx,
			`SELECT id, tenant_id::text, endpoint_id::text, requester_id::text,
			        approver_id::text, approval_notes, reason_code, justification,
			        requested_duration_seconds, state, livekit_room,
			        created_at, approved_at, started_at, ended_at, failure_reason
			 FROM live_view_sessions
			 WHERE tenant_id = $1::uuid AND state = $2
			 ORDER BY created_at DESC`,
			tenantID, string(*state),
		)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT id, tenant_id::text, endpoint_id::text, requester_id::text,
			        approver_id::text, approval_notes, reason_code, justification,
			        requested_duration_seconds, state, livekit_room,
			        created_at, approved_at, started_at, ended_at, failure_reason
			 FROM live_view_sessions
			 WHERE tenant_id = $1::uuid
			 ORDER BY created_at DESC
			 LIMIT 500`,
			tenantID,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("liveview: list: %w", err)
	}
	defer rows.Close()

	var out []*Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// ListByEmployee returns sessions targeting an employee's endpoints (for transparency portal).
func (s *Store) ListByEmployee(ctx context.Context, tenantID, employeeUserID string) ([]*Session, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT lv.id, lv.tenant_id::text, lv.endpoint_id::text, lv.requester_id::text,
		        lv.approver_id::text, lv.approval_notes, lv.reason_code, lv.justification,
		        lv.requested_duration_seconds, lv.state, lv.livekit_room,
		        lv.created_at, lv.approved_at, lv.started_at, lv.ended_at, lv.failure_reason
		 FROM live_view_sessions lv
		 JOIN endpoints e ON e.id = lv.endpoint_id AND e.tenant_id = lv.tenant_id
		 WHERE lv.tenant_id = $1::uuid AND e.assigned_user_id = $2::uuid
		 ORDER BY lv.created_at DESC`,
		tenantID, employeeUserID,
	)
	if err != nil {
		return nil, fmt.Errorf("liveview: list by employee: %w", err)
	}
	defer rows.Close()

	var out []*Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sess)
	}
	return out, rows.Err()
}

// SetState updates the state and optional metadata columns.
func (s *Store) SetState(ctx context.Context, id, tenantID string, state State, approverID *string, notes *string, at *time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE live_view_sessions
		 SET state = $1,
		     approver_id = COALESCE($2::uuid, approver_id),
		     approval_notes = COALESCE($3, approval_notes),
		     approved_at = CASE WHEN $1 = 'APPROVED' THEN $4 ELSE approved_at END,
		     started_at  = CASE WHEN $1 = 'ACTIVE'   THEN $4 ELSE started_at END,
		     ended_at    = CASE WHEN $1 IN ('ENDED','TERMINATED_BY_HR','TERMINATED_BY_DPO','EXPIRED','FAILED','DENIED') THEN $4 ELSE ended_at END
		 WHERE id = $5 AND tenant_id = $6::uuid`,
		string(state), approverID, notes, at, id, tenantID,
	)
	return wrapErr("liveview: set state", err)
}

// SetApprovalDetails stores the LiveKit room, admin/agent tokens.
func (s *Store) SetApprovalDetails(ctx context.Context, id, tenantID, room, adminToken, agentToken, signingKeyID string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE live_view_sessions
		 SET livekit_room = $1, admin_token = $2, agent_token = $3, signing_key_id = $4
		 WHERE id = $5 AND tenant_id = $6::uuid`,
		room, adminToken, agentToken, signingKeyID, id, tenantID,
	)
	return wrapErr("liveview: set approval details", err)
}

// MarkFailed marks a session as FAILED with a reason.
func (s *Store) MarkFailed(ctx context.Context, id, tenantID, reason string) error {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx,
		`UPDATE live_view_sessions
		 SET state = 'FAILED', failure_reason = $1, ended_at = $2
		 WHERE id = $3 AND tenant_id = $4::uuid`,
		reason, now, id, tenantID,
	)
	return wrapErr("liveview: mark failed", err)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanSession(row rowScanner) (*Session, error) {
	var s Session
	var durationSecs int64
	var room *string
	err := row.Scan(
		&s.ID, &s.TenantID, &s.EndpointID, &s.RequesterID,
		&s.ApproverID, &s.ApprovalNotes, &s.ReasonCode, &s.Justification,
		&durationSecs, &s.State, &room,
		&s.CreatedAt, &s.ApprovedAt, &s.StartedAt, &s.EndedAt, &s.FailureReason,
	)
	if err != nil {
		return nil, fmt.Errorf("liveview: scan: %w", err)
	}
	s.RequestedDuration = time.Duration(durationSecs) * time.Second
	s.LiveKitRoom = room
	if room != nil {
		s.LiveKitRoomStr = *room
	}
	return &s, nil
}

func wrapErr(prefix string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", prefix, err)
}
