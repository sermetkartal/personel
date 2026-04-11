// Package backup — evidence collector for backup runs.
//
// Personel backups are executed by an out-of-API cron (systemd timer that
// invokes infra/scripts/backup.sh). After a successful run the runner
// POSTs the run metadata to /v1/system/backup-runs; this service creates
// the corresponding SOC 2 evidence item (control A1.2 + CC9.1) so that
// auditors can verify the observation window had unbroken backup coverage.
//
// The backup payload is not the backup ITSELF — the actual dumps live in
// the backup MinIO bucket with lifecycle rules. This service only records
// metadata (duration, size, checksum, target path) so the evidence pack
// stays small.
package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/evidence"
)

// RunReport is what the backup runner submits after a successful backup.
type RunReport struct {
	// TenantID is the tenant whose data was backed up. May be empty for
	// platform-level backups (Vault, Postgres global); empty is recorded
	// as "platform" in the tenant_id column for query purposes.
	TenantID string `json:"tenant_id,omitempty"`

	// Kind is the backup type: "postgres", "clickhouse", "minio", "vault".
	Kind string `json:"kind"`

	// TargetPath is the backup artifact location (MinIO key or filesystem
	// path), used for forensic recovery.
	TargetPath string `json:"target_path"`

	// SizeBytes is the total backup artifact size.
	SizeBytes int64 `json:"size_bytes"`

	// SHA256 is the hex-encoded checksum of the backup artifact. Auditors
	// use this to verify that a restored backup matches the recorded run.
	SHA256 string `json:"sha256"`

	// StartedAt / FinishedAt frame the actual run duration.
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`

	// SourceHost is the host that produced the backup (hostname + pid).
	SourceHost string `json:"source_host"`
}

// Service records backup run evidence.
type Service struct {
	recorder         *audit.Recorder
	evidenceRecorder evidence.Recorder
	log              *slog.Logger
}

// NewService creates a backup evidence service. evidenceRecorder may be
// nil in scaffold mode; the service then only writes the audit entry and
// skips the evidence emission (loud log at startup).
func NewService(rec *audit.Recorder, er evidence.Recorder, log *slog.Logger) *Service {
	return &Service{recorder: rec, evidenceRecorder: er, log: log}
}

// RecordRun validates the report, writes an audit entry, and emits an
// A1.2 KindBackupRun evidence item. Returns the evidence item ID (or
// empty string in scaffold mode) and any error from the audit write —
// evidence emission errors are swallowed and logged like every other
// collector.
func (s *Service) RecordRun(ctx context.Context, r RunReport) (string, error) {
	if r.Kind == "" || r.TargetPath == "" || r.SHA256 == "" {
		return "", fmt.Errorf("backup: RecordRun requires kind, target_path, sha256")
	}
	if r.StartedAt.IsZero() || r.FinishedAt.IsZero() {
		return "", fmt.Errorf("backup: RecordRun requires started_at and finished_at")
	}
	if r.FinishedAt.Before(r.StartedAt) {
		return "", fmt.Errorf("backup: finished_at must be >= started_at")
	}

	tenantForRow := r.TenantID
	if tenantForRow == "" {
		tenantForRow = "platform"
	}

	auditID, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    r.SourceHost,
		TenantID: tenantForRow,
		Action:   audit.ActionBackupRun,
		Target:   fmt.Sprintf("backup:%s", r.Kind),
		Details: map[string]any{
			"target_path": r.TargetPath,
			"size_bytes":  r.SizeBytes,
			"sha256":      r.SHA256,
		},
	})
	if err != nil {
		return "", fmt.Errorf("backup: audit: %w", err)
	}

	if s.evidenceRecorder == nil {
		return "", nil
	}

	durationSeconds := int64(r.FinishedAt.Sub(r.StartedAt).Seconds())

	payload, err := json.Marshal(map[string]any{
		"kind":             r.Kind,
		"target_path":      r.TargetPath,
		"size_bytes":       r.SizeBytes,
		"sha256":           r.SHA256,
		"started_at":       r.StartedAt.Format(time.RFC3339Nano),
		"finished_at":      r.FinishedAt.Format(time.RFC3339Nano),
		"duration_seconds": durationSeconds,
		"source_host":      r.SourceHost,
	})
	if err != nil {
		s.log.ErrorContext(ctx, "backup: evidence payload marshal failed",
			slog.String("error", err.Error()))
		return "", nil
	}

	item := evidence.Item{
		TenantID:   tenantForRow,
		Control:    evidence.CtrlA1_2,
		Kind:       evidence.KindBackupRun,
		RecordedAt: r.FinishedAt,
		Actor:      r.SourceHost,
		SummaryTR: fmt.Sprintf(
			"Yedekleme çalıştı — tür %s, boyut %d bayt, süre %ds, SHA256 %s…",
			r.Kind, r.SizeBytes, durationSeconds, safePrefix(r.SHA256, 12),
		),
		SummaryEN: fmt.Sprintf(
			"Backup completed — kind=%s size=%d duration=%ds sha256=%s…",
			r.Kind, r.SizeBytes, durationSeconds, safePrefix(r.SHA256, 12),
		),
		Payload:            payload,
		ReferencedAuditIDs: []int64{auditID},
		AttachmentRefs:     []string{r.TargetPath},
	}

	id, err := s.evidenceRecorder.Record(ctx, item)
	if err != nil {
		s.log.ErrorContext(ctx, "backup: SOC 2 evidence emission failed",
			slog.String("kind", r.Kind),
			slog.String("error", err.Error()))
		return "", nil
	}
	return id, nil
}

func safePrefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
