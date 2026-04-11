// Package audit — thin async wrapper forwarding audit events to the Admin API.
//
// Per ADR 0019: livrec-service does NOT own its own audit chain. All audit
// events are forwarded to the main Admin API's internal endpoint:
//
//	POST /v1/internal/audit/livrec
//
// Per ADR 0019 quality bar: the audit forwarder is ASYNC and non-blocking.
// A failure to deliver an audit event MUST NOT break recording or playback.
// Events are fire-and-forget with a dedicated context that outlives the HTTP
// request context (uses a background-derived context with a short timeout).
package audit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// EventType enumerates the livrec audit event types defined in ADR 0019.
type EventType string

const (
	EventRecordingStarted  EventType = "live_view.recording_started"
	EventRecordingEnded    EventType = "live_view.recording_ended"
	EventChunkStored       EventType = "live_view.chunk_stored"
	EventPlaybackRequested EventType = "live_view.playback_requested"
	EventPlaybackStarted   EventType = "live_view.playback_started"
	EventPlaybackEnded     EventType = "live_view.playback_ended"
	EventRecordingExported EventType = "live_view.recording_exported"
	EventRecordingDestroyed EventType = "live_view.recording_destroyed"
)

// auditPayload is the JSON body sent to the Admin API audit endpoint.
type auditPayload struct {
	EventType EventType      `json:"event_type"`
	TenantID  string         `json:"tenant_id"`
	Actor     string         `json:"actor,omitempty"`
	Details   map[string]any `json:"details"`
	OccurredAt time.Time     `json:"occurred_at"`
}

// Recorder forwards audit events to the Admin API asynchronously.
type Recorder struct {
	adminBaseURL  string
	internalToken string
	client        *http.Client
	log           *slog.Logger
}

// NewRecorder constructs a Recorder.
func NewRecorder(adminBaseURL, internalToken string, log *slog.Logger) *Recorder {
	return &Recorder{
		adminBaseURL:  adminBaseURL,
		internalToken: internalToken,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
		log: log,
	}
}

// send is the internal async sender. It must be called from a goroutine.
// A failure to deliver is logged but never propagated to the caller.
func (r *Recorder) send(eventType EventType, tenantID, actor string, details map[string]any) {
	payload := auditPayload{
		EventType:  eventType,
		TenantID:   tenantID,
		Actor:      actor,
		Details:    details,
		OccurredAt: time.Now().UTC(),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		r.log.Error("audit: marshal payload failed",
			slog.String("event_type", string(eventType)),
			slog.Any("error", err),
		)
		return
	}

	// Use a background context with a dedicated timeout so audit delivery
	// does not interfere with the parent request lifecycle.
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx,
		http.MethodPost,
		fmt.Sprintf("%s/v1/internal/audit/livrec", r.adminBaseURL),
		bytes.NewReader(body),
	)
	if err != nil {
		r.log.Error("audit: build request failed", slog.Any("error", err))
		return
	}
	req.Header.Set("Authorization", "Bearer "+r.internalToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		r.log.Error("audit: http request failed",
			slog.String("event_type", string(eventType)),
			slog.Any("error", err),
		)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		r.log.Error("audit: admin api returned error",
			slog.String("event_type", string(eventType)),
			slog.Int("status", resp.StatusCode),
		)
	}
}

// RecordChunkStored fires an async audit event for a stored chunk.
// Called from upload/handler.go via go r.RecordChunkStored(...).
func (r *Recorder) RecordChunkStored(ctx context.Context, tenantID, sessionID string, chunkIndex uint64, sizeBytes int64) {
	r.send(EventChunkStored, tenantID, "livrec-service", map[string]any{
		"session_id":  sessionID,
		"chunk_index": chunkIndex,
		"size_bytes":  sizeBytes,
	})
}

// RecordRecordingStarted fires when egress begins for a session.
func (r *Recorder) RecordRecordingStarted(ctx context.Context, tenantID, sessionID, recordingID string, lvmkVersion int) {
	r.send(EventRecordingStarted, tenantID, "livrec-service", map[string]any{
		"session_id":   sessionID,
		"recording_id": recordingID,
		"lvmk_version": lvmkVersion,
	})
}

// RecordRecordingEnded fires when all chunks are stored for a session.
func (r *Recorder) RecordRecordingEnded(ctx context.Context, tenantID, sessionID, objectKey string, bytesTotal, frameCount int64) {
	r.send(EventRecordingEnded, tenantID, "livrec-service", map[string]any{
		"session_id": sessionID,
		"object_key": objectKey,
		"bytes_total": bytesTotal,
		"frame_count": frameCount,
	})
}

// RecordPlaybackStarted fires when the SSE stream begins for an approved viewer.
func (r *Recorder) RecordPlaybackStarted(ctx context.Context, tenantID, sessionID, viewerID, approverID string) {
	r.send(EventPlaybackStarted, tenantID, viewerID, map[string]any{
		"session_id":  sessionID,
		"viewer_id":   viewerID,
		"approver_id": approverID,
	})
}

// RecordPlaybackEnded fires when the SSE stream ends (completed or client disconnect).
func (r *Recorder) RecordPlaybackEnded(ctx context.Context, tenantID, sessionID, viewerID, reason string) {
	r.send(EventPlaybackEnded, tenantID, viewerID, map[string]any{
		"session_id": sessionID,
		"viewer_id":  viewerID,
		"reason":     reason,
	})
}

// RecordExport fires when a DPO exports a chain-of-custody package.
func (r *Recorder) RecordExport(ctx context.Context, tenantID, sessionID, exporterID, packageSHA256 string) {
	r.send(EventRecordingExported, tenantID, exporterID, map[string]any{
		"session_id":           sessionID,
		"exporter_id":          exporterID,
		"export_package_sha256": packageSHA256,
	})
}

// RecordDestruction fires when TTL or manual deletion destroys a recording.
func (r *Recorder) RecordDestruction(ctx context.Context, tenantID, sessionID string, lvmkVersion int, reason string) {
	r.send(EventRecordingDestroyed, tenantID, "livrec-service", map[string]any{
		"session_id":   sessionID,
		"lvmk_version": lvmkVersion,
		"reason":       reason,
	})
}
