package kvkk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"

	"github.com/personel/api/internal/audit"
)

// DocumentStore is the narrow contract the service needs for PDF uploads.
// The concrete implementation in production is a thin adapter around
// apps/api/internal/minio.Client. Scaffold deployments may pass nil; in
// that case every upload path errors with ErrNoDocumentStore.
//
// Keeping this as a local interface (rather than importing the minio
// package directly) avoids pulling minio into unit tests and lets the
// kvkk package stand on its own.
type DocumentStore interface {
	PutDocument(ctx context.Context, objectKey string, data []byte, contentType string) error
}

// ErrNoDocumentStore is returned when an upload is attempted but the
// service was constructed without a DocumentStore (scaffold mode).
var ErrNoDocumentStore = errors.New("kvkk: document store not configured")

// MaxDocumentBytes is the upload limit for DPA/DPIA/consent PDFs.
// Reject anything larger at the handler and service boundaries — the
// console enforces the same limit client-side as a fast-fail.
const MaxDocumentBytes = 10 * 1024 * 1024 // 10 MB

// Service manages all KVKK-scoped mutations and reads.
type Service struct {
	pool     *pgxpool.Pool
	recorder *audit.Recorder
	docs     DocumentStore
	log      *slog.Logger
	now      func() time.Time
}

// NewService constructs a KVKK service. docs may be nil; in that case
// upload endpoints return ErrNoDocumentStore. The audit recorder MUST
// be non-nil — every mutation writes an audit entry, nil would mean
// silent tamper-able writes and that is unacceptable here.
func NewService(pool *pgxpool.Pool, rec *audit.Recorder, docs DocumentStore, log *slog.Logger) *Service {
	if rec == nil {
		panic("kvkk: audit recorder is required")
	}
	return &Service{
		pool:     pool,
		recorder: rec,
		docs:     docs,
		log:      log,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// --- VERBİS ---

// GetVerbis returns the tenant's VERBİS registration state.
func (s *Service) GetVerbis(ctx context.Context, tenantID string) (VerbisInfo, error) {
	var info VerbisInfo
	var num *string
	var ts *time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT verbis_registration_number, verbis_registered_at
		   FROM tenants WHERE id = $1::uuid`,
		tenantID,
	).Scan(&num, &ts)
	if err != nil {
		return info, fmt.Errorf("kvkk: get verbis: %w", err)
	}
	if num != nil {
		info.RegistrationNumber = *num
	}
	info.RegisteredAt = ts
	return info, nil
}

// UpdateVerbis writes the new VERBİS registration values after an audit
// entry. An empty RegistrationNumber is rejected — clearing VERBİS data
// must be an explicit DELETE flow that does not exist yet.
func (s *Service) UpdateVerbis(ctx context.Context, actorID, tenantID string, req UpdateVerbisRequest) error {
	num := strings.TrimSpace(req.RegistrationNumber)
	if num == "" {
		return errors.New("kvkk: registration_number is required")
	}
	if len(num) > 128 {
		return errors.New("kvkk: registration_number too long (max 128)")
	}

	prev, err := s.GetVerbis(ctx, tenantID)
	if err != nil {
		return err
	}

	if _, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    actorID,
		TenantID: tenantID,
		Action:   audit.ActionKvkkVerbisUpdate,
		Target:   "tenant:" + tenantID,
		Details: map[string]any{
			"before": prev,
			"after":  req,
		},
	}); err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx,
		`UPDATE tenants
		    SET verbis_registration_number = $1,
		        verbis_registered_at       = $2,
		        updated_at                 = now()
		  WHERE id = $3::uuid`,
		num, req.RegisteredAt, tenantID,
	)
	if err != nil {
		return fmt.Errorf("kvkk: update verbis: %w", err)
	}
	return nil
}

// --- Aydınlatma metni ---

// GetAydinlatma returns the active aydınlatma metni and version counter.
func (s *Service) GetAydinlatma(ctx context.Context, tenantID string) (AydinlatmaInfo, error) {
	var info AydinlatmaInfo
	var md *string
	var ts *time.Time
	err := s.pool.QueryRow(ctx,
		`SELECT aydinlatma_markdown, aydinlatma_published_at, aydinlatma_version
		   FROM tenants WHERE id = $1::uuid`,
		tenantID,
	).Scan(&md, &ts, &info.Version)
	if err != nil {
		return info, fmt.Errorf("kvkk: get aydinlatma: %w", err)
	}
	if md != nil {
		info.Markdown = *md
	}
	info.PublishedAt = ts
	return info, nil
}

// PublishAydinlatma writes a new version of the aydınlatma metni. The
// version counter is atomically incremented in the same UPDATE so two
// concurrent publishes cannot collide. Empty markdown is rejected.
func (s *Service) PublishAydinlatma(ctx context.Context, actorID, tenantID string, req PublishAydinlatmaRequest) (AydinlatmaInfo, error) {
	md := strings.TrimSpace(req.Markdown)
	if md == "" {
		return AydinlatmaInfo{}, errors.New("kvkk: markdown is required")
	}
	if len(md) > 256*1024 {
		return AydinlatmaInfo{}, errors.New("kvkk: markdown too large (max 256 KB)")
	}

	prev, err := s.GetAydinlatma(ctx, tenantID)
	if err != nil {
		return AydinlatmaInfo{}, err
	}

	if _, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    actorID,
		TenantID: tenantID,
		Action:   audit.ActionKvkkAydinlatmaPublish,
		Target:   "tenant:" + tenantID,
		Details: map[string]any{
			"previous_version": prev.Version,
			"markdown_length":  len(md),
		},
	}); err != nil {
		return AydinlatmaInfo{}, err
	}

	now := s.now()
	var info AydinlatmaInfo
	var outMD string
	err = s.pool.QueryRow(ctx,
		`UPDATE tenants
		    SET aydinlatma_markdown     = $1,
		        aydinlatma_published_at = $2,
		        aydinlatma_version      = aydinlatma_version + 1,
		        updated_at              = now()
		  WHERE id = $3::uuid
		  RETURNING aydinlatma_markdown, aydinlatma_published_at, aydinlatma_version`,
		md, now, tenantID,
	).Scan(&outMD, &info.PublishedAt, &info.Version)
	if err != nil {
		return AydinlatmaInfo{}, fmt.Errorf("kvkk: publish aydinlatma: %w", err)
	}
	info.Markdown = outMD
	return info, nil
}

// --- DPA ---

// GetDpa returns DPA metadata (no document bytes).
func (s *Service) GetDpa(ctx context.Context, tenantID string) (DpaInfo, error) {
	var info DpaInfo
	var key, hash *string
	var sigs []byte
	err := s.pool.QueryRow(ctx,
		`SELECT dpa_signed_at, dpa_document_key, dpa_document_sha256, dpa_signatories
		   FROM tenants WHERE id = $1::uuid`,
		tenantID,
	).Scan(&info.SignedAt, &key, &hash, &sigs)
	if err != nil {
		return info, fmt.Errorf("kvkk: get dpa: %w", err)
	}
	if key != nil {
		info.DocumentKey = *key
	}
	if hash != nil {
		info.DocumentSHA256 = *hash
	}
	if len(sigs) > 0 {
		_ = json.Unmarshal(sigs, &info.Signatories)
	}
	return info, nil
}

// UploadDpa stores the PDF in MinIO and updates tenants.dpa_* columns.
func (s *Service) UploadDpa(ctx context.Context, actorID, tenantID string, req UploadDpaRequest) (DpaInfo, error) {
	if err := validatePDF(req.PDFBytes); err != nil {
		return DpaInfo{}, err
	}
	if req.SignedAt.IsZero() {
		return DpaInfo{}, errors.New("kvkk: signed_at is required")
	}
	if len(req.Signatories) == 0 {
		return DpaInfo{}, errors.New("kvkk: at least one signatory is required")
	}
	if s.docs == nil {
		return DpaInfo{}, ErrNoDocumentStore
	}

	hash := sha256Hex(req.PDFBytes)
	objectKey := fmt.Sprintf("kvkk/%s/dpa/%s.pdf", tenantID, ulid.Make().String())

	prev, err := s.GetDpa(ctx, tenantID)
	if err != nil {
		return DpaInfo{}, err
	}

	if _, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    actorID,
		TenantID: tenantID,
		Action:   audit.ActionKvkkDpaUpload,
		Target:   "tenant:" + tenantID,
		Details: map[string]any{
			"object_key":    objectKey,
			"sha256":        hash,
			"size_bytes":    len(req.PDFBytes),
			"signed_at":     req.SignedAt.Format(time.RFC3339),
			"signatories":   req.Signatories,
			"previous_key":  prev.DocumentKey,
			"previous_hash": prev.DocumentSHA256,
		},
	}); err != nil {
		return DpaInfo{}, err
	}

	if err := s.docs.PutDocument(ctx, objectKey, req.PDFBytes, "application/pdf"); err != nil {
		return DpaInfo{}, fmt.Errorf("kvkk: put dpa: %w", err)
	}

	sigsJSON, err := json.Marshal(req.Signatories)
	if err != nil {
		return DpaInfo{}, fmt.Errorf("kvkk: marshal signatories: %w", err)
	}

	_, err = s.pool.Exec(ctx,
		`UPDATE tenants
		    SET dpa_signed_at       = $1,
		        dpa_document_key    = $2,
		        dpa_document_sha256 = $3,
		        dpa_signatories     = $4::jsonb,
		        updated_at          = now()
		  WHERE id = $5::uuid`,
		req.SignedAt, objectKey, hash, sigsJSON, tenantID,
	)
	if err != nil {
		return DpaInfo{}, fmt.Errorf("kvkk: update dpa: %w", err)
	}

	signedAt := req.SignedAt
	return DpaInfo{
		SignedAt:       &signedAt,
		DocumentKey:    objectKey,
		DocumentSHA256: hash,
		Signatories:    req.Signatories,
	}, nil
}

// --- DPIA ---

// GetDpia returns DPIA amendment metadata.
func (s *Service) GetDpia(ctx context.Context, tenantID string) (DpiaInfo, error) {
	var info DpiaInfo
	var key, hash *string
	err := s.pool.QueryRow(ctx,
		`SELECT dpia_amendment_key, dpia_amendment_sha256, dpia_completed_at
		   FROM tenants WHERE id = $1::uuid`,
		tenantID,
	).Scan(&key, &hash, &info.CompletedAt)
	if err != nil {
		return info, fmt.Errorf("kvkk: get dpia: %w", err)
	}
	if key != nil {
		info.AmendmentKey = *key
	}
	if hash != nil {
		info.AmendmentSHA256 = *hash
	}
	return info, nil
}

// UploadDpia stores the DPIA amendment PDF and updates dpia_* columns.
func (s *Service) UploadDpia(ctx context.Context, actorID, tenantID string, req UploadDpiaRequest) (DpiaInfo, error) {
	if err := validatePDF(req.PDFBytes); err != nil {
		return DpiaInfo{}, err
	}
	if req.CompletedAt.IsZero() {
		return DpiaInfo{}, errors.New("kvkk: completed_at is required")
	}
	if s.docs == nil {
		return DpiaInfo{}, ErrNoDocumentStore
	}

	hash := sha256Hex(req.PDFBytes)
	objectKey := fmt.Sprintf("kvkk/%s/dpia/%s.pdf", tenantID, ulid.Make().String())

	prev, err := s.GetDpia(ctx, tenantID)
	if err != nil {
		return DpiaInfo{}, err
	}

	if _, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    actorID,
		TenantID: tenantID,
		Action:   audit.ActionKvkkDpiaUpload,
		Target:   "tenant:" + tenantID,
		Details: map[string]any{
			"object_key":    objectKey,
			"sha256":        hash,
			"size_bytes":    len(req.PDFBytes),
			"completed_at":  req.CompletedAt.Format(time.RFC3339),
			"previous_key":  prev.AmendmentKey,
			"previous_hash": prev.AmendmentSHA256,
		},
	}); err != nil {
		return DpiaInfo{}, err
	}

	if err := s.docs.PutDocument(ctx, objectKey, req.PDFBytes, "application/pdf"); err != nil {
		return DpiaInfo{}, fmt.Errorf("kvkk: put dpia: %w", err)
	}

	_, err = s.pool.Exec(ctx,
		`UPDATE tenants
		    SET dpia_amendment_key    = $1,
		        dpia_amendment_sha256 = $2,
		        dpia_completed_at     = $3,
		        updated_at            = now()
		  WHERE id = $4::uuid`,
		objectKey, hash, req.CompletedAt, tenantID,
	)
	if err != nil {
		return DpiaInfo{}, fmt.Errorf("kvkk: update dpia: %w", err)
	}

	completedAt := req.CompletedAt
	return DpiaInfo{
		AmendmentKey:    objectKey,
		AmendmentSHA256: hash,
		CompletedAt:     &completedAt,
	}, nil
}

// --- User consent ---

// ListConsents returns consent records for the tenant, optionally filtered
// by consent_type. RLS on user_consent enforces tenant isolation.
func (s *Service) ListConsents(ctx context.Context, tenantID, consentType string) ([]ConsentRecord, error) {
	var rows pgx.Rows
	var err error
	if consentType == "" {
		rows, err = s.pool.Query(ctx,
			`SELECT id, user_id, consent_type, signed_at, revoked_at,
			        document_key, document_sha256, created_at
			   FROM user_consent
			  WHERE tenant_id = $1::uuid
			  ORDER BY created_at DESC`,
			tenantID,
		)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT id, user_id, consent_type, signed_at, revoked_at,
			        document_key, document_sha256, created_at
			   FROM user_consent
			  WHERE tenant_id = $1::uuid AND consent_type = $2
			  ORDER BY created_at DESC`,
			tenantID, consentType,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("kvkk: list consents: %w", err)
	}
	defer rows.Close()

	var out []ConsentRecord
	for rows.Next() {
		var r ConsentRecord
		var key, hash *string
		if err := rows.Scan(
			&r.ID, &r.UserID, &r.ConsentType, &r.SignedAt, &r.RevokedAt,
			&key, &hash, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		if key != nil {
			r.DocumentKey = *key
		}
		if hash != nil {
			r.DocumentSHA256 = *hash
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// RecordConsent writes a new or refreshed consent row. Document bytes are
// decoded from base64, stored in MinIO, and referenced by object key.
// Re-consenting after a revoke updates the same row (UNIQUE constraint on
// (user_id, consent_type)) and clears revoked_at.
func (s *Service) RecordConsent(ctx context.Context, actorID, tenantID string, req RecordConsentRequest, docBytes []byte) (ConsentRecord, error) {
	if req.UserID == uuid.Nil {
		return ConsentRecord{}, errors.New("kvkk: user_id is required")
	}
	if _, ok := AllowedConsentTypes[req.ConsentType]; !ok {
		return ConsentRecord{}, fmt.Errorf("kvkk: consent_type %q is not in the allowed set", req.ConsentType)
	}
	if req.SignedAt.IsZero() {
		return ConsentRecord{}, errors.New("kvkk: signed_at is required")
	}
	if err := validatePDF(docBytes); err != nil {
		return ConsentRecord{}, err
	}
	if s.docs == nil {
		return ConsentRecord{}, ErrNoDocumentStore
	}

	hash := sha256Hex(docBytes)
	objectKey := fmt.Sprintf("kvkk/%s/consent/%s/%s.pdf",
		tenantID, req.UserID.String(), ulid.Make().String())

	if _, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    actorID,
		TenantID: tenantID,
		Action:   audit.ActionKvkkConsentRecord,
		Target:   "user:" + req.UserID.String() + ":consent:" + req.ConsentType,
		Details: map[string]any{
			"consent_type": req.ConsentType,
			"signed_at":    req.SignedAt.Format(time.RFC3339),
			"object_key":   objectKey,
			"sha256":       hash,
			"size_bytes":   len(docBytes),
		},
	}); err != nil {
		return ConsentRecord{}, err
	}

	if err := s.docs.PutDocument(ctx, objectKey, docBytes, "application/pdf"); err != nil {
		return ConsentRecord{}, fmt.Errorf("kvkk: put consent: %w", err)
	}

	var rec ConsentRecord
	err := s.pool.QueryRow(ctx,
		`INSERT INTO user_consent
		        (tenant_id, user_id, consent_type, signed_at,
		         document_key, document_sha256, revoked_at)
		 VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, NULL)
		 ON CONFLICT (user_id, consent_type) DO UPDATE
		    SET signed_at       = EXCLUDED.signed_at,
		        document_key    = EXCLUDED.document_key,
		        document_sha256 = EXCLUDED.document_sha256,
		        revoked_at      = NULL
		 RETURNING id, user_id, consent_type, signed_at, revoked_at,
		           document_key, document_sha256, created_at`,
		tenantID, req.UserID, req.ConsentType, req.SignedAt,
		objectKey, hash,
	).Scan(
		&rec.ID, &rec.UserID, &rec.ConsentType, &rec.SignedAt, &rec.RevokedAt,
		&rec.DocumentKey, &rec.DocumentSHA256, &rec.CreatedAt,
	)
	if err != nil {
		return ConsentRecord{}, fmt.Errorf("kvkk: upsert consent: %w", err)
	}
	return rec, nil
}

// RevokeConsent marks an existing consent row revoked_at = now().
// If no row exists or it is already revoked, returns an error.
func (s *Service) RevokeConsent(ctx context.Context, actorID, tenantID string, userID uuid.UUID, consentType string) error {
	if _, ok := AllowedConsentTypes[consentType]; !ok {
		return fmt.Errorf("kvkk: consent_type %q is not in the allowed set", consentType)
	}

	if _, err := s.recorder.Append(ctx, audit.Entry{
		Actor:    actorID,
		TenantID: tenantID,
		Action:   audit.ActionKvkkConsentRevoke,
		Target:   "user:" + userID.String() + ":consent:" + consentType,
		Details: map[string]any{
			"consent_type": consentType,
		},
	}); err != nil {
		return err
	}

	tag, err := s.pool.Exec(ctx,
		`UPDATE user_consent
		    SET revoked_at = now()
		  WHERE tenant_id = $1::uuid
		    AND user_id   = $2::uuid
		    AND consent_type = $3
		    AND revoked_at IS NULL`,
		tenantID, userID, consentType,
	)
	if err != nil {
		return fmt.Errorf("kvkk: revoke consent: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("kvkk: no active consent to revoke")
	}
	return nil
}

// --- helpers ---

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// validatePDF rejects empty and oversized uploads and sanity-checks the
// magic header. Full PDF validation is out of scope — the tamper-evident
// guarantee is the server-computed SHA-256, not structural parsing.
func validatePDF(b []byte) error {
	if len(b) == 0 {
		return errors.New("kvkk: empty document")
	}
	if len(b) > MaxDocumentBytes {
		return fmt.Errorf("kvkk: document too large (%d bytes, max %d)", len(b), MaxDocumentBytes)
	}
	if len(b) < 4 || string(b[:4]) != "%PDF" {
		return errors.New("kvkk: document is not a PDF (missing %PDF header)")
	}
	return nil
}
