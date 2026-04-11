// Package playback — per-session DEK delivery via SSE.
//
// Per ADR 0019 §Playback flow:
//   - The session DEK is delivered ONCE as the first SSE event (event: dek).
//   - The DEK is unwrapped from Vault only after dual-control approval is
//     confirmed by ApprovalGate.
//   - The DEK is transmitted as a base64-encoded string in the SSE data field.
//   - The DEK is NEVER persisted server-side after transmission; it is held
//     only briefly in handler memory while writing the SSE event.
//   - The browser receives the DEK, decrypts chunks in WebCrypto (in-memory),
//     and clears it on page unload. No disk write.
package playback

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"

	"github.com/personel/livrec/internal/crypto"
)

// DEKDelivery handles the one-time DEK unwrap and SSE write for a playback session.
type DEKDelivery struct {
	deriver *crypto.LVMKDeriver
	log     *slog.Logger
}

// NewDEKDelivery constructs a DEKDelivery backed by the given LVMK deriver.
func NewDEKDelivery(deriver *crypto.LVMKDeriver, log *slog.Logger) *DEKDelivery {
	return &DEKDelivery{deriver: deriver, log: log}
}

// UnwrapForPlayback unwraps the stored DEK wrap for the given tenant, returns
// the base64-encoded DEK string suitable for direct inclusion in SSE data field.
// The plaintext DEK is zeroed after encoding.
//
// This must be called only AFTER ApprovalGate.CheckPlaybackApproval succeeds.
func (d *DEKDelivery) UnwrapForPlayback(ctx context.Context, tenantID, wrappedDEK string) (string, error) {
	dek, err := d.deriver.UnwrapDEK(ctx, tenantID, wrappedDEK)
	if err != nil {
		return "", fmt.Errorf("dek_delivery: unwrap: %w", err)
	}
	defer crypto.ZeroDEK(dek)

	// Encode before zeroing the source slice.
	encoded := base64.StdEncoding.EncodeToString(dek)

	d.log.Debug("dek delivered for playback",
		slog.String("tenant_id", tenantID),
	)
	return encoded, nil
}
