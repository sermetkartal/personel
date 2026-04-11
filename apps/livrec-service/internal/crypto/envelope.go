// Package crypto — AES-256-GCM chunk envelope.
//
// Per ADR 0019 §Storage:
//   - Each WebM chunk is encrypted with AES-256-GCM.
//   - Nonce (IV) is derived from HKDF(session_dek, chunk_index) to ensure
//     deterministic nonces that enable seek-without-full-decrypt on playback.
//   - The on-disk format: [12-byte nonce][16-byte GCM tag fused into ciphertext]
//     i.e. standard golang AES-GCM output from Seal, which appends the tag.
//   - AAD = "livrec-chunk:" || session_id || ":" || chunk_index_big_endian_uint64
//
// Thread-safe: Envelope has no mutable state; callers provide all inputs.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
)

// EncryptChunk encrypts plaintext with AES-256-GCM using a randomly generated
// 12-byte nonce. sessionID and chunkIndex are bound as AAD so a chunk cannot
// be transplanted into a different session or reordered without detection.
//
// Wire format written to out:
//
//	[12 bytes: nonce][ciphertext + 16-byte GCM tag]
//
// Per ADR 0019 quality bar: uses proper IV generation (crypto/rand).
func EncryptChunk(dek []byte, sessionID string, chunkIndex uint64, plaintext []byte) ([]byte, error) {
	if len(dek) != 32 {
		return nil, fmt.Errorf("envelope: dek must be 32 bytes, got %d", len(dek))
	}

	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("envelope: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("envelope: new gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("envelope: generate nonce: %w", err)
	}

	aad := buildAAD(sessionID, chunkIndex)
	ciphertext := gcm.Seal(nil, nonce, plaintext, aad)

	// Prepend nonce so DecryptChunk can extract it.
	out := make([]byte, len(nonce)+len(ciphertext))
	copy(out, nonce)
	copy(out[len(nonce):], ciphertext)
	return out, nil
}

// DecryptChunk decrypts a chunk produced by EncryptChunk.
// The wire format is [12-byte nonce][ciphertext+tag].
// sessionID and chunkIndex must match exactly what was used during encryption.
func DecryptChunk(dek []byte, sessionID string, chunkIndex uint64, data []byte) ([]byte, error) {
	if len(dek) != 32 {
		return nil, fmt.Errorf("envelope: dek must be 32 bytes, got %d", len(dek))
	}

	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("envelope: new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("envelope: new gcm: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize+gcm.Overhead() {
		return nil, fmt.Errorf("envelope: ciphertext too short")
	}

	nonce := data[:nonceSize]
	ciphertext := data[nonceSize:]
	aad := buildAAD(sessionID, chunkIndex)

	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("envelope: gcm open (auth failure or wrong key): %w", err)
	}
	return plaintext, nil
}

// buildAAD constructs the Additional Authenticated Data for a chunk.
// Format: "livrec-chunk:" + sessionID + ":" + big-endian uint64 chunk index.
func buildAAD(sessionID string, chunkIndex uint64) []byte {
	prefix := "livrec-chunk:" + sessionID + ":"
	aad := make([]byte, len(prefix)+8)
	copy(aad, prefix)
	binary.BigEndian.PutUint64(aad[len(prefix):], chunkIndex)
	return aad
}
