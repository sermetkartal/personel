// Package screenshots — screenshot gallery via MinIO presigned URLs.
//
// EVERY issuance is audited with actor, target screenshot ID, and reason_code.
// Only Investigator and DPO roles can access screenshots (enforced in RBAC).
// No admin, manager, or HR role has access.
package screenshots

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	minioclient "github.com/personel/api/internal/minio"
	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/auth"
)

// Screenshot metadata returned to callers.
type Screenshot struct {
	ID          string    `json:"id"`
	TenantID    string    `json:"tenant_id"`
	EndpointID  string    `json:"endpoint_id"`
	OccurredAt  time.Time `json:"occurred_at"`
	MinIOKey    string    `json:"-"` // never serialized; internal use only
	IsSensitive bool      `json:"is_sensitive"`
	ExpiresAt   time.Time `json:"expires_at"` // retention expiry
}

// Service manages screenshot presigned URL issuance.
type Service struct {
	minio    *minioclient.Client
	recorder *audit.Recorder
	presignTTL time.Duration
	log      *slog.Logger
}

// NewService creates the screenshots service.
func NewService(mc *minioclient.Client, rec *audit.Recorder, presignTTL time.Duration, log *slog.Logger) *Service {
	return &Service{minio: mc, recorder: rec, presignTTL: presignTTL, log: log}
}

// PresignedURLRequest is the input for issuing a presigned URL.
type PresignedURLRequest struct {
	ScreenshotID string
	TenantID     string
	EndpointID   string
	MinIOKey     string
	ReasonCode   string // MANDATORY — empty is rejected
	ActorID      string
}

// IssuePresignedURL generates a short-lived (60s default) presigned URL.
// MANDATORY: audit entry is written BEFORE the URL is generated.
// If the audit write fails, the URL is NOT issued.
func (s *Service) IssuePresignedURL(ctx context.Context, p *auth.Principal, req PresignedURLRequest) (string, error) {
	// Enforce RBAC — already checked in middleware, but double-check here.
	if !auth.Can(p, auth.OpRead, auth.ResourceScreenshot) {
		return "", auth.ErrForbidden
	}
	if req.ReasonCode == "" {
		return "", fmt.Errorf("screenshots: reason_code is required for every access")
	}

	// Audit BEFORE issuing the URL. This is non-negotiable.
	_, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    p.UserID,
		TenantID: p.TenantID,
		Action:   audit.ActionScreenshotViewed,
		Target:   fmt.Sprintf("screenshot:%s", req.ScreenshotID),
		Details: map[string]any{
			"endpoint_id":   req.EndpointID,
			"reason_code":   req.ReasonCode,
			"presign_ttl_s": int(s.presignTTL.Seconds()),
		},
	})
	if err != nil {
		return "", fmt.Errorf("screenshots: audit failed — URL not issued: %w", err)
	}

	url, err := s.minio.PresignedGetURL(ctx, req.MinIOKey, s.presignTTL)
	if err != nil {
		return "", fmt.Errorf("screenshots: presign: %w", err)
	}
	return url, nil
}

// List returns screenshot metadata (not the blobs themselves) for an endpoint.
// The actual presigned URLs are issued per-item via IssuePresignedURL.
func (s *Service) List(ctx context.Context, tenantID, endpointID string, from, to time.Time) ([]*Screenshot, error) {
	// In a full implementation, this would query Postgres screenshot_metadata table.
	// Stub: return empty for now; the ClickHouse/MinIO writer service owns the metadata.
	s.log.Debug("screenshots: list",
		slog.String("tenant_id", tenantID),
		slog.String("endpoint_id", endpointID),
	)
	return nil, nil
}
