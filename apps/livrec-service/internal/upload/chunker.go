// Package upload — chunk validation and ordering logic.
//
// Per ADR 0019:
//   - Chunks are WebM segments encrypted with AES-256-GCM.
//   - Chunk index must be strictly monotonic (no gaps, no reorder).
//   - Maximum chunk size: 1 MiB (1,048,576 bytes) per ADR 0019 §Storage.
//   - X-Chunk-Hash header carries the SHA-256 of the plaintext (before
//     browser-side encryption); used for integrity verification.
package upload

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
)

const (
	// MaxChunkSize is the maximum permissible chunk payload size in bytes.
	// ADR 0019 §Storage: fixed-size 1 MiB chunks.
	MaxChunkSize = 1 << 20 // 1 MiB

	// MaxChunkIndex is a sanity cap on chunk index to prevent integer
	// overflow in edge cases (a 30-day recording at 10-second segments
	// produces at most ~259,200 chunks).
	MaxChunkIndex = 1_000_000
)

// ChunkMeta carries validated metadata extracted from request headers.
type ChunkMeta struct {
	SessionID  string
	ChunkIndex uint64
	ChunkHash  []byte // raw SHA-256 bytes from X-Chunk-Hash (hex-decoded)
}

// ParseChunkHeaders parses and validates the chunk metadata headers.
// Returns ChunkMeta or an error describing the first invalid field.
func ParseChunkHeaders(sessionID, chunkIndexStr, chunkHashHex string) (*ChunkMeta, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("chunker: X-Session-ID is required")
	}
	if chunkIndexStr == "" {
		return nil, fmt.Errorf("chunker: X-Chunk-Index is required")
	}

	idx, err := strconv.ParseUint(chunkIndexStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("chunker: X-Chunk-Index must be uint64: %w", err)
	}
	if idx > MaxChunkIndex {
		return nil, fmt.Errorf("chunker: X-Chunk-Index %d exceeds maximum %d", idx, MaxChunkIndex)
	}

	if chunkHashHex == "" {
		return nil, fmt.Errorf("chunker: X-Chunk-Hash is required")
	}
	hashBytes, err := hex.DecodeString(chunkHashHex)
	if err != nil {
		return nil, fmt.Errorf("chunker: X-Chunk-Hash must be hex-encoded SHA-256: %w", err)
	}
	if len(hashBytes) != sha256.Size {
		return nil, fmt.Errorf("chunker: X-Chunk-Hash must be 32 bytes, got %d", len(hashBytes))
	}

	return &ChunkMeta{
		SessionID:  sessionID,
		ChunkIndex: idx,
		ChunkHash:  hashBytes,
	}, nil
}

// ValidateChunkSize returns an error if the chunk payload exceeds MaxChunkSize.
func ValidateChunkSize(size int) error {
	if size == 0 {
		return fmt.Errorf("chunker: empty chunk body")
	}
	if size > MaxChunkSize {
		return fmt.Errorf("chunker: chunk size %d exceeds maximum %d bytes (1 MiB)", size, MaxChunkSize)
	}
	return nil
}
