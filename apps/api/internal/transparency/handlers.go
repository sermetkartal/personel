// Package transparency — HTTP handlers for the employee transparency portal.
package transparency

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/dsr"
	"github.com/personel/api/internal/httpx"
)

// MyDataHandler — GET /v1/me
func MyDataHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		summary, err := svc.MyData(r.Context(), p, p.UserID)
		if err != nil {
			httpx.WriteError(w, r, http.StatusForbidden, httpx.ProblemTypeForbidden, "Forbidden", "err.forbidden")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, summary)
	}
}

// MyLiveViewHistoryHandler — GET /v1/me/live-view-history
// Shows the employee their own live view session history.
// Default ON per live-view-protocol.md; restricted only by audited DPO action.
func MyLiveViewHistoryHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		sessions, err := svc.MyLiveViewHistory(r.Context(), p, p.UserID)
		if err != nil {
			httpx.WriteError(w, r, http.StatusForbidden, httpx.ProblemTypeForbidden, "Forbidden", "err.forbidden")
			return
		}
		// Return session list with only role-level actor info (not names).
		type sessionView struct {
			ID           string `json:"id"`
			OccurredAt   string `json:"occurred_at"`
			Duration     string `json:"duration_seconds"`
			RequesterRole string `json:"requester_role"` // "admin" or "manager" — NOT the user's name
			ApproverRole  string `json:"approver_role"`  // "hr" — NOT the user's name
			ReasonCategory string `json:"reason_category"` // category only, not free text
			State         string `json:"state"`
		}
		views := make([]sessionView, 0, len(sessions))
		for _, s := range sessions {
			views = append(views, sessionView{
				ID:             s.ID,
				OccurredAt:     s.CreatedAt.Format("2006-01-02T15:04:05Z"),
				Duration:       s.RequestedDuration.String(),
				RequesterRole:  "yonetici", // role label, not PII
				ApproverRole:   "ik",
				ReasonCategory: reasonCategory(s.ReasonCode),
				State:          string(s.State),
			})
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": views})
	}
}

// SubmitDSRHandler — POST /v1/me/dsr (employee submits a DSR via portal)
// Delegates to the DSR service submit flow.
func SubmitDSRHandler(dsrSvc *dsr.Service) http.HandlerFunc {
	type reqBody struct {
		RequestType   dsr.RequestType `json:"request_type"`
		ScopeJSON     map[string]any  `json:"scope"`
		Justification string          `json:"justification"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		var body reqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Bad Request", "err.validation")
			return
		}

		req, err := dsrSvc.Submit(r.Context(), dsr.SubmitInput{
			TenantID:       p.TenantID,
			EmployeeUserID: p.UserID,
			RequestType:    body.RequestType,
			ScopeJSON:      body.ScopeJSON,
			Justification:  body.Justification,
			ActorUA:        r.UserAgent(),
		})
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}

		httpx.WriteJSON(w, http.StatusCreated, map[string]any{
			"ticket_id":    req.ID,
			"sla_deadline": req.SLADeadline,
			"state":        string(req.State),
			"message":      "Başvurunuz alındı. SLA süresi: 30 gün.",
		})
	}
}

// MyDSRHandler — GET /v1/me/dsr (employee views their own DSR status)
func MyDSRHandler(dsrSvc *dsr.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		list, err := dsrSvc.List(r.Context(), p.TenantID, nil)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}

		// Filter to only the employee's own requests.
		var own []*dsr.Request
		for _, req := range list {
			if req.EmployeeUserID == p.UserID {
				own = append(own, req)
			}
		}

		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": own})
	}
}

// AcknowledgeNotificationHandler — POST /v1/me/acknowledge-notification
//
// Writes an audited "Anladım" acknowledgement for the employee transparency
// first-login modal. Legally required per KVKK m.10.
// Idempotent: if the employee already acknowledged this aydinlatma_version,
// returns 200 with the existing audit_id; otherwise 201.
func AcknowledgeNotificationHandler(svc *Service) http.HandlerFunc {
	type reqBody struct {
		NotificationType  string `json:"notification_type"`
		AydinlatmaVersion string `json:"aydinlatma_version"`
		Locale            string `json:"locale"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		var body reqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Bad Request", "err.validation")
			return
		}
		if body.NotificationType == "" {
			body.NotificationType = "first_login_disclosure"
		}
		if body.AydinlatmaVersion == "" {
			body.AydinlatmaVersion = "1.0"
		}
		if body.Locale == "" {
			body.Locale = "tr"
		}

		rec := audit.FromContext(r.Context())
		result, err := svc.AcknowledgeNotification(r.Context(), rec, AcknowledgeInput{
			UserID:            p.UserID,
			TenantID:          p.TenantID,
			NotificationType:  body.NotificationType,
			AydinlatmaVersion: body.AydinlatmaVersion,
			Locale:            body.Locale,
		})
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}

		status := http.StatusCreated
		if result.AlreadyDone {
			status = http.StatusOK
		}
		httpx.WriteJSON(w, status, map[string]any{
			"acknowledged_at": result.AcknowledgedAt,
			"audit_id":        result.AuditID,
		})
	}
}

// MyDSRDetailHandler — GET /v1/me/dsr/{id}
//
// Returns a single DSR owned by the calling employee. Returns 404 (not 403)
// when the DSR does not exist or belongs to a different user, to prevent
// information leakage about other users' DSR IDs.
func MyDSRDetailHandler(dsrSvc *dsr.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "id")

		// Delegates to GetScoped which enforces owner check server-side.
		req, err := dsrSvc.GetScoped(r.Context(), p.TenantID, p.UserID, id)
		if err != nil || req == nil {
			// Always 404 — never 403, to prevent enumeration.
			httpx.WriteError(w, r, http.StatusNotFound, httpx.ProblemTypeNotFound, "Not Found", "err.not_found")
			return
		}

		httpx.WriteJSON(w, http.StatusOK, req)
	}
}

// reasonCategory converts a reason_code to a general category string
// that does not leak investigation details to the employee.
func reasonCategory(code string) string {
	if code == "" {
		return "diger"
	}
	// In a full implementation this would look up a category map.
	return "guvenlik-sorusturma"
}
