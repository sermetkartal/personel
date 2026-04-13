// export_handler.go — HTTP handler for POST /v1/reports/export.
//
// RBAC: admin, manager, hr, dpo, investigator. Same gate as the CH handlers
// — export is a read-only surface over the same aggregations.
//
// Request body:
//   { "report_type": "top_apps", "format": "pdf", "params": { ... } }
package reports

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// ExportHandler wires the Exporter into an HTTP endpoint.
type ExportHandler struct {
	exp *Exporter
}

// NewExportHandler constructs the handler.
func NewExportHandler(exp *Exporter) *ExportHandler {
	return &ExportHandler{exp: exp}
}

// Post serves POST /v1/reports/export.
func (h *ExportHandler) Post(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFromContext(r.Context())
	if p == nil || p.TenantID == "" {
		httpx.WriteError(w, r, http.StatusUnauthorized, httpx.ProblemTypeAuth, "Unauthorized", "err.auth")
		return
	}
	if !(auth.HasRole(p, auth.RoleAdmin) ||
		auth.HasRole(p, auth.RoleManager) ||
		auth.HasRole(p, auth.RoleHR) ||
		auth.HasRole(p, auth.RoleDPO) ||
		auth.HasRole(p, auth.RoleInvestigator)) {
		httpx.WriteError(w, r, http.StatusForbidden, httpx.ProblemTypeForbidden, "Forbidden", "err.forbidden")
		return
	}

	var body ExportRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "invalid JSON body", "err.validation")
		return
	}

	if body.ReportType == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "report_type is required", "err.validation")
		return
	}
	if body.Format == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "format is required", "err.validation")
		return
	}

	res, err := h.exp.Export(r.Context(), p, body)
	if err != nil {
		switch {
		case errors.Is(err, ErrUnknownReport) || errors.Is(err, ErrUnknownFormat):
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, err.Error(), "err.validation")
		case errors.Is(err, ErrExportTooLarge):
			httpx.WriteError(w, r, http.StatusRequestEntityTooLarge, httpx.ProblemTypeValidation, err.Error(), "err.too_large")
		case errors.Is(err, ErrRiskUnavailable):
			httpx.WriteError(w, r, http.StatusServiceUnavailable, httpx.ProblemTypeInternal, "risk scoring not available", "err.unavailable")
		default:
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "export failed", "err.internal")
		}
		return
	}

	httpx.WriteJSON(w, http.StatusOK, res)
}
