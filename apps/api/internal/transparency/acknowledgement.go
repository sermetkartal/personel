package transparency

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/personel/api/internal/audit"
)

// AcknowledgementRecord is a first-login acknowledgement stored in
// first_login_acknowledgements.
type AcknowledgementRecord struct {
	UserID             string
	AydinlatmaVersion  string
	AcknowledgedAt     time.Time
	AuditID            string
	Locale             string
}

// acknowledgementStore handles persistence for first-login acknowledgements.
// It is an unexported type used only by Service.
type acknowledgementStore struct {
	pool *pgxpool.Pool
}

// newAcknowledgementStore creates an acknowledgementStore.
func newAcknowledgementStore(pool *pgxpool.Pool) *acknowledgementStore {
	return &acknowledgementStore{pool: pool}
}

// Get returns an existing acknowledgement for (userID, aydinlatmaVersion), or
// (nil, nil) if none exists.
func (s *acknowledgementStore) Get(ctx context.Context, userID, aydinlatmaVersion string) (*AcknowledgementRecord, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT user_id::text, aydinlatma_version, acknowledged_at, audit_id, locale
		 FROM first_login_acknowledgements
		 WHERE user_id = $1::uuid AND aydinlatma_version = $2`,
		userID, aydinlatmaVersion,
	)
	var rec AcknowledgementRecord
	err := row.Scan(&rec.UserID, &rec.AydinlatmaVersion, &rec.AcknowledgedAt, &rec.AuditID, &rec.Locale)
	if err != nil {
		// pgx returns pgx.ErrNoRows when no row matches — treat as "not found" (nil, nil).
		return nil, nil //nolint:nilerr
	}
	return &rec, nil
}

// Insert writes a new acknowledgement row. Returns the AcknowledgementRecord on
// success. If a row already exists for (userID, aydinlatmaVersion) the insert is
// silently skipped (ON CONFLICT DO NOTHING) and the caller should call Get first
// to check for idempotency.
func (s *acknowledgementStore) Insert(ctx context.Context, userID, aydinlatmaVersion, locale, auditID string, at time.Time) (*AcknowledgementRecord, error) {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO first_login_acknowledgements
		   (user_id, aydinlatma_version, acknowledged_at, audit_id, locale)
		 VALUES ($1::uuid, $2, $3, $4, $5)
		 ON CONFLICT (user_id, aydinlatma_version) DO NOTHING`,
		userID, aydinlatmaVersion, at, auditID, locale,
	)
	if err != nil {
		return nil, fmt.Errorf("transparency: insert acknowledgement: %w", err)
	}
	return &AcknowledgementRecord{
		UserID:            userID,
		AydinlatmaVersion: aydinlatmaVersion,
		AcknowledgedAt:    at,
		AuditID:           auditID,
		Locale:            locale,
	}, nil
}

// AcknowledgeInput is the input for Service.AcknowledgeNotification.
type AcknowledgeInput struct {
	UserID            string
	TenantID          string
	NotificationType  string
	AydinlatmaVersion string
	Locale            string
}

// AcknowledgeResult is the output of Service.AcknowledgeNotification.
type AcknowledgeResult struct {
	AcknowledgedAt string `json:"acknowledged_at"` // ISO8601
	AuditID        string `json:"audit_id"`
	AlreadyDone    bool   `json:"-"` // true → return 200 instead of 201
}

// AcknowledgeNotification writes (or retrieves) the first-login acknowledgement
// for an employee. Idempotent: if the employee has already acknowledged this
// aydinlatma_version, the existing audit_id is returned with AlreadyDone=true.
func (s *Service) AcknowledgeNotification(ctx context.Context, rec *audit.Recorder, in AcknowledgeInput) (*AcknowledgeResult, error) {
	store := newAcknowledgementStore(s.pg)

	// Idempotency check first — no audit entry on re-submission.
	existing, err := store.Get(ctx, in.UserID, in.AydinlatmaVersion)
	if err != nil {
		return nil, fmt.Errorf("transparency: acknowledge: check existing: %w", err)
	}
	if existing != nil {
		return &AcknowledgeResult{
			AcknowledgedAt: existing.AcknowledgedAt.Format("2006-01-02T15:04:05Z"),
			AuditID:        existing.AuditID,
			AlreadyDone:    true,
		}, nil
	}

	// New acknowledgement — audit BEFORE the DB write.
	auditID, err := rec.Append(ctx, audit.Entry{
		Actor:    in.UserID,
		TenantID: in.TenantID,
		Action:   audit.ActionFirstLoginAcknowledged,
		Target:   fmt.Sprintf("employee:%s", in.UserID),
		Details: map[string]any{
			"notification_type":  in.NotificationType,
			"aydinlatma_version": in.AydinlatmaVersion,
			"locale":             in.Locale,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("transparency: acknowledge: audit: %w", err)
	}

	auditRef := fmt.Sprintf("audit:%d", auditID)

	now := time.Now().UTC()
	written, err := store.Insert(ctx, in.UserID, in.AydinlatmaVersion, in.Locale, auditRef, now)
	if err != nil {
		return nil, fmt.Errorf("transparency: acknowledge: insert: %w", err)
	}

	return &AcknowledgeResult{
		AcknowledgedAt: written.AcknowledgedAt.Format("2006-01-02T15:04:05Z"),
		AuditID:        written.AuditID,
		AlreadyDone:    false,
	}, nil
}
