package backup

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

func writeErr(w http.ResponseWriter, r *http.Request, status int, msg string) {
	httpx.WriteError(w, r, status, httpx.ProblemTypeValidation, msg, "err.validation")
}

// ListTargetsHandler handles GET /v1/settings/backup/targets.
func ListTargetsHandler(svc *TargetService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		items, err := svc.ListTargets(r.Context(), p.TenantID)
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

// CreateTargetHandler handles POST /v1/settings/backup/targets.
func CreateTargetHandler(svc *TargetService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		var req CreateTargetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, r, http.StatusBadRequest, "invalid body")
			return
		}
		rec, err := svc.CreateTarget(r.Context(), p.UserID, p.TenantID, req)
		if err != nil {
			status := http.StatusUnprocessableEntity
			switch {
			case errors.Is(err, ErrUnknownKind):
				status = http.StatusBadRequest
			case errors.Is(err, ErrVaultUnavailable):
				status = http.StatusServiceUnavailable
			}
			writeErr(w, r, status, err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, rec)
	}
}

// UpdateTargetHandler handles PATCH /v1/settings/backup/targets/{id}.
func UpdateTargetHandler(svc *TargetService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeErr(w, r, http.StatusBadRequest, "invalid target id")
			return
		}
		var req UpdateTargetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, r, http.StatusBadRequest, "invalid body")
			return
		}
		if err := svc.UpdateTarget(r.Context(), p.UserID, p.TenantID, id, req); err != nil {
			status := http.StatusUnprocessableEntity
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				status = http.StatusNotFound
			case errors.Is(err, ErrVaultUnavailable):
				status = http.StatusServiceUnavailable
			}
			writeErr(w, r, status, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// DeleteTargetHandler handles DELETE /v1/settings/backup/targets/{id}.
func DeleteTargetHandler(svc *TargetService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeErr(w, r, http.StatusBadRequest, "invalid target id")
			return
		}
		if err := svc.DeleteTarget(r.Context(), p.UserID, p.TenantID, id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeErr(w, r, http.StatusNotFound, "not found")
				return
			}
			writeErr(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// TriggerRunHandler handles POST /v1/settings/backup/targets/{id}/run.
// Body: {"kind": "full"|"incremental"}.
func TriggerRunHandler(svc *TargetService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeErr(w, r, http.StatusBadRequest, "invalid target id")
			return
		}
		var body struct {
			Kind string `json:"kind"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, r, http.StatusBadRequest, "invalid body")
			return
		}
		if body.Kind == "" {
			body.Kind = "full"
		}
		rec, err := svc.TriggerRun(r.Context(), p.UserID, p.TenantID, id, body.Kind)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeErr(w, r, http.StatusNotFound, "target not found")
				return
			}
			writeErr(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusAccepted, rec)
	}
}

// ListRunsHandler handles GET /v1/settings/backup/targets/{id}/runs.
func ListRunsHandler(svc *TargetService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			writeErr(w, r, http.StatusUnauthorized, "unauthenticated")
			return
		}
		id, err := uuid.Parse(chi.URLParam(r, "id"))
		if err != nil {
			writeErr(w, r, http.StatusBadRequest, "invalid target id")
			return
		}
		runs, err := svc.ListRuns(r.Context(), p.TenantID, id)
		if err != nil {
			writeErr(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": runs})
	}
}
