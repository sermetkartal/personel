// Package kvkk implements the backend for the console KVKK compliance
// section (Wave 9 Sprint 2). It exposes per-tenant VERBİS, aydınlatma
// metni, DPA, and DPIA metadata as well as a per-user açık rıza
// (explicit consent) ledger.
//
// Design notes:
//
//  1. Tenant-level data (VERBİS, aydınlatma, DPA, DPIA) lives as columns on
//     the tenants row — migration 0038. Each tenant has exactly one current
//     value for each of these, and history is preserved through the audit
//     log. Document bytes (DPA / DPIA PDFs) live in MinIO; Postgres stores
//     only the object key plus a SHA-256 hash so the UI can prove the file
//     hasn't been swapped out underneath.
//
//  2. Per-user açık rıza lives in the user_consent table — migration 0039.
//     One row per (user_id, consent_type) tuple. Re-signing after a revoke
//     updates the same row; the audit log is the authoritative timeline.
//
//  3. Every mutation writes an audit entry BEFORE any DB / MinIO write —
//     matches the discipline enforced by audit.Recorder docstrings. The
//     audit entry is the single tamper-evident record; failure to append
//     short-circuits the mutation.
//
//  4. RBAC is enforced at the router layer (auth.RequireRole) and again
//     inside the service only for defence-in-depth where relevant (e.g.
//     cross-user consent writes).
package kvkk

import (
	"time"

	"github.com/google/uuid"
)

// --- Tenant-scoped aggregates ---

// VerbisInfo carries the tenant's VERBİS registration state.
type VerbisInfo struct {
	RegistrationNumber string     `json:"registration_number,omitempty"`
	RegisteredAt       *time.Time `json:"registered_at,omitempty"`
}

// UpdateVerbisRequest is the body of PATCH /v1/kvkk/verbis.
type UpdateVerbisRequest struct {
	RegistrationNumber string     `json:"registration_number"`
	RegisteredAt       *time.Time `json:"registered_at,omitempty"`
}

// AydinlatmaInfo holds the current aydınlatma metni state.
type AydinlatmaInfo struct {
	Markdown    string     `json:"markdown,omitempty"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	Version     int        `json:"version"`
}

// PublishAydinlatmaRequest is the body of POST /v1/kvkk/aydinlatma/publish.
type PublishAydinlatmaRequest struct {
	Markdown string `json:"markdown"`
}

// DpaSignatory is one party listed in dpa_signatories.
type DpaSignatory struct {
	Name         string    `json:"name"`
	Role         string    `json:"role"`
	Organization string    `json:"organization"`
	SignedAt     time.Time `json:"signed_at"`
}

// DpaInfo holds DPA metadata.
type DpaInfo struct {
	SignedAt       *time.Time     `json:"signed_at,omitempty"`
	DocumentKey    string         `json:"document_key,omitempty"`
	DocumentSHA256 string         `json:"document_sha256,omitempty"`
	Signatories    []DpaSignatory `json:"signatories,omitempty"`
}

// UploadDpaRequest is the in-memory parsed form of the multipart
// /v1/kvkk/dpa/upload request.
type UploadDpaRequest struct {
	PDFBytes    []byte
	ContentType string
	SignedAt    time.Time
	Signatories []DpaSignatory
}

// DpiaInfo holds DPIA metadata.
type DpiaInfo struct {
	AmendmentKey    string     `json:"amendment_key,omitempty"`
	AmendmentSHA256 string     `json:"amendment_sha256,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
}

// UploadDpiaRequest is the in-memory form of /v1/kvkk/dpia/upload.
type UploadDpiaRequest struct {
	PDFBytes    []byte
	ContentType string
	CompletedAt time.Time
}

// --- User-scoped consent ---

// ConsentRecord is one row from user_consent.
type ConsentRecord struct {
	ID             uuid.UUID  `json:"id"`
	UserID         uuid.UUID  `json:"user_id"`
	ConsentType    string     `json:"consent_type"`
	SignedAt       *time.Time `json:"signed_at,omitempty"`
	RevokedAt      *time.Time `json:"revoked_at,omitempty"`
	DocumentKey    string     `json:"document_key,omitempty"`
	DocumentSHA256 string     `json:"document_sha256,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

// RecordConsentRequest is the body of POST /v1/kvkk/consents.
type RecordConsentRequest struct {
	UserID         uuid.UUID `json:"user_id"`
	ConsentType    string    `json:"consent_type"`
	SignedAt       time.Time `json:"signed_at"`
	DocumentBase64 string    `json:"document_base64"`
}

// AllowedConsentTypes is the whitelist enforced by the service.
// Free-form strings are not accepted — a typo would silently create a
// new category that the DLP engine does not honour.
var AllowedConsentTypes = map[string]struct{}{
	"dlp":                       {},
	"live_view_recording":       {},
	"screen_capture_high_freq":  {},
	"cross_department_transfer": {},
}
