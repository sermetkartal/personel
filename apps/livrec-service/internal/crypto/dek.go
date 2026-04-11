// Package crypto — per-session DEK lifecycle helpers.
//
// Per ADR 0019: the plaintext DEK is never persisted to disk; only its Vault-
// wrapped form is stored in Postgres. In-memory lifetime is bounded to the
// recording session or the playback window (30-minute max per spec).
package crypto

import (
	"crypto/rand"
	"fmt"
)

// GenerateSessionDEK generates a cryptographically random 32-byte session key.
// This is used when live-view-recorder cannot reach Vault for derive (fallback
// not recommended for production; prefer DeriveSessionDEK which ties the key
// to LVMK and provides cryptographic shredding on key destruction).
//
// NOTE: In normal flow, use LVMKDeriver.DeriveSessionDEK instead.
// This function is provided for testing and for the LVMK-less bootstrap path
// only.
func GenerateSessionDEK() ([]byte, error) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("dek: generate: %w", err)
	}
	return key, nil
}

// ZeroDEK overwrites a DEK byte slice with zeros.
// Call this as soon as the DEK is no longer needed in memory.
// Does not prevent the GC from creating a copy; pair with mlockall in
// production for highest assurance.
func ZeroDEK(dek []byte) {
	for i := range dek {
		dek[i] = 0
	}
}
