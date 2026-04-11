// Package retention — TTL scheduler for live view recordings.
//
// Per ADR 0019 §Storage:
//   - Default retention: 30 days from session start.
//   - A daily job scans Postgres for sessions where ttl_expires_at <= now()
//     AND destroyed_at IS NULL AND legal_hold_id IS NULL.
//   - For each eligible session: delete MinIO objects, mark destroyed_at in
//     Postgres, fire async audit event.
//   - Legal hold flag is checked via LegalHoldChecker (separate interface).
//
// The 30-day TTL is the cryptographic shredding trigger: when MinIO objects
// are deleted the ciphertext becomes unrecoverable even if the wrapped DEK
// were somehow leaked (which it can't be after LVMK version trim).
package retention

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/personel/livrec/internal/audit"
	"github.com/personel/livrec/internal/storage"
)

// ExpiredSession holds the minimum data needed to delete a recording.
type ExpiredSession struct {
	SessionID   string
	TenantID    string
	ObjectKey   string // completed-session key in MinIO (from Postgres live_view_recordings)
	LVMKVersion int
}

// SessionExpirer queries Postgres for expired sessions. Implemented by
// the postgres package in production; stub in tests.
type SessionExpirer interface {
	ListExpiredSessions(ctx context.Context, now time.Time) ([]ExpiredSession, error)
	MarkDestroyed(ctx context.Context, sessionID string, destroyedAt time.Time) error
}

// LegalHoldChecker determines if a session has an active legal hold.
// Separate interface per §Retention design — actual Postgres query TBD in Phase 3.
type LegalHoldChecker interface {
	IsOnLegalHold(ctx context.Context, sessionID string) (bool, error)
}

// TTLScheduler runs the daily deletion job.
type TTLScheduler struct {
	expirer   SessionExpirer
	holdCheck LegalHoldChecker
	minio     *storage.Client
	auditor   *audit.Recorder
	interval  time.Duration
	log       *slog.Logger
}

// NewTTLScheduler creates a TTLScheduler.
// interval is the polling period (default 24h in production).
func NewTTLScheduler(
	expirer SessionExpirer,
	holdCheck LegalHoldChecker,
	minio *storage.Client,
	auditor *audit.Recorder,
	interval time.Duration,
	log *slog.Logger,
) *TTLScheduler {
	return &TTLScheduler{
		expirer:   expirer,
		holdCheck: holdCheck,
		minio:     minio,
		auditor:   auditor,
		interval:  interval,
		log:       log,
	}
}

// Run starts the TTL loop. Returns when ctx is cancelled.
// Run is designed to be called in a goroutine; it blocks until ctx is done.
func (s *TTLScheduler) Run(ctx context.Context) {
	s.log.Info("ttl scheduler started", slog.Duration("interval", s.interval))
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			s.log.Info("ttl scheduler stopped")
			return
		case t := <-ticker.C:
			if err := s.runCycle(ctx, t); err != nil {
				s.log.Error("ttl cycle failed", slog.Any("error", err))
			}
		}
	}
}

// runCycle performs one TTL deletion cycle.
func (s *TTLScheduler) runCycle(ctx context.Context, now time.Time) error {
	sessions, err := s.expirer.ListExpiredSessions(ctx, now)
	if err != nil {
		return fmt.Errorf("ttl: list expired sessions: %w", err)
	}

	s.log.Info("ttl cycle", slog.Int("candidates", len(sessions)))

	for _, sess := range sessions {
		if err := s.deleteSession(ctx, sess); err != nil {
			s.log.Error("ttl: failed to delete session",
				slog.String("session_id", sess.SessionID),
				slog.Any("error", err),
			)
			// Continue processing remaining sessions.
			continue
		}
	}
	return nil
}

// deleteSession checks legal hold, then removes MinIO objects and marks
// destroyed in Postgres.
func (s *TTLScheduler) deleteSession(ctx context.Context, sess ExpiredSession) error {
	// Legal hold check — skip if hold is active.
	held, err := s.holdCheck.IsOnLegalHold(ctx, sess.SessionID)
	if err != nil {
		return fmt.Errorf("ttl: legal hold check for %s: %w", sess.SessionID, err)
	}
	if held {
		s.log.Info("ttl: session on legal hold, skipping",
			slog.String("session_id", sess.SessionID))
		return nil
	}

	// Delete all chunk objects from MinIO.
	chunkPrefix := storage.ChunkPrefix(sess.TenantID, sess.SessionID)
	chunkKeys, err := s.minio.ListChunks(ctx, chunkPrefix)
	if err != nil {
		return fmt.Errorf("ttl: list chunks for %s: %w", sess.SessionID, err)
	}
	for _, key := range chunkKeys {
		if err := s.minio.DeleteObject(ctx, key); err != nil {
			return fmt.Errorf("ttl: delete chunk %s: %w", key, err)
		}
	}

	// Delete the completed session object if present.
	if sess.ObjectKey != "" {
		if err := s.minio.DeleteObject(ctx, sess.ObjectKey); err != nil && !isNotFound(err) {
			return fmt.Errorf("ttl: delete session object %s: %w", sess.ObjectKey, err)
		}
	}

	// Mark destroyed in Postgres.
	if err := s.expirer.MarkDestroyed(ctx, sess.SessionID, time.Now().UTC()); err != nil {
		return fmt.Errorf("ttl: mark destroyed for %s: %w", sess.SessionID, err)
	}

	// Async audit.
	go s.auditor.RecordDestruction(context.Background(),
		sess.TenantID, sess.SessionID, sess.LVMKVersion, "ttl_expired")

	s.log.Info("ttl: session destroyed",
		slog.String("session_id", sess.SessionID),
		slog.Int("chunks_deleted", len(chunkKeys)),
	)
	return nil
}

// isNotFound returns true if the error represents a MinIO 404 (object not found).
// The MinIO Go SDK returns an error with "NoSuchKey" in its code for missing objects.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "NoSuchKey") ||
		strings.Contains(err.Error(), "does not exist")
}
