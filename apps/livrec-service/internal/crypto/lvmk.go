// Package crypto provides the cryptographic primitives for livrec-service.
//
// LVMK (Live View Master Key) is a Vault transit key separate from TMK.
// Per ADR 0019 §Key hierarchy: zero cross-contamination — LVMK and TMK
// have independent Vault keys, independent AppRoles, and independent rotation
// schedules. No code in this package may reference TMK paths.
package crypto

import (
	"context"
	"fmt"

	"github.com/personel/livrec/internal/vault"
)

// LVMKDeriver performs per-session DEK derivation from the LVMK in Vault.
// It holds no key material itself; every derive call goes to Vault.
type LVMKDeriver struct {
	vc *vault.Client
}

// NewLVMKDeriver creates a deriver backed by the given Vault client.
func NewLVMKDeriver(vc *vault.Client) *LVMKDeriver {
	return &LVMKDeriver{vc: vc}
}

// DeriveSessionDEK derives a fresh 32-byte session DEK for recording.
// The derivation context is canonical per ADR 0019:
//
//	context = sessionID || tenantID || "lv-session-dek-v1"
//
// Returns (dekBytes, lvmkVersion, error). The returned key is NEVER written
// to disk; the caller must wrap it via WrapDEK and store only the wrap.
// After wrapping, the caller must zero the returned slice.
func (d *LVMKDeriver) DeriveSessionDEK(ctx context.Context, tenantID, sessionID string) ([]byte, int, error) {
	derivationCtx := sessionID + tenantID + "lv-session-dek-v1"
	dek, version, err := d.vc.DeriveLVMK(ctx, tenantID, derivationCtx)
	if err != nil {
		return nil, 0, fmt.Errorf("lvmk: derive session dek: %w", err)
	}
	return dek, version, nil
}

// WrapDEK wraps a plaintext DEK under the LVMK for storage.
// Returns the Vault ciphertext envelope string ("vault:v1:...").
func (d *LVMKDeriver) WrapDEK(ctx context.Context, tenantID string, dek []byte) (string, error) {
	return d.vc.WrapDEK(ctx, tenantID, dek)
}

// UnwrapDEK decrypts a stored DEK wrap back to 32 plaintext bytes.
// Used during playback after dual-control approval is verified.
// Caller MUST zero the returned bytes after use.
func (d *LVMKDeriver) UnwrapDEK(ctx context.Context, tenantID, wrappedDEK string) ([]byte, error) {
	dek, err := d.vc.UnwrapDEK(ctx, tenantID, wrappedDEK)
	if err != nil {
		return nil, fmt.Errorf("lvmk: unwrap dek: %w", err)
	}
	return dek, nil
}
