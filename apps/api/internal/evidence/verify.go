package evidence

import (
	"context"
	"fmt"
)

// Verifier is the narrow read-side contract for signature verification.
// The implementation (typically vault.Client) must be able to verify any
// historical signature against the control-plane signing key, including
// versions that have since been rotated. This is essential for the
// 5-year WORM retention path: an evidence item signed with v1 of the
// key must remain verifiable after the operator rotates to v2, v3, etc.
//
// Vault transit's verify endpoint does this natively — the key history
// is preserved, so Verify() succeeds against any past version as long
// as the min_decryption_version allows it. Operators must NOT set
// min_decryption_version to skip past any version that might still
// have live evidence items or the SOC 2 chain breaks.
type Verifier interface {
	// Verify returns nil if the signature is valid for the payload under
	// the key identified by keyVersion. keyVersion is the same string
	// the Signer returned at Sign time (e.g. "control-plane-signing:v1").
	Verify(ctx context.Context, payload []byte, signature []byte, keyVersion string) error
}

// VerifyItem recomputes the canonical form of an Item and asks the
// Verifier to check the stored signature. Used by auditor tooling,
// the DPO "pack integrity check" button, and the periodic WORM
// reconciliation job.
//
// Returns an error if the signature does not match OR if the item's
// canonical encoding has drifted since it was signed. Both cases are
// indistinguishable to the verifier — by design, since a canonical
// drift is a silent integrity failure.
func VerifyItem(ctx context.Context, v Verifier, item Item) error {
	if v == nil {
		return fmt.Errorf("evidence: Verifier is nil")
	}
	if len(item.Signature) == 0 {
		return fmt.Errorf("evidence: item %q has no signature", item.ID)
	}
	if item.SignatureKeyVersion == "" {
		return fmt.Errorf("evidence: item %q has no signature_key_version", item.ID)
	}

	canonical := canonicalize(item)
	return v.Verify(ctx, canonical, item.Signature, item.SignatureKeyVersion)
}
