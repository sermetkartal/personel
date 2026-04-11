package accessreview

import (
	"encoding/json"
	"net/http"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// RecordReviewHandler handles POST /v1/system/access-reviews.
// DPO and Admin roles can submit. The request body is a ReviewReport.
func RecordReviewHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized,
				httpx.ProblemTypeAuth, "Unauthorized", "err.auth")
			return
		}

		var req ReviewReport
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "Invalid Body", "err.validation")
			return
		}
		// Force the request's tenant_id to match the authenticated principal
		// so a DPO cannot submit a review against a different tenant.
		req.TenantID = p.TenantID
		if req.ReviewerID == "" {
			req.ReviewerID = p.UserID
		}

		id, err := svc.RecordReview(r.Context(), req)
		if err != nil {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity,
				httpx.ProblemTypeValidation, err.Error(), "err.validation")
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, map[string]any{"evidence_id": id})
	}
}
