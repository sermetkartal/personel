// Package playback — HTTP handler for GET /v1/record/{session_id}/stream.
//
// Implements SSE streaming with:
//   event: dek     — one-time, first event; base64-encoded wrapped session DEK
//   event: chunk   — repeated; one per chunk; data carries chunk_index + base64 ciphertext
//   event: end     — stream complete
//
// Per ADR 0019 quality bar:
//   - SSE stream is properly flushed (explicit w.(http.Flusher).Flush() after each event)
//   - Dual HR approval verified before any data is sent
//   - No download path; browser must decrypt in WebCrypto
//   - Audit entries written async (non-blocking)
package playback

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/personel/livrec/internal/audit"
	"github.com/personel/livrec/internal/httpserver"
	"github.com/personel/livrec/internal/storage"
)

// RecordingMeta is a minimal struct passed to the handler from a Postgres lookup
// (stub here — real lookup is wired in main.go via a closure or interface).
type RecordingMeta struct {
	TenantID    string
	WrappedDEK  string
	LVMKVersion int
	ChunkCount  int64
}

// RecordingStore is the interface livrec-service uses to look up recording
// metadata. Implemented against Postgres in production; stub in tests.
type RecordingStore interface {
	GetRecordingMeta(ctx context.Context, sessionID string) (*RecordingMeta, error)
}

// StreamHandler handles GET /v1/record/{session_id}/stream.
type StreamHandler struct {
	gate        *ApprovalGate
	dekDelivery *DEKDelivery
	minio       *storage.Client
	store       RecordingStore
	auditor     *audit.Recorder
	log         *slog.Logger
}

// NewStreamHandler returns a wired StreamHandler.
func NewStreamHandler(
	gate *ApprovalGate,
	dekDelivery *DEKDelivery,
	minio *storage.Client,
	store RecordingStore,
	auditor *audit.Recorder,
	log *slog.Logger,
) *StreamHandler {
	return &StreamHandler{
		gate:        gate,
		dekDelivery: dekDelivery,
		minio:       minio,
		store:       store,
		auditor:     auditor,
		log:         log,
	}
}

// ServeHTTP handles the SSE playback stream.
//
//	GET /v1/record/{session_id}/stream
//	Accept: text/event-stream
func (h *StreamHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		httpserver.WriteError(w, http.StatusBadRequest, "session_id required")
		return
	}

	// Require Accept: text/event-stream.
	if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		httpserver.WriteError(w, http.StatusNotAcceptable, "Accept: text/event-stream required")
		return
	}

	requesterID := httpserver.UserIDFromContext(r.Context())
	if requesterID == "" {
		httpserver.WriteError(w, http.StatusUnauthorized, "missing user context")
		return
	}
	tenantID := httpserver.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpserver.WriteError(w, http.StatusUnauthorized, "missing tenant context")
		return
	}

	// Dual-control gate — fail-closed.
	approval, err := h.gate.CheckPlaybackApproval(r.Context(), sessionID, requesterID)
	if err != nil {
		h.log.Warn("playback approval check failed",
			slog.String("session_id", sessionID),
			slog.String("requester_id", requesterID),
			slog.Any("error", err),
		)
		httpserver.WriteError(w, http.StatusForbidden, "dual-control approval required: "+err.Error())
		return
	}

	// Look up recording metadata from Postgres.
	recMeta, err := h.store.GetRecordingMeta(r.Context(), sessionID)
	if err != nil || recMeta == nil {
		h.log.Error("recording metadata lookup failed",
			slog.String("session_id", sessionID),
			slog.Any("error", err),
		)
		httpserver.WriteError(w, http.StatusNotFound, "recording not found")
		return
	}
	meta := recMeta

	// Unwrap session DEK for delivery.
	dekBase64, err := h.dekDelivery.UnwrapForPlayback(r.Context(), meta.TenantID, meta.WrappedDEK)
	if err != nil {
		h.log.Error("dek unwrap failed",
			slog.String("session_id", sessionID),
			slog.Any("error", err),
		)
		httpserver.WriteError(w, http.StatusInternalServerError, "key unavailable")
		return
	}

	// All checks passed — establish SSE stream.
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpserver.WriteError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering
	w.WriteHeader(http.StatusOK)

	// Async audit — non-blocking.
	go h.auditor.RecordPlaybackStarted(context.Background(),
		meta.TenantID, sessionID, requesterID, approval.ApproverID)

	// Event 1: dek — one-time DEK delivery.
	writeSSEEvent(w, "dek", dekBase64)
	flusher.Flush()

	// Stream chunks from MinIO.
	prefix := storage.ChunkPrefix(meta.TenantID, sessionID)
	chunkKeys, err := h.minio.ListChunks(r.Context(), prefix)
	if err != nil {
		writeSSEEvent(w, "error", "failed to list chunks")
		flusher.Flush()
		return
	}
	// Sort to guarantee chunk order in the stream.
	sort.Strings(chunkKeys)

	for _, key := range chunkKeys {
		select {
		case <-r.Context().Done():
			// Client disconnected.
			h.log.Debug("playback stream: client disconnected",
				slog.String("session_id", sessionID))
			go h.auditor.RecordPlaybackEnded(context.Background(),
				meta.TenantID, sessionID, requesterID, "client_disconnect")
			return
		default:
		}

		chunkData, err := h.minio.GetChunkBytes(r.Context(), key)
		if err != nil {
			h.log.Error("minio get chunk failed during playback",
				slog.String("key", key),
				slog.Any("error", err),
			)
			writeSSEEvent(w, "error", "chunk unavailable: "+key)
			flusher.Flush()
			return
		}

		chunkIndex := extractChunkIndex(key)
		payload := fmt.Sprintf("%d %s", chunkIndex, base64.StdEncoding.EncodeToString(chunkData))
		writeSSEEvent(w, "chunk", payload)
		flusher.Flush() // Per ADR 0019 quality bar: flush after each chunk.
	}

	// Event: end — all chunks delivered.
	writeSSEEvent(w, "end", strconv.FormatInt(int64(len(chunkKeys)), 10))
	flusher.Flush()

	go h.auditor.RecordPlaybackEnded(context.Background(),
		meta.TenantID, sessionID, requesterID, "completed")

	h.log.Info("playback stream completed",
		slog.String("session_id", sessionID),
		slog.Int("chunks", len(chunkKeys)),
	)
}

// writeSSEEvent writes a single SSE event to w.
// Format:
//
//	event: <eventType>\n
//	data: <data>\n
//	\n
func writeSSEEvent(w http.ResponseWriter, eventType, data string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, data)
}

// extractChunkIndex parses the chunk index from an object key such as
// "tenant/tid/session/sid/chunk-42.webm.enc" → 42.
// Returns 0 on parse failure (safe because chunks are already sorted by key).
func extractChunkIndex(key string) uint64 {
	// Key ends with chunk-{N}.webm.enc
	base := key
	if idx := strings.LastIndex(base, "chunk-"); idx >= 0 {
		base = base[idx+6:]
	}
	base = strings.TrimSuffix(base, ".webm.enc")
	n, _ := strconv.ParseUint(base, 10, 64)
	return n
}
