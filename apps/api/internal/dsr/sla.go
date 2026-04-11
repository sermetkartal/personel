// Package dsr — SLA timer job and escalation logic.
//
// Runs as a background goroutine via a ticker. On each tick:
//  1. Transition open→at_risk at day 20.
//  2. Transition at_risk→overdue at day 30.
//  3. Emit notifications for at-risk and overdue tickets.
//  4. At day 25, escalate to DPO secondary contact.
package dsr

import (
	"context"
	"log/slog"
	"time"
)

// Notifier sends notifications for SLA events.
type Notifier interface {
	NotifyDPO(ctx context.Context, tenantID, requestID, event string) error
	NotifyEmployee(ctx context.Context, tenantID, requestID, employeeID, event string) error
	EscalateToDPOSecondary(ctx context.Context, tenantID, requestID string) error
}

// SLAJob ticks daily and enforces SLA thresholds.
type SLAJob struct {
	store     *Store
	notifier  Notifier
	tenantIDs []string // list of tenants to process
	log       *slog.Logger
}

// NewSLAJob creates the SLA job.
func NewSLAJob(store *Store, notifier Notifier, tenantIDs []string, log *slog.Logger) *SLAJob {
	return &SLAJob{store: store, notifier: notifier, tenantIDs: tenantIDs, log: log}
}

// Run starts the daily SLA tick. Blocks until ctx is done.
func (j *SLAJob) Run(ctx context.Context) {
	// Align to next midnight + 5 minutes for first run.
	now := time.Now()
	nextRun := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 5, 0, 0, now.Location())
	timer := time.NewTimer(time.Until(nextRun))
	defer timer.Stop()

	j.log.Info("dsr sla job: scheduled", slog.Time("next_run", nextRun))

	for {
		select {
		case <-ctx.Done():
			j.log.Info("dsr sla job: stopping")
			return
		case <-timer.C:
			j.tick(ctx)
			// Reset for next day.
			nextRun = nextRun.Add(24 * time.Hour)
			timer.Reset(time.Until(nextRun))
		}
	}
}

func (j *SLAJob) tick(ctx context.Context) {
	for _, tid := range j.tenantIDs {
		if err := j.processTeant(ctx, tid); err != nil {
			j.log.Error("dsr sla job: process tenant",
				slog.String("tenant_id", tid),
				slog.Any("error", err),
			)
		}
	}
}

func (j *SLAJob) processTeant(ctx context.Context, tenantID string) error {
	// Transition states.
	if err := j.store.TickSLAs(ctx, tenantID); err != nil {
		return err
	}

	// Fetch at-risk and overdue for notifications.
	atRisk, err := j.store.List(ctx, tenantID, []State{StateAtRisk})
	if err != nil {
		return err
	}
	for _, r := range atRisk {
		_ = j.notifier.NotifyDPO(ctx, tenantID, r.ID, "at_risk")
		// At day 25: sla_deadline - now <= 5 days → escalate.
		remaining := time.Until(r.SLADeadline)
		if remaining <= 5*24*time.Hour && remaining > 4*24*time.Hour {
			_ = j.notifier.EscalateToDPOSecondary(ctx, tenantID, r.ID)
		}
	}

	overdue, err := j.store.List(ctx, tenantID, []State{StateOverdue})
	if err != nil {
		return err
	}
	for _, r := range overdue {
		_ = j.notifier.NotifyDPO(ctx, tenantID, r.ID, "overdue")
	}

	j.log.Info("dsr sla job: tick completed",
		slog.String("tenant_id", tenantID),
		slog.Int("at_risk", len(atRisk)),
		slog.Int("overdue", len(overdue)),
	)
	return nil
}
