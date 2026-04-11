// crypto.go produces encrypted keystroke and clipboard blobs that match the
// format the real Rust agent would produce (AES-256-GCM with PE-DEK).
//
// This module intentionally does NOT implement the full key hierarchy from
// key-hierarchy.md — for load testing we use a test PE-DEK directly. The
// red-team test in test/security/ exercises the key hierarchy from the outside.
package simulator

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
)

// TestKeyVersion constants mirror what the key-hierarchy.md describes.
// In production these come from Vault; in tests we use static values.
const (
	DefaultPEDEKVersion uint32 = 1
	DefaultTMKVersion   uint32 = 1
)

// TestPEDEK is a 32-byte AES-256 key used exclusively in test scenarios.
// It is derived deterministically from the endpoint ID so scenarios are
// reproducible. This is never a secret — it only encrypts synthetic data.
type TestPEDEK struct {
	key        [32]byte
	version    uint32
	tmkVersion uint32
}

// NewTestPEDEK derives a deterministic 32-byte test PE-DEK from the endpoint
// ID and version. Uses SHA-256(endpointID || version || "personel-test-dek")
// so different endpoints and versions always produce distinct keys.
func NewTestPEDEK(endpointID string, version, tmkVersion uint32) *TestPEDEK {
	h := sha256.New()
	h.Write([]byte(endpointID))
	versionBytes := make([]byte, 8)
	binary.BigEndian.PutUint32(versionBytes[:4], version)
	binary.BigEndian.PutUint32(versionBytes[4:], tmkVersion)
	h.Write(versionBytes)
	h.Write([]byte("personel-test-dek-v1"))

	dek := &TestPEDEK{version: version, tmkVersion: tmkVersion}
	copy(dek.key[:], h.Sum(nil))
	return dek
}

// EncryptedBlob is the result of encrypting a plaintext buffer, matching the
// format described in key-hierarchy.md §Keystroke Encryption at the Endpoint:
//
//	ciphertext = AES-256-GCM(key=PE-DEK, nonce=random96, aad=endpoint_id||seq, plaintext)
type EncryptedBlob struct {
	Ciphertext []byte
	Nonce      []byte // 12-byte GCM nonce
	AAD        []byte // endpoint_id || seq (big-endian uint64)
	KeyVersion uint32
	TMKVersion uint32
	ByteLen    uint32
}

// EncryptKeystrokeContent encrypts a synthetic keystroke buffer for the given
// endpoint and sequence number. The AAD matches the production format.
func (dek *TestPEDEK) EncryptKeystrokeContent(endpointID string, seq uint64, plaintext []byte) (*EncryptedBlob, error) {
	return dek.encryptBlob(endpointID, seq, plaintext)
}

// EncryptClipboardContent encrypts a synthetic clipboard blob.
func (dek *TestPEDEK) EncryptClipboardContent(endpointID string, seq uint64, plaintext []byte) (*EncryptedBlob, error) {
	return dek.encryptBlob(endpointID, seq, plaintext)
}

func (dek *TestPEDEK) encryptBlob(endpointID string, seq uint64, plaintext []byte) (*EncryptedBlob, error) {
	block, err := aes.NewCipher(dek.key[:])
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize()) // 12 bytes
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}

	// AAD = endpoint_id (as UTF-8 bytes) || seq (big-endian uint64)
	// This matches the production format described in key-hierarchy.md.
	aad := buildAAD(endpointID, seq)

	ciphertext := gcm.Seal(nil, nonce, plaintext, aad)

	return &EncryptedBlob{
		Ciphertext: ciphertext,
		Nonce:      nonce,
		AAD:        aad,
		KeyVersion: dek.version,
		TMKVersion: dek.tmkVersion,
		ByteLen:    uint32(len(ciphertext)),
	}, nil
}

// buildAAD constructs the additional authenticated data: endpoint_id || seq.
func buildAAD(endpointID string, seq uint64) []byte {
	seqBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(seqBytes, seq)
	aad := make([]byte, 0, len(endpointID)+8)
	aad = append(aad, []byte(endpointID)...)
	aad = append(aad, seqBytes...)
	return aad
}

// Version returns the PE-DEK version.
func (dek *TestPEDEK) Version() uint32 { return dek.version }

// TMKVersion returns the TMK version under which this DEK was wrapped.
func (dek *TestPEDEK) TMKVersion() uint32 { return dek.tmkVersion }

// SyntheticKeystrokeBuffer generates realistic synthetic keystroke plaintext
// of approximately the given byte count. The content is English-like
// typing patterns (letters, spaces, punctuation) representative of business
// document editing, not truly random bytes, which tests DLP pattern matching
// more realistically.
func SyntheticKeystrokeBuffer(approxBytes int) []byte {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 ,. \n\t"
	buf := make([]byte, approxBytes)
	// Use deterministic fill based on pattern — not crypto-random — because
	// we want reproducible test blobs when seeded.
	for i := range buf {
		buf[i] = chars[i%len(chars)]
	}
	return buf
}

// SyntheticKeystrokeBufferWithTCKN generates a keystroke buffer that contains
// a Turkish National ID (TCKN) pattern. Used to test that DLP does NOT leak
// this content through admin APIs (the red-team test relies on this).
func SyntheticKeystrokeBufferWithTCKN() []byte {
	// 12345678901 is a structurally valid TCKN format (11 digits).
	// In production, the DLP service would detect this as PII.
	return []byte("Dear HR,\n\nMy TC Kimlik No is: 12345678901\nPlease update your records.\n\nRegards")
}
