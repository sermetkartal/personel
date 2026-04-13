// Package endpoint — HTTP handlers for remote command operations.
package endpoint

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// wipeRequest is the POST /v1/endpoints/{id}/wipe body.
type wipeRequest struct {
	Reason string `json:"reason"`
}

// WipeHandler issues a crypto-erase command to a single endpoint.
func WipeHandler(svc *CommandService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "endpointID")

		var req wipeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Invalid JSON", "err.validation")
			return
		}
		if strings.TrimSpace(req.Reason) == "" {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Reason required", "err.validation")
			return
		}

		cmd, err := svc.IssueWipe(r.Context(), p, id, strings.TrimSpace(req.Reason))
		if err != nil {
			writeCommandError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusAccepted, cmd)
	}
}

// DeactivateHandler issues a deactivate command to a single endpoint.
func DeactivateHandler(svc *CommandService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "endpointID")

		var req wipeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Invalid JSON", "err.validation")
			return
		}
		if strings.TrimSpace(req.Reason) == "" {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Reason required", "err.validation")
			return
		}

		cmd, err := svc.IssueDeactivate(r.Context(), p, id, strings.TrimSpace(req.Reason))
		if err != nil {
			writeCommandError(w, r, err)
			return
		}
		httpx.WriteJSON(w, http.StatusAccepted, cmd)
	}
}

// bulkRequest is the POST /v1/endpoints/bulk body. Operation must be
// one of wipe / deactivate / revoke. EndpointIDs is capped at BulkLimit.
type bulkRequest struct {
	Operation   string   `json:"operation"`
	EndpointIDs []string `json:"endpoint_ids"`
	Reason      string   `json:"reason"`
}

// BulkHandler fans out an operation over up to BulkLimit endpoints.
// Returns 207 Multi-Status-shaped JSON body with per-endpoint results.
// Note: status code is 200 with per-row outcomes; admins who want a
// strict all-or-nothing semantic should call the per-endpoint handlers.
func BulkHandler(svc *CommandService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())

		var req bulkRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Invalid JSON", "err.validation")
			return
		}
		req.Operation = strings.TrimSpace(req.Operation)
		req.Reason = strings.TrimSpace(req.Reason)
		if req.Operation == "" || req.Reason == "" || len(req.EndpointIDs) == 0 {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "operation, reason, endpoint_ids required", "err.validation")
			return
		}

		results, err := svc.BulkOperation(r.Context(), p, req.Operation, req.EndpointIDs, req.Reason)
		if err != nil && results == nil {
			writeCommandError(w, r, err)
			return
		}

		var successCount, failCount int
		for _, rr := range results {
			if rr.Success {
				successCount++
			} else {
				failCount++
			}
		}

		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"operation": req.Operation,
			"total":     len(req.EndpointIDs),
			"success":   successCount,
			"failed":    failCount,
			"results":   results,
		})
	}
}

// ListCommandsHandler returns the most recent commands issued to a
// specific endpoint. Used by the console endpoint detail page to render
// a command history panel.
func ListCommandsHandler(svc *CommandService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		id := chi.URLParam(r, "endpointID")
		cmds, err := svc.ListCommandsByEndpoint(r.Context(), p.TenantID, id, 100)
		if err != nil {
			if errors.Is(err, ErrEndpointNotFound) {
				httpx.WriteError(w, r, http.StatusNotFound, httpx.ProblemTypeNotFound, "Not Found", "err.not_found")
				return
			}
			httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"items": cmds,
		})
	}
}

// ackRequest is the body of /v1/internal/commands/{id}/ack. Called by
// the in-cluster gateway to report agent acknowledgement or terminal
// completion. The tenant_id is part of the body (not a header) so the
// gateway can route without needing a JWT.
type ackRequest struct {
	TenantID string `json:"tenant_id"`
	NewState string `json:"new_state"`
	Error    string `json:"error,omitempty"`
}

// InternalAckHandler is mounted under an internal-only route group
// protected by InternalTokenMiddleware. It does NOT require a user
// JWT — the gateway is in-cluster trusted.
func InternalAckHandler(svc *CommandService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "commandID")

		var req ackRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Invalid JSON", "err.validation")
			return
		}
		if req.TenantID == "" || req.NewState == "" {
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "tenant_id and new_state required", "err.validation")
			return
		}

		if err := svc.Acknowledge(r.Context(), req.TenantID, id, req.NewState, req.Error); err != nil {
			if errors.Is(err, ErrEndpointNotFound) {
				httpx.WriteError(w, r, http.StatusNotFound, httpx.ProblemTypeNotFound, "Not Found", "err.not_found")
				return
			}
			httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, err.Error(), "err.validation")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// writeCommandError maps domain sentinel errors to HTTP responses with
// canonical RFC 7807 problem types.
func writeCommandError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrEndpointNotFound):
		httpx.WriteError(w, r, http.StatusNotFound, httpx.ProblemTypeNotFound, "Not Found", "err.not_found")
	case errors.Is(err, ErrReasonRequired):
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Reason required", "err.validation")
	case errors.Is(err, ErrUnderLegalHold):
		httpx.WriteError(w, r, http.StatusConflict, httpx.ProblemTypeConflict, "Under legal hold", "err.conflict")
	case errors.Is(err, ErrBulkLimitExceeded):
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Too many endpoints", "err.validation")
	case errors.Is(err, ErrUnknownOperation):
		httpx.WriteError(w, r, http.StatusBadRequest, httpx.ProblemTypeValidation, "Unknown operation", "err.validation")
	case errors.Is(err, ErrPublishFailed):
		httpx.WriteError(w, r, http.StatusServiceUnavailable, httpx.ProblemTypeInternal, "Publish failed", "err.internal")
	default:
		httpx.WriteError(w, r, http.StatusInternalServerError, httpx.ProblemTypeInternal, "Internal Error", "err.internal")
	}
}
