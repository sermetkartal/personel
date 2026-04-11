package dlpstate

import (
	"context"
	"net/http"

	"github.com/personel/api/internal/httpx"
)

// ModuleStatus is a uniform state descriptor for a Phase 1/2 optional
// module. The shape is the same for every module so the admin console
// and transparency portal can render a generic "module state" UI without
// one fetch per module.
//
// Phase 1 modules: dlp.
// Phase 2 modules (reserved): ocr, ml, livrec (live view recording), hris.
type ModuleStatus struct {
	// Name is the module identifier (e.g. "dlp", "ocr", "ml").
	Name string `json:"name"`

	// State is the machine-readable state. Values vary per module but share
	// the convention "disabled" | "enabling" | "enabled" | "disabling" | "error".
	State string `json:"state"`

	// Enabled is a boolean convenience derived from State.
	Enabled bool `json:"enabled"`

	// EnabledAt is the ISO8601 timestamp when the module entered its
	// current enabled state, or nil.
	EnabledAt *string `json:"enabled_at"`

	// EnabledBy is the actor who activated the module, or nil.
	EnabledBy *string `json:"enabled_by"`

	// Message is a human-readable Turkish summary shown in the UI.
	Message string `json:"message"`

	// Metadata is module-specific data. For DLP this includes the ceremony
	// form hash and Vault Secret ID presence; for OCR it would include the
	// selected OCR engine and language packs.
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ModuleStateResponse is the top-level payload for GET /v1/system/module-state.
// The map is keyed by module name; callers can pick the modules they care
// about without separate roundtrips.
type ModuleStateResponse struct {
	Modules map[string]ModuleStatus `json:"modules"`
}

// GetModuleState assembles the ModuleStateResponse by collecting each
// module's status. Phase 1 only has DLP real state; Phase 2 will add
// OCR, ML, LIVREC, HRIS. Phase 2 implementers should refactor this into
// a ModuleStatusSource interface rather than hard-coding every module.
func (s *Service) GetModuleState(ctx context.Context) (*ModuleStateResponse, error) {
	resp := &ModuleStateResponse{
		Modules: make(map[string]ModuleStatus),
	}

	// --- DLP (Phase 1 — real state) ---
	dlpStatus, err := s.GetStatus(ctx)
	if err != nil {
		return nil, err
	}
	dlpMeta := map[string]any{
		"vault_secret_id_present": dlpStatus.VaultSecretIDPresent,
		"container_health":        string(dlpStatus.ContainerHealth),
		"last_audit_event_id":     dlpStatus.LastAuditEventID,
	}
	if dlpStatus.CeremonyFormHash != nil {
		dlpMeta["ceremony_form_hash"] = *dlpStatus.CeremonyFormHash
	}
	resp.Modules["dlp"] = ModuleStatus{
		Name:      "dlp",
		State:     string(dlpStatus.State),
		Enabled:   dlpStatus.State == StateEnabled,
		EnabledAt: dlpStatus.EnabledAt,
		EnabledBy: dlpStatus.EnabledBy,
		Message:   dlpStatus.Message,
		Metadata:  dlpMeta,
	}

	// --- OCR (Phase 2 placeholder) ---
	resp.Modules["ocr"] = ModuleStatus{
		Name:    "ocr",
		State:   "disabled",
		Enabled: false,
		Message: "OCR modülü Faz 2'de sunulacak (ADR pending).",
	}

	// --- ML category classifier (Phase 2 placeholder — ADR 0017) ---
	resp.Modules["ml"] = ModuleStatus{
		Name:    "ml",
		State:   "disabled",
		Enabled: false,
		Message: "ML kategori sınıflandırıcı Faz 2'de sunulacak (ADR 0017).",
	}

	// --- Live view recording (Phase 2 placeholder — ADR 0019) ---
	resp.Modules["livrec"] = ModuleStatus{
		Name:    "livrec",
		State:   "disabled",
		Enabled: false,
		Message: "Canlı izleme kayıt özelliği Faz 2'de sunulacak (ADR 0019).",
	}

	// --- HRIS sync (Phase 2 placeholder — ADR 0018) ---
	resp.Modules["hris"] = ModuleStatus{
		Name:    "hris",
		State:   "disabled",
		Enabled: false,
		Message: "HRIS entegrasyonu Faz 2'de sunulacak (ADR 0018).",
	}

	return resp, nil
}

// GetModuleStateHandler — GET /v1/system/module-state
//
// Returns the state of every Phase 1 and reserved-Phase-2 module in a
// single response. Readable by all authenticated roles. Phase 2 will
// add real sources for ocr, ml, livrec, hris. Portals and consoles
// should prefer this endpoint over /dlp-state for new code.
func GetModuleStateHandler(svc *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp, err := svc.GetModuleState(r.Context())
		if err != nil {
			httpx.WriteError(w, r, http.StatusInternalServerError,
				httpx.ProblemTypeInternal, "Internal Error", "err.internal")
			return
		}
		httpx.WriteJSON(w, http.StatusOK, resp)
	}
}
