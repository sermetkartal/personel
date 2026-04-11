// Package audit provides the audit recorder that wraps the
// audit.append_event stored procedure.
//
// IMPORTANT: Every admin-facing handler MUST call Recorder.Append BEFORE
// performing any database write or NATS publish. This is enforced by a CI
// test that stubs the recorder and asserts it was called.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Entry represents a single event to be appended to the audit log.
type Entry struct {
	Actor    string         // user id, service identity, or "system"
	ActorIP  net.IP         // may be nil
	ActorUA  string         // user-agent string; may be empty
	TenantID string         // UUID string
	Action   Action         // from actions.go
	Target   string         // resource identifier
	Details  map[string]any // arbitrary structured details
}

// Recorder appends audit events using the stored procedure.
// The stored procedure serializes appends with pg_advisory_xact_lock
// so this method is safe for concurrent callers.
type Recorder struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

// NewRecorder creates a Recorder backed by the given pool.
func NewRecorder(pool *pgxpool.Pool, log *slog.Logger) *Recorder {
	return &Recorder{pool: pool, log: log}
}

// Append writes one audit entry. It blocks until the stored procedure commits.
// Returns the assigned bigint ID or an error.
// The caller is responsible for calling this BEFORE the side effect.
func (r *Recorder) Append(ctx context.Context, e Entry) (int64, error) {
	if !ValidAction(e.Action) {
		return 0, fmt.Errorf("audit: unknown action %q — add it to actions.go", e.Action)
	}

	detailsJSON, err := json.Marshal(e.Details)
	if err != nil {
		return 0, fmt.Errorf("audit: marshal details: %w", err)
	}

	var actorIP *string
	if e.ActorIP != nil {
		s := e.ActorIP.String()
		actorIP = &s
	}

	var id int64
	err = r.pool.QueryRow(ctx,
		`SELECT audit.append_event($1, $2::inet, $3, $4::uuid, $5, $6, $7::jsonb)`,
		e.Actor,
		actorIP,
		e.ActorUA,
		e.TenantID,
		string(e.Action),
		e.Target,
		detailsJSON,
	).Scan(&id)
	if err != nil {
		r.log.Error("audit: append_event failed",
			slog.String("action", string(e.Action)),
			slog.String("actor", e.Actor),
			slog.String("target", e.Target),
			slog.Any("error", err),
		)
		return 0, fmt.Errorf("audit: append_event: %w", err)
	}

	r.log.Debug("audit: event appended",
		slog.Int64("id", id),
		slog.String("action", string(e.Action)),
		slog.String("actor", e.Actor),
	)
	return id, nil
}

// AppendSystem is a convenience wrapper that sets Actor to "system" and
// ActorIP/ActorUA to empty. Used by background jobs.
func (r *Recorder) AppendSystem(ctx context.Context, tenantID string, action Action, target string, details map[string]any) (int64, error) {
	return r.Append(ctx, Entry{
		Actor:    "system",
		TenantID: tenantID,
		Action:   action,
		Target:   target,
		Details:  details,
	})
}

// contextKey for the recorder so middleware can inject it.
type contextKey struct{}

// WithRecorder stores the recorder in ctx.
func WithRecorder(ctx context.Context, r *Recorder) context.Context {
	return context.WithValue(ctx, contextKey{}, r)
}

// FromContext retrieves the recorder. Panics if not set, because every
// admin-facing request MUST have it — absence is a programmer error.
func FromContext(ctx context.Context) *Recorder {
	r, ok := ctx.Value(contextKey{}).(*Recorder)
	if !ok || r == nil {
		panic("audit: recorder not in context — check audit middleware is applied")
	}
	return r
}

// NopRecorder is a no-op recorder for use in unit tests. It records calls
// so tests can assert Append was invoked.
type NopRecorder struct {
	Calls []Entry
}

func (n *NopRecorder) Append(_ context.Context, e Entry) (int64, error) {
	n.Calls = append(n.Calls, e)
	return int64(len(n.Calls)), nil
}
