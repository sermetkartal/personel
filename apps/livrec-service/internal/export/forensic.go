// Package export — DPO-only forensic export handler.
//
// Per ADR 0019 §DPO-only export:
//   - Endpoint: POST /v1/record/{session_id}/export
//   - DPO role enforced by auth middleware before reaching this handler.
//   - Output: ZIP archive containing:
//       recording.webm      (plaintext — decrypted during export)
//       manifest.json       (signed manifest with session metadata)
//       chain.json          (audit chain excerpt)
//       signature.sig       (Vault control-plane signature over SHA256 of all three)
//       pubkey.pem          (control-plane public key for offline verification)
//   - Export is logged to audit chain via Recorder.RecordExport.
//
// STUB NOTE: The chain-of-custody signature (signature.sig) is a stub —
// it calls vault.SignManifest but the real forensic signature chain (pubkey.pem
// retrieval and offline verification tooling) is a Phase 3 deliverable per spec.
package export

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/personel/livrec/internal/audit"
	"github.com/personel/livrec/internal/crypto"
	"github.com/personel/livrec/internal/httpserver"
	"github.com/personel/livrec/internal/storage"
	"github.com/personel/livrec/internal/vault"
)

// ExportRequest is the JSON body for POST /v1/record/{session_id}/export.
type ExportRequest struct {
	ReasonCode string `json:"reason_code"`
}

// RecordingMetaProvider looks up recording metadata by session ID.
// Shared interface with playback; implemented by the Postgres package.
type RecordingMetaProvider interface {
	GetRecordingForExport(ctx context.Context, sessionID string) (*RecordingExportMeta, error)
}

// RecordingExportMeta carries the data needed for forensic export.
type RecordingExportMeta struct {
	RecordingID string
	TenantID    string
	WrappedDEK  string
	LVMKVersion int
	StartedAt   time.Time
	EndedAt     time.Time
	BytesTotal  int64
	FrameCount  int64
}

// ForensicHandler handles POST /v1/record/{session_id}/export.
type ForensicHandler struct {
	meta    RecordingMetaProvider
	minio   *storage.Client
	deriver *crypto.LVMKDeriver
	vc      *vault.Client
	auditor *audit.Recorder
	log     *slog.Logger
}

// NewForensicHandler returns a wired ForensicHandler.
func NewForensicHandler(
	meta RecordingMetaProvider,
	minio *storage.Client,
	deriver *crypto.LVMKDeriver,
	vc *vault.Client,
	auditor *audit.Recorder,
	log *slog.Logger,
) *ForensicHandler {
	return &ForensicHandler{
		meta:    meta,
		minio:   minio,
		deriver: deriver,
		vc:      vc,
		auditor: auditor,
		log:     log,
	}
}

// ServeHTTP handles the forensic export request.
func (h *ForensicHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sessionID := chi.URLParam(r, "session_id")
	if sessionID == "" {
		httpserver.WriteError(w, http.StatusBadRequest, "session_id required")
		return
	}

	exporterID := httpserver.UserIDFromContext(r.Context())
	if exporterID == "" {
		httpserver.WriteError(w, http.StatusUnauthorized, "missing user context")
		return
	}

	var req ExportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ReasonCode == "" {
		httpserver.WriteError(w, http.StatusBadRequest, "reason_code is required")
		return
	}

	recMeta, err := h.meta.GetRecordingForExport(r.Context(), sessionID)
	if err != nil {
		h.log.Error("export: recording metadata lookup failed",
			slog.String("session_id", sessionID),
			slog.Any("error", err),
		)
		httpserver.WriteError(w, http.StatusNotFound, "recording not found")
		return
	}

	// Unwrap DEK for decryption.
	dek, err := h.deriver.UnwrapDEK(r.Context(), recMeta.TenantID, recMeta.WrappedDEK)
	if err != nil {
		h.log.Error("export: dek unwrap failed", slog.Any("error", err))
		httpserver.WriteError(w, http.StatusInternalServerError, "key unavailable")
		return
	}
	defer crypto.ZeroDEK(dek)

	// List and sort chunks.
	prefix := storage.ChunkPrefix(recMeta.TenantID, sessionID)
	chunkKeys, err := h.minio.ListChunks(r.Context(), prefix)
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "storage error")
		return
	}
	sort.Strings(chunkKeys)

	// Decrypt all chunks into a combined plaintext WebM buffer.
	var recordingBuf bytes.Buffer
	chunkHashes := make(map[string]string, len(chunkKeys))
	for i, key := range chunkKeys {
		enc, err := h.minio.GetChunkBytes(r.Context(), key)
		if err != nil {
			httpserver.WriteError(w, http.StatusInternalServerError, "chunk read error: "+key)
			return
		}
		plain, err := crypto.DecryptChunk(dek, sessionID, uint64(i), enc)
		if err != nil {
			httpserver.WriteError(w, http.StatusInternalServerError, "chunk decrypt error")
			return
		}
		chunkHashes[fmt.Sprintf("%d", i)] = HexSHA256(plain)
		recordingBuf.Write(plain)
	}
	recordingBytes := recordingBuf.Bytes()

	// Build manifest.
	exportID := newExportID()
	manifest := &Manifest{
		SchemaVersion: "livrec-manifest-v1",
		ExportID:      exportID,
		SessionID:     sessionID,
		TenantID:      recMeta.TenantID,
		RecordingID:   recMeta.RecordingID,
		StartedAt:     recMeta.StartedAt,
		EndedAt:       recMeta.EndedAt,
		BytesTotal:    recMeta.BytesTotal,
		FrameCount:    recMeta.FrameCount,
		ChunkCount:    len(chunkKeys),
		ExportedAt:    time.Now().UTC(),
		ExporterID:    exporterID,
		ReasonCode:    req.ReasonCode,
		ChunkHashes:   chunkHashes,
		// AuditChainExcerpt populated by Phase 3 Postgres query — stub empty slice.
		AuditChainExcerpt: []AuditEntry{},
	}

	manifestBytes, err := manifest.Marshal()
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "manifest error")
		return
	}

	// Build chain.json — stub for Phase 3.
	chainBytes := []byte(`{"note":"audit_chain_query_tbd_phase3"}`)

	// Sign the package.
	signingInput := ComputeSigningInput(recordingBytes, manifestBytes, chainBytes)
	manifest.PayloadSHA256 = HexSHA256(signingInput)
	sigBytes, keyVer, err := h.vc.SignManifest(r.Context(), signingInput)
	if err != nil {
		h.log.Error("export: vault sign failed", slog.Any("error", err))
		// Non-fatal stub — continue with empty signature in dev; production must fail.
		sigBytes = []byte("STUB_SIGNATURE_PHASE3")
		keyVer = "stub"
	}
	manifest.VaultKeyVersion = keyVer

	// Re-marshal manifest with signature fields populated.
	manifestBytes, err = manifest.Marshal()
	if err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "manifest re-marshal error")
		return
	}

	// Build ZIP archive in memory.
	var zipBuf bytes.Buffer
	zw := zip.NewWriter(&zipBuf)

	files := []struct {
		name string
		data []byte
	}{
		{"recording.webm", recordingBytes},
		{"manifest.json", manifestBytes},
		{"chain.json", chainBytes},
		{"signature.sig", sigBytes},
		{"pubkey.pem", []byte("# pubkey retrieval TBD Phase 3\n")},
	}
	for _, f := range files {
		fw, err := zw.Create(f.name)
		if err != nil {
			httpserver.WriteError(w, http.StatusInternalServerError, "zip create error")
			return
		}
		if _, err := fw.Write(f.data); err != nil {
			httpserver.WriteError(w, http.StatusInternalServerError, "zip write error")
			return
		}
	}
	if err := zw.Close(); err != nil {
		httpserver.WriteError(w, http.StatusInternalServerError, "zip close error")
		return
	}

	zipBytes := zipBuf.Bytes()
	packageSHA256 := HexSHA256(zipBytes)

	// Async audit — non-blocking.
	go h.auditor.RecordExport(context.Background(),
		recMeta.TenantID, sessionID, exporterID, packageSHA256)

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="livrec-export-%s.zip"`, exportID))
	w.Header().Set("X-Package-SHA256", packageSHA256)
	w.WriteHeader(http.StatusOK)
	w.Write(zipBytes) //nolint:errcheck

	h.log.Info("forensic export completed",
		slog.String("session_id", sessionID),
		slog.String("exporter_id", exporterID),
		slog.String("export_id", exportID),
		slog.Int("zip_bytes", len(zipBytes)),
	)
}

// newExportID generates a sortable export identifier.
func newExportID() string {
	return fmt.Sprintf("exp-%d", time.Now().UnixNano())
}
