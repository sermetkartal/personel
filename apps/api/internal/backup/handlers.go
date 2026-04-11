package backup

import (
	"encoding/json"
	"net/http"

	"github.com/personel/api/internal/httpx"
)

// RecordRunHandler handles POST /v1/system/backup-runs.
//
// Expected caller: the out-of-API backup runner (systemd timer + script).
// The runner authenticates using a service-account bearer token sourced
// from systemd credentials. Router gates this endpoint on the admin role
// until a dedicated "system" role is introduced.
func RecordRunHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req RunReport
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "Invalid Body", "err.validation")
			return
		}

		id, err := svc.RecordRun(r.Context(), req)
		if err != nil {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity,
				httpx.ProblemTypeValidation, err.Error(), "err.validation")
			return
		}

		// Return the evidence ID (empty in scaffold mode) so the runner
		// can log it alongside the backup artifact.
		httpx.WriteJSON(w, http.StatusCreated, map[string]any{
			"evidence_id": id,
		})
	}
}
