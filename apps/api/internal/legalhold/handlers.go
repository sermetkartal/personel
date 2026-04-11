// Package legalhold — HTTP handlers (DPO-only).
package legalhold

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// PlaceHandler — POST /v1/legal-holds
func PlaceHandler(svc *Service) http.HandlerFunc {
	type reqBody struct {
		ReasonCode    string     `json:"reason_code"`
		TicketID      string     `json:"ticket_id"`
		Justification string     `json:"justification"`
		EndpointID    *string    `json:"endpoint_id,omitempty"`
		UserSID       *string    `json:"user_sid,omitempty"`
		DateRangeFrom *time.Time `json:"date_range_from,omitempty"`
		DateRangeTo   *time.Time `json:"date_range_to,omitempty"`
		EventTypes    []string   `json:"event_types,omitempty"`
		DurationDays  int        `json:"duration_days"` // max 730
	}
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		var body reqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Bad Request", "err.validation")
			return
		}
		if body.ReasonCode == "" || body.TicketID == "" || body.Justification == "" {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity, httpx.ProblemTypeValidation, "Validation Error", "err.validation")
			return
		}

		dur := time.Duration(body.DurationDays) * 24 * time.Hour
		if dur == 0 {
			dur = 2 * 365 * 24 * time.Hour
		}

		hold, err := svc.Place(r.Context(), p, PlaceInput{
			TenantID:      p.TenantID,
			DPOUserID:     p.UserID,
			ReasonCode:    body.ReasonCode,
			TicketID:      body.TicketID,
			Justification: body.Justification,
			EndpointID:    body.EndpointID,
			UserSID:       body.UserSID,
			DateRangeFrom: body.DateRangeFrom,
			DateRangeTo:   body.DateRangeTo,
			EventTypes:    body.EventTypes,
			Duration:      dur,
		})
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, hold)
	}
}

// ListHandler — GET /v1/legal-holds
func ListHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		activeOnly := r.URL.Query().Get("active") != "false"
		list, err := svc.List(r.Context(), p.TenantID, activeOnly)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": list})
	}
}

// GetHandler — GET /v1/legal-holds/{holdID}
func GetHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "holdID")
		hold, err := svc.Get(r.Context(), p.TenantID, id)
		if err != nil {
			httpx.WriteError(w, r, http.StatusNotFound, httpx.ProblemTypeNotFound, "Not Found", "err.not_found")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, hold)
	}
}

// ReleaseHandler — POST /v1/legal-holds/{holdID}/release
func ReleaseHandler(svc *Service) http.HandlerFunc {
	type reqBody struct {
		Reason string `json:"reason"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "holdID")
		var body reqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Reason == "" {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Bad Request", "err.validation")
			return
		}
		if err := svc.Release(r.Context(), p, id, body.Reason); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
