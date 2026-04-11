// Package export — signed manifest format for chain-of-custody packages.
//
// Per ADR 0019 §DPO-only export: the ZIP package contains manifest.json with:
//   session_id, tenant_id, recording_id, started_at, ended_at,
//   session_participants, recording_audit_chain_excerpt
//
// The manifest is signed by the control-plane Vault signing key.
// signature.sig = sign(SHA256(recording.webm || manifest.json || chain.json))
package export

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// Manifest is the structured content of manifest.json in the export ZIP.
type Manifest struct {
	SchemaVersion string `json:"schema_version"` // "livrec-manifest-v1"
	ExportID      string `json:"export_id"`
	SessionID     string `json:"session_id"`
	TenantID      string `json:"tenant_id"`
	RecordingID   string `json:"recording_id"`
	StartedAt     time.Time `json:"started_at"`
	EndedAt       time.Time `json:"ended_at"`
	BytesTotal    int64  `json:"bytes_total"`
	FrameCount    int64  `json:"frame_count,omitempty"`
	ChunkCount    int    `json:"chunk_count"`
	ExportedAt    time.Time `json:"exported_at"`
	ExporterID    string `json:"exporter_id"` // DPO user ID
	ReasonCode    string `json:"reason_code"`
	// ChunkHashes maps chunk_index (as string) to hex-encoded SHA-256.
	ChunkHashes map[string]string `json:"chunk_hashes"`
	// AuditChainExcerpt is a human-readable summary of the relevant audit chain
	// entries (recording_started → recording_ended → export).
	AuditChainExcerpt []AuditEntry `json:"audit_chain_excerpt"`
	// Signature fields — populated after Vault signing.
	PayloadSHA256    string `json:"payload_sha256"`    // hex SHA-256 of signing input
	VaultKeyVersion  string `json:"vault_key_version"` // e.g. "control-plane-signer:v1"
}

// AuditEntry is a condensed audit entry for the manifest chain excerpt.
type AuditEntry struct {
	EventType  string    `json:"event_type"`
	ActorID    string    `json:"actor_id,omitempty"`
	OccurredAt time.Time `json:"occurred_at"`
	Hash       string    `json:"hash,omitempty"` // this_hash from audit_records
}

// Marshal serialises the manifest to canonical JSON.
// The canonical form is used as signing input and for deterministic hashing.
func (m *Manifest) Marshal() ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// ComputeSigningInput builds the byte sequence signed by the Vault control-plane
// key: SHA256(recordingBytes || manifestBytes || chainBytes).
func ComputeSigningInput(recordingBytes, manifestBytes, chainBytes []byte) []byte {
	h := sha256.New()
	h.Write(recordingBytes)
	h.Write(manifestBytes)
	h.Write(chainBytes)
	return h.Sum(nil)
}

// HexSHA256 returns the hex-encoded SHA-256 of data.
func HexSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
