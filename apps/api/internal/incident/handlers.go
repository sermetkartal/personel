package incident

import (
	"encoding/json"
	"net/http"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// RecordClosureHandler handles POST /v1/system/incident-closures.
// DPO and Admin roles submit after post-incident review.
func RecordClosureHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized,
				httpx.ProblemTypeAuth, "Unauthorized", "err.auth")
			return
		}

		var req IncidentReport
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "Invalid Body", "err.validation")
			return
		}
		req.TenantID = p.TenantID
		if req.LeadResponderID == "" {
			req.LeadResponderID = p.UserID
		}

		id, err := svc.RecordClosure(r.Context(), req)
		if err != nil {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity,
				httpx.ProblemTypeValidation, err.Error(), "err.validation")
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, map[string]any{"evidence_id": id})
	}
}
