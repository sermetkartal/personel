// Package upload — HTTP handler for POST /v1/record/chunk.
//
// This handler receives encrypted WebM chunks from the LiveKit egress shim
// (or directly from a browser MediaRecorder in later phases) and stores them
// in MinIO under the tenant-scoped path defined in storage/layout.go.
//
// Per ADR 0019 quality bar:
//   - Chunk-index monotonicity enforced (reject out-of-order)
//   - Audit forwarder is ASYNC and non-blocking (audit failures do not break recording)
//   - Chunk is already encrypted by the time it arrives (browser-side AES-GCM)
package upload

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync"

	"github.com/personel/livrec/internal/audit"
	"github.com/personel/livrec/internal/crypto"
	"github.com/personel/livrec/internal/httpserver"
	"github.com/personel/livrec/internal/storage"
)

// SessionStore is a concurrency-safe in-memory map of active recording sessions.
// In production this is keyed by session ID. Sessions are initialised on first
// chunk arrival if not pre-registered by a session-start event.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*RecordingSession
}

// NewSessionStore returns an empty SessionStore.
func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]*RecordingSession)}
}

// Get returns a session by ID. Returns nil if not found.
func (ss *SessionStore) Get(id string) *RecordingSession {
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	return ss.sessions[id]
}

// GetOrCreate returns an existing session or creates a new one in StateWaiting.
// wrappedDEK and lvmkVersion are used only when creating a new session.
func (ss *SessionStore) GetOrCreate(sessionID, tenantID, wrappedDEK string, lvmkVersion int) *RecordingSession {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if s, ok := ss.sessions[sessionID]; ok {
		return s
	}
	s := NewSession(sessionID, tenantID, wrappedDEK, lvmkVersion)
	ss.sessions[sessionID] = s
	return s
}

// Remove deletes a session from the store (called on completion or error).
func (ss *SessionStore) Remove(id string) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	delete(ss.sessions, id)
}

// ChunkHandler handles POST /v1/record/chunk.
type ChunkHandler struct {
	store   *SessionStore
	minio   *storage.Client
	deriver *crypto.LVMKDeriver
	auditor *audit.Recorder
	log     *slog.Logger
}

// NewChunkHandler returns a ChunkHandler wired to the given dependencies.
func NewChunkHandler(
	store *SessionStore,
	minio *storage.Client,
	deriver *crypto.LVMKDeriver,
	auditor *audit.Recorder,
	log *slog.Logger,
) *ChunkHandler {
	return &ChunkHandler{
		store:   store,
		minio:   minio,
		deriver: deriver,
		auditor: auditor,
		log:     log,
	}
}

// ServeHTTP handles the chunk upload request.
//
//	POST /v1/record/chunk
//	Content-Type: application/octet-stream
//	X-Session-ID: {session_id}
//	X-Chunk-Index: {N}
//	X-Chunk-Hash: {sha256-hex of plaintext}
//	Body: AES-256-GCM ciphertext (already encrypted by sender)
func (h *ChunkHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	meta, err := ParseChunkHeaders(
		r.Header.Get("X-Session-ID"),
		r.Header.Get("X-Chunk-Index"),
		r.Header.Get("X-Chunk-Hash"),
	)
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Read body with a size cap to enforce the 1 MiB chunk limit.
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxChunkSize+1))
	if err != nil {
		httpserver.WriteError(w, http.StatusBadRequest, "failed to read chunk body")
		return
	}
	if err := ValidateChunkSize(len(body)); err != nil {
		httpserver.WriteError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}

	// Derive tenant from authentication context (populated by auth middleware).
	tenantID := httpserver.TenantIDFromContext(r.Context())
	if tenantID == "" {
		httpserver.WriteError(w, http.StatusUnauthorized, "missing tenant context")
		return
	}

	// Retrieve or create the session. For first-chunk sessions the DEK wrap
	// is expected to be pre-registered via the session start API. If not present,
	// we bootstrap a new DEK here.
	sess := h.store.Get(meta.SessionID)
	if sess == nil {
		sess = h.bootstrapSession(r.Context(), meta.SessionID, tenantID)
		if sess == nil {
			httpserver.WriteError(w, http.StatusInternalServerError, "failed to initialise session")
			return
		}
	}

	// Enforce monotonic chunk ordering.
	if err := sess.AcceptChunk(meta.ChunkIndex, len(body)); err != nil {
		h.log.Warn("chunk rejected",
			slog.String("session_id", meta.SessionID),
			slog.Uint64("chunk_index", meta.ChunkIndex),
			slog.Any("error", err),
		)
		httpserver.WriteError(w, http.StatusConflict, err.Error())
		return
	}

	// Build the MinIO object key and store the already-encrypted chunk.
	objectKey := storage.ChunkObjectKey(tenantID, meta.SessionID, meta.ChunkIndex)
	if err := h.minio.PutChunk(r.Context(), objectKey, body); err != nil {
		sess.Fail("minio put failed: " + err.Error())
		h.log.Error("minio put chunk failed",
			slog.String("session_id", meta.SessionID),
			slog.Uint64("chunk_index", meta.ChunkIndex),
			slog.Any("error", err),
		)
		httpserver.WriteError(w, http.StatusInternalServerError, "storage error")
		return
	}

	// Async audit — non-blocking; a failure here MUST NOT break recording.
	go h.auditor.RecordChunkStored(context.Background(), tenantID, meta.SessionID, meta.ChunkIndex, int64(len(body)))

	h.log.Debug("chunk stored",
		slog.String("session_id", meta.SessionID),
		slog.Uint64("chunk_index", meta.ChunkIndex),
		slog.Int("bytes", len(body)),
	)
	w.WriteHeader(http.StatusNoContent)
}

// bootstrapSession derives a new session DEK, wraps it, and registers the
// session in the store. Returns nil and logs on failure.
func (h *ChunkHandler) bootstrapSession(ctx context.Context, sessionID, tenantID string) *RecordingSession {
	dek, version, err := h.deriver.DeriveSessionDEK(ctx, tenantID, sessionID)
	if err != nil {
		h.log.Error("lvmk derive failed during session bootstrap",
			slog.String("session_id", sessionID),
			slog.Any("error", err),
		)
		return nil
	}
	defer crypto.ZeroDEK(dek)

	wrappedDEK, err := h.deriver.WrapDEK(ctx, tenantID, dek)
	if err != nil {
		h.log.Error("dek wrap failed during session bootstrap",
			slog.String("session_id", sessionID),
			slog.Any("error", err),
		)
		return nil
	}

	return h.store.GetOrCreate(sessionID, tenantID, wrappedDEK, version)
}
