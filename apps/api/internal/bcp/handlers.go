package bcp

import (
	"encoding/json"
	"net/http"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// RecordDrillHandler handles POST /v1/system/bcp-drills.
// Admin role submits after a live drill or tabletop exercise.
func RecordDrillHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized,
				httpx.ProblemTypeAuth, "Unauthorized", "err.auth")
			return
		}

		var req DrillReport
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "Invalid Body", "err.validation")
			return
		}
		req.TenantID = p.TenantID
		if req.FacilitatorID == "" {
			req.FacilitatorID = p.UserID
		}

		id, err := svc.RecordDrill(r.Context(), req)
		if err != nil {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity,
				httpx.ProblemTypeValidation, err.Error(), "err.validation")
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, map[string]any{"evidence_id": id})
	}
}
