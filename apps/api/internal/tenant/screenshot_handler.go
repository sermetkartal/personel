// Package tenant — screenshot preset handler.
//
// Endpoints:
//   GET   /v1/tenants/me/screenshot-preset   → current preset
//   PATCH /v1/tenants/me/screenshot-preset   → update preset
//
// The preset controls agent-side screen capture footprint via a single
// named profile (minimal / low / medium / high / max). The chosen value
// is written to `tenants.screenshot_preset` (migration 0037) and the
// agent reads it at boot time via the `PERSONEL_SCREENSHOT_PRESET` env
// var wired in `personel-agent::service`. Once PolicyPush apply lands
// agent-side, the same preset also flows through the live bundle path.
//
// RBAC: admin + it_manager only. Hidden/disabled for everyone else.
// Audit: every PATCH writes `tenant.screenshot_preset.update` with
// before+after values.
package tenant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/auth"
	"github.com/personel/api/internal/httpx"
)

// DefaultScreenshotPreset is returned when the column is NULL.
const DefaultScreenshotPreset = "high"

// validScreenshotPresets is the canonical allow-list. Any other value is
// rejected with 400 Bad Request.
var validScreenshotPresets = map[string]struct{}{
	"minimal": {},
	"low":     {},
	"medium":  {},
	"high":    {},
	"max":     {},
}

// GetScreenshotPreset returns the active screenshot preset for the given
// tenant, or the `DefaultScreenshotPreset` when the column is NULL.
func (s *Service) GetScreenshotPreset(ctx context.Context, tenantID string) (string, error) {
	var preset *string
	err := s.pool.QueryRow(ctx,
		`SELECT screenshot_preset FROM tenants WHERE id = $1::uuid`, tenantID,
	).Scan(&preset)
	if err != nil {
		return "", fmt.Errorf("tenant: get screenshot preset: %w", err)
	}
	if preset == nil || *preset == "" {
		return DefaultScreenshotPreset, nil
	}
	return *preset, nil
}

// UpdateScreenshotPreset validates + persists a new preset and writes an
// audit entry. Returns the old and new values on success.
func (s *Service) UpdateScreenshotPreset(
	ctx context.Context, tenantID, actorID, newPreset string,
) (oldPreset string, err error) {
	if _, ok := validScreenshotPresets[newPreset]; !ok {
		return "", fmt.Errorf("tenant: invalid screenshot preset: %q", newPreset)
	}
	oldPreset, err = s.GetScreenshotPreset(ctx, tenantID)
	if err != nil {
		return "", err
	}
	if _, err = s.pool.Exec(ctx,
		`UPDATE tenants SET screenshot_preset = $1, updated_at = now() WHERE id = $2::uuid`,
		newPreset, tenantID,
	); err != nil {
		return "", fmt.Errorf("tenant: update screenshot preset: %w", err)
	}
	if _, err = s.recorder.Append(ctx, audit.Entry{
		Actor:    actorID,
		TenantID: tenantID,
		Action:   audit.ActionTenantScreenshotPreset,
		Target:   "tenant:" + tenantID,
		Details: map[string]any{
			"before": oldPreset,
			"after":  newPreset,
		},
	}); err != nil {
		// Persisted update is authoritative; audit failure is logged but
		// does not roll back — matches the rest of the service.
		s.log.Warn("tenant: audit append failed",
			"error", err, "action", "tenant.screenshot_preset.update")
	}
	return oldPreset, nil
}

// GetScreenshotPresetHandler is GET /v1/tenants/me/screenshot-preset.
// Any authenticated user in the tenant can read the current value —
// only the PATCH path is gated on admin/it_manager.
func GetScreenshotPresetHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil || p.TenantID == "" {
			httpx.WriteError(w, r, http.StatusUnauthorized,
				httpx.ProblemTypeAuth, "Unauthorized", "err.auth")
			return
		}
		preset, err := svc.GetScreenshotPreset(r.Context(), p.TenantID)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError,
				httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{"preset": preset})
	}
}

// PatchScreenshotPresetHandler is PATCH /v1/tenants/me/screenshot-preset.
// Route should be mounted behind the admin+it_manager role gate.
func PatchScreenshotPresetHandler(svc *Service) http.HandlerFunc {
	type reqBody struct {
		Preset string `json:"preset"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		p := auth.PrincipalFromContext(r.Context())
		if p == nil || p.TenantID == "" {
			httpx.WriteError(w, r, http.StatusUnauthorized,
				httpx.ProblemTypeAuth, "Unauthorized", "err.auth")
			return
		}
		var body reqBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Preset == "" {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "Bad Request", "err.validation")
			return
		}
		if _, ok := validScreenshotPresets[body.Preset]; !ok {
			httpx.WriteError(w, r, http.StatusBadRequest,
				httpx.ProblemTypeValidation, "Invalid preset", "err.validation")
			return
		}
		oldPreset, err := svc.UpdateScreenshotPreset(r.Context(), p.TenantID, p.UserID, body.Preset)
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError,
				httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"preset":        body.Preset,
			"previous":      oldPreset,
			"valid_presets": []string{"minimal", "low", "medium", "high", "max"},
		})
	}
}
