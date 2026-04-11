// Package httpx provides HTTP helpers (RFC 7807 problem responses, JSON
// helpers, request-id context plumbing) that are shared between the
// httpserver package and every domain package's HTTP handlers.
//
// This package exists to break the import cycle that would otherwise form:
// httpserver imports domain packages (to register routes), and domain
// packages need the error/JSON helpers. Putting the helpers in a
// dependency-free package avoids that.
package httpx

import (
	"context"
	"encoding/json"
	"net/http"
)

// Problem is an RFC 7807 problem detail object.
type Problem struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Status    int    `json:"status"`
	Detail    string `json:"detail,omitempty"`
	Instance  string `json:"instance,omitempty"`
	TraceID   string `json:"trace_id,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	Code      string `json:"code,omitempty"`
}

const problemContentType = "application/problem+json"
const problemBase = "https://personel.internal/problems/"

// Well-known problem types.
const (
	ProblemTypeValidation    = problemBase + "validation-error"
	ProblemTypeAuth          = problemBase + "authentication-required"
	ProblemTypeForbidden     = problemBase + "forbidden"
	ProblemTypeNotFound      = problemBase + "not-found"
	ProblemTypeConflict      = problemBase + "conflict"
	ProblemTypeRateLimit     = problemBase + "rate-limit-exceeded"
	ProblemTypeInternal      = problemBase + "internal-server-error"
	ProblemTypeSLAViolation  = problemBase + "sla-violation"
	ProblemTypeWorkflowState = problemBase + "invalid-workflow-state"
)

// Turkish user-facing strings, keyed by error code.
var trStrings = map[string]string{
	"err.unauthenticated":            "Kimlik doğrulama gereklidir. Lütfen tekrar giriş yapın.",
	"err.forbidden":                  "Bu işlem için yetkiniz bulunmamaktadır.",
	"err.not_found":                  "İstenen kaynak bulunamadı.",
	"err.validation":                 "Gönderilen veriler geçersiz. Lütfen kontrol edin.",
	"err.conflict":                   "Bu kayıt zaten mevcut veya çakışan bir durum var.",
	"err.rate_limit":                 "Çok fazla istek gönderildi. Lütfen bekleyip tekrar deneyin.",
	"err.internal":                   "Sunucu hatası oluştu. Lütfen yöneticinize bildirin.",
	"err.workflow_state":             "Bu işlem mevcut durum için uygun değil.",
	"err.approver_same_as_requester": "Onaylayan kişi, talebi oluşturan kişi olamaz.",
	"err.dsr_sla_overdue":            "Bu başvurunun yasal süresi (30 gün) dolmuştur.",
	"err.legalhold_max_duration":     "Yasal saklama süresi en fazla 2 yıl olabilir.",
	"err.liveview_cap":               "Canlı izleme süresi en fazla 60 dakika olabilir.",
	"err.dlp_state_unavailable":      "DLP durum bilgisi şu an kullanılamıyor. Lütfen yöneticinize bildirin.",
	"err.bootstrap_failed":           "PE-DEK oluşturma işlemi başarısız oldu. Vault bağlantısını kontrol edin.",
	"err.policy_invariant_dlp":       "Politika geçersiz: keystroke içerik kaydı, DLP etkinleştirilmeden açılamaz.",
	"err.acknowledge_failed":         "Bildirim onayı kaydedilemedi. Lütfen tekrar deneyin.",
}

// TRString returns the Turkish user-facing string for a code key.
func TRString(code string) string {
	if s, ok := trStrings[code]; ok {
		return s
	}
	return code
}

// --- Request ID context plumbing -----------------------------------------

type requestIDKey struct{}

// WithRequestID attaches a request ID to ctx.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestIDFromContext returns the request ID from ctx, or "".
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// --- Response helpers -----------------------------------------------------

// WriteError writes an RFC 7807 problem response.
func WriteError(w http.ResponseWriter, r *http.Request, status int, problemType, title, code string) {
	p := Problem{
		Type:      problemType,
		Title:     title,
		Status:    status,
		Detail:    TRString(code),
		Instance:  r.URL.Path,
		RequestID: RequestIDFromContext(r.Context()),
		Code:      code,
	}
	w.Header().Set("Content-Type", problemContentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}

// WriteValidationError writes a 422 with field-level details.
func WriteValidationError(w http.ResponseWriter, r *http.Request, fields map[string]string) {
	type fieldErr struct {
		Field   string `json:"field"`
		Message string `json:"message"`
	}
	type validationProblem struct {
		Problem
		Errors []fieldErr `json:"errors"`
	}
	fe := make([]fieldErr, 0, len(fields))
	for f, m := range fields {
		fe = append(fe, fieldErr{Field: f, Message: m})
	}
	vp := validationProblem{
		Problem: Problem{
			Type:      ProblemTypeValidation,
			Title:     "Validation Error",
			Status:    http.StatusUnprocessableEntity,
			Detail:    TRString("err.validation"),
			Instance:  r.URL.Path,
			RequestID: RequestIDFromContext(r.Context()),
			Code:      "err.validation",
		},
		Errors: fe,
	}
	w.Header().Set("Content-Type", problemContentType)
	w.WriteHeader(http.StatusUnprocessableEntity)
	_ = json.NewEncoder(w).Encode(vp)
}

// WriteJSON writes a JSON response body with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
