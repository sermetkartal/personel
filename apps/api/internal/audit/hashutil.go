package audit

import "crypto/sha256"

// hashSHA256 is isolated so both canonical.go and verifier.go
// reference the same underlying SHA-256 implementation.
func hashSHA256(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}
