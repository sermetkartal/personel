// trends_handler.go — HTTP handler for GET /v1/reports/trends
//
// Route contract:
//   GET /v1/reports/trends?metric=<name>&window=<days>
//
// RBAC: admin, manager, hr, dpo. (investigator NOT allowed — trends are
// rolled-up aggregate analytics that fall under KVKK m.5 proportionality
// rather than investigation scope.)
//
// Response shape: { "result": TrendResult } on success, RFC7807 problem+json on error.
package reports

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// TrendsHandler wires the TrendService into an HTTP endpoint.
type TrendsHandler struct {
	svc *TrendService
}

// NewTrendsHandler constructs the handler.
func NewTrendsHandler(svc *TrendService) *TrendsHandler {
	return &TrendsHandler{svc: svc}
}

// Get serves GET /v1/reports/trends.
func (h *TrendsHandler) Get(w http.ResponseWriter, r *http.Request) {
	p := auth.PrincipalFromContext(r.Context())
	if p == nil || p.TenantID == "" {
		httpx.WriteError(w, r, http.StatusUnauthorized, httpx.ProblemTypeAuth, "Unauthorized", "err.auth")
		return
	}

	// Role gate.
	if !(auth.HasRole(p, auth.RoleAdmin) ||
		auth.HasRole(p, auth.RoleManager) ||
		auth.HasRole(p, auth.RoleHR) ||
		auth.HasRole(p, auth.RoleDPO)) {
		httpx.WriteError(w, r, http.StatusForbidden, httpx.ProblemTypeForbidden, "Forbidden", "err.forbidden")
		return
	}

	metric := MetricName(r.URL.Query().Get("metric"))
	if metric == "" {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "metric is required", "err.validation")
		return
	}
	if _, ok := allMetrics[metric]; !ok {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "unknown metric", "err.validation")
		return
	}

	windowStr := r.URL.Query().Get("window")
	if windowStr == "" {
		windowStr = "7"
	}
	window, err := strconv.Atoi(windowStr)
	if err != nil {
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "window must be an integer", "err.validation")
		return
	}

	res, err := h.svc.TrendReport(r.Context(), p.TenantID, metric, window)
	if err != nil {
		switch {
		case errors.Is(err, ErrUnknownMetric) || errors.Is(err, ErrInvalidWindow):
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, err.Error(), "err.validation")
		case errors.Is(err, ErrInsufficientData):
			// 409 — the request was structurally valid but the DB does not
			// yet have enough history. The caller should retry later or
			// reduce `window`. Not a 500.
			httpx.WriteError(w, r, http.StatusConflict, httpx.ProblemTypeConflict, "insufficient history for trend", "err.insufficient_data")
		default:
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "trend query failed", "err.internal")
		}
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{"result": res})
}
