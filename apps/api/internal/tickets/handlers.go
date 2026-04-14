// Package tickets — HTTP handlers for POST /v1/tickets and
// POST /v1/tickets/webhook.
package tickets

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// CreateHandler — POST /v1/tickets
//
// Gated to admin + it_manager roles. Body is the CreateRequest
// struct. Returns the created Ticket with provider_id populated (or
// empty if the provider stub was used).
func CreateHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized,
				httpx.ProblemTypeAuth, "Unauthenticated", "err.unauthenticated")
			return
		}

		var body CreateRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "Invalid Body", "err.validation")
			return
		}

		// Default tenant to the caller's principal tenant when absent.
		if body.TenantID == uuid.Nil {
			if parsed, err := uuid.Parse(p.TenantID); err == nil {
				body.TenantID = parsed
			}
		}

		t, err := svc.Create(r.Context(), p.UserID, body)
		if err != nil {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity,
				httpx.ProblemTypeValidation, err.Error(), "err.tickets.create")
			return
		}
		httpx.WriteJSON(w, http.StatusCreated, t)
	}
}

// StateChangeRequest is the body for POST /v1/tickets/{id}/state.
type StateChangeRequest struct {
	State  State  `json:"state"`
	Reason string `json:"reason,omitempty"`
}

// UpdateStateHandler — POST /v1/tickets/{id}/state
func UpdateStateHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil {
			httpx.WriteError(w, r, http.StatusUnauthorized,
				httpx.ProblemTypeAuth, "Unauthenticated", "err.unauthenticated")
			return
		}

		idStr := chi.URLParam(r, "id")
		if idStr == "" {
			// Unit test fallback — handler called directly without chi router
			idStr = r.URL.Query().Get("id")
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "invalid ticket id", "err.validation")
			return
		}

		var body StateChangeRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "Invalid Body", "err.validation")
			return
		}

		if err := svc.UpdateState(r.Context(), p.UserID, id, body.State, body.Reason); err != nil {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity,
				httpx.ProblemTypeValidation, err.Error(), "err.tickets.state")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// WebhookHandler — POST /v1/tickets/webhook?provider=jira
//
// This endpoint is NOT gated behind the bearer-token auth the rest
// of /v1/* uses, because it receives callbacks from external systems
// that authenticate with HMAC signatures. The real implementation
// MUST verify the provider-specific signature header before trusting
// the payload.
//
// In scaffold mode the handler reads the provider name from the
// query string and the raw body is forwarded to the provider's
// HandleWebhook method. Each provider stub returns ErrNotConfigured
// so the endpoint effectively 422s until a real adapter lands.
func WebhookHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		providerName := r.URL.Query().Get("provider")
		if providerName == "" {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "provider query param required",
				"err.validation")
			return
		}

		// TODO: HMAC signature verification per provider
		// (x-hub-signature-256 for Jira, X-Zendesk-Webhook-Signature,
		// X-Freshdesk-Signature). For now the stub accepts any body.

		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MiB cap
		if err != nil {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "read body failed", "err.validation")
			return
		}

		if err := svc.HandleWebhook(r.Context(), providerName, body); err != nil {
			httpx.WriteError(w, r, http.StatusUnprocessableEntity,
				httpx.ProblemTypeValidation, err.Error(), "err.tickets.webhook")
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}
}
