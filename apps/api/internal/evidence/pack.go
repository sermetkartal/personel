package evidence

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// PackManifest describes the contents of an evidence pack. Serialised as
// the first file inside the ZIP (manifest.json) so an auditor can inspect
// the contents without any Personel-specific tooling. The manifest is
// itself signed by the control-plane key — see PackBuilder.Build.
type PackManifest struct {
	SchemaVersion    int           `json:"schema_version"`
	TenantID         string        `json:"tenant_id"`
	CollectionPeriod string        `json:"collection_period"`
	GeneratedAt      string        `json:"generated_at"`
	GeneratedBy      string        `json:"generated_by"`
	ItemCount        int           `json:"item_count"`
	ControlsCovered  []string      `json:"controls_covered"`
	Items            []ManifestRow `json:"items"`
}

// ManifestRow is one line in the manifest index — enough metadata for an
// auditor to decide whether to open the full item, plus the WORM bucket
// coordinates for independent verification.
type ManifestRow struct {
	ID                  string   `json:"id"`
	Control             string   `json:"control"`
	Kind                string   `json:"kind"`
	RecordedAt          string   `json:"recorded_at"`
	Actor               string   `json:"actor"`
	SummaryTR           string   `json:"summary_tr"`
	SummaryEN           string   `json:"summary_en"`
	ReferencedAuditIDs  []int64  `json:"referenced_audit_ids"`
	AttachmentRefs      []string `json:"attachment_refs"`
	SignatureKeyVersion string   `json:"signature_key_version"`
	WORMObjectKey       string   `json:"worm_object_key"`
}

// PackBuilder assembles a Type II evidence pack ZIP for a tenant + period.
// The output stream contains:
//
//	manifest.json           — PackManifest, signed at the end of the build
//	items/{id}.json         — each Item serialised; structured not canonical
//	items/{id}.signature    — raw signature bytes (Ed25519) for the item
//
// Canonical bytes themselves are NOT re-packed into the ZIP — auditors who
// want byte-for-byte integrity verification pull the canonical form from
// the audit-worm bucket directly using the WORMObjectKey in the manifest.
// This keeps the pack size down while preserving end-to-end verifiability.
type PackBuilder struct {
	store  *Store
	signer Signer
}

// NewPackBuilder creates a builder.
func NewPackBuilder(store *Store, signer Signer) *PackBuilder {
	return &PackBuilder{store: store, signer: signer}
}

// Build writes a complete evidence pack ZIP to w for the given tenant and
// period. Optional control filter limits the items included. The pack is
// streamed — no intermediate buffering of item JSON — so memory usage
// stays bounded regardless of item count.
//
// If no items match, the ZIP still contains a manifest with ItemCount=0.
// Returning an empty pack rather than an error lets the DPO dashboard
// present "no evidence for this period" as a deliberate result instead
// of a transient API failure.
func (b *PackBuilder) Build(
	ctx context.Context,
	w io.Writer,
	req PackRequest,
	generatedBy string,
) (*PackManifest, error) {
	if req.TenantID == "" {
		return nil, fmt.Errorf("evidence/pack: tenant_id is required")
	}

	period := req.PeriodStart.Format("2006-01")
	if req.PeriodStart.IsZero() {
		return nil, fmt.Errorf("evidence/pack: period_start is required")
	}

	items, err := b.store.ListByPeriod(ctx, req.TenantID, period, req.Controls)
	if err != nil {
		return nil, fmt.Errorf("evidence/pack: list: %w", err)
	}

	zw := zip.NewWriter(w)
	defer func() { _ = zw.Close() }()

	controlsSeen := make(map[string]struct{}, 8)
	manifest := &PackManifest{
		SchemaVersion:    1,
		TenantID:         req.TenantID,
		CollectionPeriod: period,
		GeneratedAt:      time.Now().UTC().Format(time.RFC3339Nano),
		GeneratedBy:      generatedBy,
		ItemCount:        len(items),
		Items:            make([]ManifestRow, 0, len(items)),
	}

	for _, it := range items {
		controlsSeen[string(it.Control)] = struct{}{}

		// Structured JSON item — convenient for auditor tooling. Note
		// this is NOT the canonical signed form; the canonical bytes
		// live in WORM and are addressable via worm_object_key in the
		// manifest row.
		itemFile, err := zw.Create(fmt.Sprintf("items/%s.json", it.ID))
		if err != nil {
			return nil, fmt.Errorf("evidence/pack: zip create item: %w", err)
		}
		if err := json.NewEncoder(itemFile).Encode(it); err != nil {
			return nil, fmt.Errorf("evidence/pack: encode item %s: %w", it.ID, err)
		}

		// Raw signature bytes alongside the item, so auditors can
		// verify without parsing or reconstructing hex/base64.
		sigFile, err := zw.Create(fmt.Sprintf("items/%s.signature", it.ID))
		if err != nil {
			return nil, fmt.Errorf("evidence/pack: zip create sig: %w", err)
		}
		if _, err := sigFile.Write(it.Signature); err != nil {
			return nil, fmt.Errorf("evidence/pack: write sig %s: %w", it.ID, err)
		}

		manifest.Items = append(manifest.Items, ManifestRow{
			ID:                  it.ID,
			Control:             string(it.Control),
			Kind:                string(it.Kind),
			RecordedAt:          it.RecordedAt.Format(time.RFC3339Nano),
			Actor:               it.Actor,
			SummaryTR:           it.SummaryTR,
			SummaryEN:           it.SummaryEN,
			ReferencedAuditIDs:  it.ReferencedAuditIDs,
			AttachmentRefs:      it.AttachmentRefs,
			SignatureKeyVersion: it.SignatureKeyVersion,
			// WORM object key is deterministic — same scheme as
			// audit.EvidenceObjectKey() but duplicated here to avoid
			// the import cycle (audit imports evidence would break).
			WORMObjectKey: fmt.Sprintf("evidence/%s/%s/%s.bin", it.TenantID, it.CollectionPeriod, it.ID),
		})
	}

	for c := range controlsSeen {
		manifest.ControlsCovered = append(manifest.ControlsCovered, c)
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("evidence/pack: marshal manifest: %w", err)
	}

	// Sign the manifest with the control-plane key. An auditor who
	// receives this pack from the DPO can verify the manifest signature
	// against the published Vault public key without trusting Personel
	// infrastructure. If the manifest signature checks out AND each
	// item's individual signature checks out AND the WORM bucket
	// contents match the canonical form, the pack is admissible.
	sig, keyVersion, err := b.signer.Sign(ctx, manifestBytes)
	if err != nil {
		return nil, fmt.Errorf("evidence/pack: sign manifest: %w", err)
	}

	manifestFile, err := zw.Create("manifest.json")
	if err != nil {
		return nil, fmt.Errorf("evidence/pack: zip create manifest: %w", err)
	}
	if _, err := manifestFile.Write(manifestBytes); err != nil {
		return nil, fmt.Errorf("evidence/pack: write manifest: %w", err)
	}

	manifestSigFile, err := zw.Create("manifest.signature")
	if err != nil {
		return nil, fmt.Errorf("evidence/pack: zip create manifest sig: %w", err)
	}
	if _, err := manifestSigFile.Write(sig); err != nil {
		return nil, fmt.Errorf("evidence/pack: write manifest sig: %w", err)
	}

	// Key version in a discoverable txt file so an offline auditor can
	// pick the right public key from the Vault key history dump.
	keyFile, err := zw.Create("manifest.key_version.txt")
	if err != nil {
		return nil, fmt.Errorf("evidence/pack: zip create key version: %w", err)
	}
	if _, err := keyFile.Write([]byte(keyVersion)); err != nil {
		return nil, fmt.Errorf("evidence/pack: write key version: %w", err)
	}

	return manifest, nil
}
