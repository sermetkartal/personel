package mobile

import (
	"encoding/json"
	"net/http"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// GetSummaryHandler — GET /v1/mobile/summary
func GetSummaryHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		resp, err := svc.GetSummary(r.Context(), p.TenantID, p.UserID)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError,
				httpx.ProblemTypeInternal, "Summary Failed", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, resp)
	}
}

// RegisterPushTokenHandler — POST /v1/mobile/push-tokens
func RegisterPushTokenHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())

		var req PushTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "Invalid Body", "err.validation")
			return
		}
		if req.Token == "" || req.Platform == "" || req.DeviceID == "" {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity,
				httpx.ProblemTypeValidation, "Missing Fields", "err.validation")
			return
		}
		if req.Platform != "ios" && req.Platform != "android" {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity,
				httpx.ProblemTypeValidation, "Invalid Platform", "err.validation")
			return
		}

		resp, err := svc.RegisterPushToken(r.Context(), p.UserID, req)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError,
				httpx.ProblemTypeInternal, "Registration Failed", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, resp)
	}
}

// ListPendingLiveViewHandler — GET /v1/mobile/live-view/pending
func ListPendingLiveViewHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		items, err := svc.ListPendingLiveView(r.Context(), p.TenantID)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError,
				httpx.ProblemTypeInternal, "List Failed", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// ListDSRQueueHandler — GET /v1/mobile/dsr/queue
func ListDSRQueueHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		items, err := svc.ListDSRQueue(r.Context(), p.TenantID)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError,
				httpx.ProblemTypeInternal, "List Failed", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// ListSilenceHandler — GET /v1/mobile/silence
func ListSilenceHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		items, err := svc.ListSilenceAlerts(r.Context(), p.TenantID)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError,
				httpx.ProblemTypeInternal, "List Failed", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}
