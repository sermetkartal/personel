package kvkk

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// maxMultipartMemory is the in-memory buffer ceiling for ParseMultipartForm.
// Larger uploads spill to disk (OS temp). 12 MB covers the 10 MB PDF limit
// plus multipart overhead.
const maxMultipartMemory = 12 << 20

// writeErr is a small convenience to keep handler code compact.
func writeErr(w http.ResponseWriter, r *http.Request, status int, msg string) {
	httpx.WriteError(w, r, status, httpx.ProblemTypeValidation, msg, "err.validation")
}

// --- VERBİS ---

func GetVerbisHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		info, err := svc.GetVerbis(r.Context(), p.TenantID)
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusOK, info)
	}
}

func PatchVerbisHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		var req UpdateVerbisRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, r, http.StatusBadRequest, "invalid body")
			return
		}
		if err := svc.UpdateVerbis(r.Context(), p.UserID, p.TenantID, req); err != nil {
			writeErr(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// --- Aydınlatma metni ---

func GetAydinlatmaHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		info, err := svc.GetAydinlatma(r.Context(), p.TenantID)
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusOK, info)
	}
}

func PublishAydinlatmaHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		var req PublishAydinlatmaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, r, http.StatusBadRequest, "invalid body")
			return
		}
		info, err := svc.PublishAydinlatma(r.Context(), p.UserID, p.TenantID, req)
		if err != nil {
			writeErr(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusOK, info)
	}
}

// --- DPA ---

func GetDpaHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		info, err := svc.GetDpa(r.Context(), p.TenantID)
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusOK, info)
	}
}

// UploadDpaHandler accepts a multipart form:
//
//	file         — the PDF (field name "file")
//	signed_at    — RFC3339 timestamp string
//	signatories  — JSON array of DpaSignatory objects
func UploadDpaHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		if err := r.ParseMultipartForm(maxMultipartMemory); err != nil {
			writeErr(w, r, http.StatusBadRequest, "invalid multipart form")
			return
		}

		pdf, contentType, err := readMultipartFile(r, "file")
		if err != nil {
			writeErr(w, r, http.StatusBadRequest, err.Error())
			return
		}
		signedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(r.FormValue("signed_at")))
		if err != nil {
			writeErr(w, r, http.StatusBadRequest, "signed_at must be RFC3339")
			return
		}
		var sigs []DpaSignatory
		if raw := r.FormValue("signatories"); raw != "" {
			if err := json.Unmarshal([]byte(raw), &sigs); err != nil {
				writeErr(w, r, http.StatusBadRequest, "signatories must be JSON array")
				return
			}
		}

		info, err := svc.UploadDpa(r.Context(), p.UserID, p.TenantID, UploadDpaRequest{
			PDFBytes:    pdf,
			ContentType: contentType,
			SignedAt:    signedAt,
			Signatories: sigs,
		})
		if err != nil {
			status := http.StatusUnprocessableEntity
			if errors.Is(err, ErrNoDocumentStore) {
				status = http.StatusServiceUnavailable
			}
			writeErr(w, r, status, err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, info)
	}
}

// --- DPIA ---

func GetDpiaHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		info, err := svc.GetDpia(r.Context(), p.TenantID)
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusOK, info)
	}
}

// UploadDpiaHandler accepts a multipart form:
//
//	file          — the amendment PDF
//	completed_at  — RFC3339 timestamp string
func UploadDpiaHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		if err := r.ParseMultipartForm(maxMultipartMemory); err != nil {
			writeErr(w, r, http.StatusBadRequest, "invalid multipart form")
			return
		}
		pdf, contentType, err := readMultipartFile(r, "file")
		if err != nil {
			writeErr(w, r, http.StatusBadRequest, err.Error())
			return
		}
		completedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(r.FormValue("completed_at")))
		if err != nil {
			writeErr(w, r, http.StatusBadRequest, "completed_at must be RFC3339")
			return
		}

		info, err := svc.UploadDpia(r.Context(), p.UserID, p.TenantID, UploadDpiaRequest{
			PDFBytes:    pdf,
			ContentType: contentType,
			CompletedAt: completedAt,
		})
		if err != nil {
			status := http.StatusUnprocessableEntity
			if errors.Is(err, ErrNoDocumentStore) {
				status = http.StatusServiceUnavailable
			}
			writeErr(w, r, status, err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, info)
	}
}

// --- User consent ---

func ListConsentsHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		consentType := r.URL.Query().Get("type")
		records, err := svc.ListConsents(r.Context(), p.TenantID, consentType)
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": records})
	}
}

func RecordConsentHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		var req RecordConsentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, r, http.StatusBadRequest, "invalid body")
			return
		}
		pdf, err := base64.StdEncoding.DecodeString(req.DocumentBase64)
		if err != nil {
			writeErr(w, r, http.StatusBadRequest, "document_base64 is not valid base64")
			return
		}
		// Cap enforced at the service boundary, but short-circuit here
		// so we never allocate huge buffers from an attacker-controlled
		// Content-Length.
		if len(pdf) > MaxDocumentBytes {
			writeErr(w, r, http.StatusRequestEntityTooLarge,
				"document exceeds "+strconv.Itoa(MaxDocumentBytes)+" bytes")
			return
		}
		rec, err := svc.RecordConsent(r.Context(), p.UserID, p.TenantID, req, pdf)
		if err != nil {
			status := http.StatusUnprocessableEntity
			if errors.Is(err, ErrNoDocumentStore) {
				status = http.StatusServiceUnavailable
			}
			writeErr(w, r, status, err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, rec)
	}
}

func RevokeConsentHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		userID, err := uuid.Parse(chi.URLParam(r, "userID"))
		if err != nil {
			writeErr(w, r, http.StatusBadRequest, "invalid user_id")
			return
		}
		consentType := chi.URLParam(r, "consentType")
		if consentType == "" {
			writeErr(w, r, http.StatusBadRequest, "consent_type is required")
			return
		}
		if err := svc.RevokeConsent(r.Context(), p.UserID, p.TenantID, userID, consentType); err != nil {
			writeErr(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// readMultipartFile reads the named file field from the multipart form
// and enforces MaxDocumentBytes at read time. Returns the bytes and the
// reported Content-Type.
func readMultipartFile(r *http.Request, field string) ([]byte, string, error) {
	file, header, err := r.FormFile(field)
	if err != nil {
		return nil, "", errors.New("missing file field")
	}
	defer file.Close()
	if header.Size > MaxDocumentBytes {
		return nil, "", errors.New("file exceeds size limit")
	}
	// io.LimitReader defends against a lying header.Size by capping the
	// actual bytes read regardless of advertised length.
	limited := io.LimitReader(file, MaxDocumentBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, "", errors.New("read failed")
	}
	if len(data) > MaxDocumentBytes {
		return nil, "", errors.New("file exceeds size limit")
	}
	ct := header.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}
	return data, ct, nil
}
