// Package sync is the HRIS sync orchestrator. It runs periodic polls,
// consumes incremental webhook pushes, and reconciles HRIS state with
// the Personel `users` table.
//
// Phase 2.5 scaffold: exposes the orchestrator interface and lifecycle,
// but defers actual DB writes to Phase 2.6 when the Connector implementations
// produce real employee data.
package sync

import (
	"context"
	"log/slog"
	"time"

	"github.com/personel/api/internal/hris"
)

// Orchestrator runs a Connector on a schedule, diffs the returned employees
// against the current users table, and upserts changes with an audit trail.
//
// One Orchestrator instance per tenant per connector. The api cmd/ binary
// spawns one goroutine per (tenant, connector) pair at startup.
type Orchestrator struct {
	connector    hris.Connector
	tenantID     string
	pollInterval time.Duration
	log          *slog.Logger

	// Phase 2.6 will add:
	// - pool *pgxpool.Pool for the users table
	// - recorder *audit.Recorder for hris.sync.* audit entries
	// - retentionClock triggering KVKK countdown on terminations
}

// Options configures an Orchestrator.
type Options struct {
	Connector    hris.Connector
	TenantID     string
	PollInterval time.Duration // 0 means use connector's Capabilities.PollInterval or global default
	Log          *slog.Logger
}

// New constructs an Orchestrator but does not start it. Call Run to begin.
func New(opts Options) *Orchestrator {
	interval := opts.PollInterval
	if interval == 0 {
		interval = opts.Connector.Capabilities().PollInterval
	}
	if interval == 0 {
		interval = 1 * time.Hour // global default
	}
	return &Orchestrator{
		connector:    opts.Connector,
		tenantID:     opts.TenantID,
		pollInterval: interval,
		log:          opts.Log,
	}
}

// Run blocks until ctx is cancelled, orchestrating polls and optional
// webhook consumption. Phase 2.5 scaffold: logs a startup message and
// returns. Phase 2.6 will implement the actual loop.
//
// Expected Phase 2.6 behavior:
//  1. At startup: perform TestConnection; on auth error, page DPO and exit
//  2. Immediate full sync via ListEmployees
//  3. Start WatchChanges goroutine if the connector supports webhooks
//  4. Start polling timer at PollInterval (used as a safety net even
//     when webhooks are active, because webhook delivery is best-effort)
//  5. On each sync round, diff against users table and emit upsert SQL
//     inside a transaction with an audit.Append entry per change
//  6. Handle termination events: mark user inactive, set terminated_at,
//     schedule KVKK retention countdown
//  7. On ctx cancellation: close channels, flush in-flight work, return
func (o *Orchestrator) Run(ctx context.Context) error {
	o.log.InfoContext(ctx, "hris orchestrator starting",
		slog.String("connector", o.connector.Name()),
		slog.String("tenant_id", o.tenantID),
		slog.Duration("poll_interval", o.pollInterval),
	)

	// Phase 2.5 scaffold: verify the connector is well-formed by calling
	// TestConnection. If it returns the expected scaffold error, log and
	// continue; otherwise, surface the real error. Phase 2.6 will replace
	// this with a real sync loop.
	if err := o.connector.TestConnection(ctx); err != nil {
		if hris.IsAuth(err) {
			o.log.ErrorContext(ctx, "hris connector auth failed at startup",
				slog.String("error", err.Error()))
			return err
		}
		// Scaffold error — not fatal for Phase 2.5
		o.log.WarnContext(ctx, "hris connector TestConnection returned (scaffold)",
			slog.String("error", err.Error()))
	}

	// Wait for ctx cancellation — Phase 2.5 does not actually sync.
	<-ctx.Done()
	o.log.InfoContext(ctx, "hris orchestrator stopping",
		slog.String("connector", o.connector.Name()),
	)
	return ctx.Err()
}

// Capabilities returns the underlying connector's capability matrix.
func (o *Orchestrator) Capabilities() hris.Capabilities {
	return o.connector.Capabilities()
}

// PollInterval is the effective poll interval after applying defaults.
func (o *Orchestrator) PollInterval() time.Duration { return o.pollInterval }
