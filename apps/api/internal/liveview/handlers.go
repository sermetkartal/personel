// Package liveview — HTTP handlers for live view endpoints.
package liveview

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// RequestHandler — POST /v1/live-view/requests
func RequestHandler(svc *Service) http.HandlerFunc {
	type reqBody struct {
		EndpointID    string `json:"endpoint_id"`
		ReasonCode    string `json:"reason_code"`
		Justification string `json:"justification"`
		DurationSecs  uint32 `json:"duration_seconds"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		var body reqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.EndpointID == "" || body.ReasonCode == "" {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Bad Request", "err.validation")
			return
		}

		sess, err := svc.RequestLiveView(r.Context(), p, body.EndpointID, body.ReasonCode, body.Justification, body.DurationSecs)
		if err != nil {
			switch err {
			case auth.ErrForbidden:
				httpx.WriteError(w, r, http.StatusForbidden, httpx.ProblemTypeForbidden, "Forbidden", "err.forbidden")
			default:
				httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			}
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, sess)
	}
}

// ListRequestsHandler — GET /v1/live-view/requests
func ListRequestsHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		var statePtr *State
		if s := r.URL.Query().Get("state"); s != "" {
			state := State(s)
			statePtr = &state
		}
		list, err := svc.ListRequests(r.Context(), p.TenantID, statePtr)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"items": list,
			"pagination": map[string]any{"page": 1, "page_size": len(list), "total": len(list)},
		})
	}
}

// GetRequestHandler — GET /v1/live-view/requests/{requestID}
func GetRequestHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "requestID")
		sess, err := svc.GetSession(r.Context(), p.TenantID, id)
		if err != nil {
			httpx.WriteError(w, r, http.StatusNotFound, httpx.ProblemTypeNotFound, "Not Found", "err.not_found")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, sess)
	}
}

// ApproveHandler — POST /v1/live-view/requests/{requestID}/approve (IT Manager or Admin; dual-control)
func ApproveHandler(svc *Service) http.HandlerFunc {
	type reqBody struct {
		Notes string `json:"notes"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "requestID")
		var body reqBody
		_ = json.NewDecoder(r.Body).Decode(&body)

		sess, err := svc.Approve(r.Context(), p, id, body.Notes)
		if err != nil {
			switch err {
			case auth.ErrForbidden:
				httpx.WriteError(w, r, http.StatusForbidden, httpx.ProblemTypeForbidden, "Forbidden", "err.forbidden")
			default:
				if isWorkflowErr(err) {
					httpx.WriteError(w, r, http.StatusConflict, httpx.ProblemTypeWorkflowState, "Invalid State", "err.workflow_state")
				} else {
					httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
				}
			}
			return
		}
		httpx.WriteJSON(w, http.StatusOK, sess)
	}
}

// RejectHandler — POST /v1/live-view/requests/{requestID}/reject (IT Manager or Admin)
func RejectHandler(svc *Service) http.HandlerFunc {
	type reqBody struct {
		Notes string `json:"notes"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "requestID")
		var body reqBody
		_ = json.NewDecoder(r.Body).Decode(&body)

		if err := svc.Reject(r.Context(), p, id, body.Notes); err != nil {
			httpx.WriteError(w, r, http.StatusForbidden, httpx.ProblemTypeForbidden, "Forbidden", "err.forbidden")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ListSessionsHandler — GET /v1/live-view/sessions
func ListSessionsHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		var statePtr *State
		if s := r.URL.Query().Get("state"); s != "" {
			state := State(s)
			statePtr = &state
		}
		list, err := svc.ListRequests(r.Context(), p.TenantID, statePtr)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"items": list,
			"pagination": map[string]any{"page": 1, "page_size": len(list), "total": len(list)},
		})
	}
}

// GetSessionHandler — GET /v1/live-view/sessions/{sessionID}
func GetSessionHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "sessionID")
		sess, err := svc.GetSession(r.Context(), p.TenantID, id)
		if err != nil {
			httpx.WriteError(w, r, http.StatusNotFound, httpx.ProblemTypeNotFound, "Not Found", "err.not_found")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, sess)
	}
}

// EndSessionHandler — POST /v1/live-view/sessions/{sessionID}/end
func EndSessionHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "sessionID")
		if err := svc.EndSession(r.Context(), p, id); err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// TerminateHandler — POST /v1/live-view/sessions/{sessionID}/terminate (IT Manager, Admin, or DPO compliance override)
func TerminateHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "sessionID")
		if err := svc.Terminate(r.Context(), p, id); err != nil {
			if err == auth.ErrForbidden {
				httpx.WriteError(w, r, http.StatusForbidden, httpx.ProblemTypeForbidden, "Forbidden", "err.forbidden")
			} else {
				httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			}
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func isWorkflowErr(err error) bool {
	return err == ErrInvalidTransition
}
