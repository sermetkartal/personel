package settings

import "testing"

// --- validateCaMode ---

func TestValidateCaModeRejectsUnknown(t *testing.T) {
	err := validateCaMode(UpdateCaModeRequest{Mode: "vanilla"})
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
}

func TestValidateCaModeLetsencryptRequiresConfig(t *testing.T) {
	if err := validateCaMode(UpdateCaModeRequest{Mode: CaModeLetsencrypt}); err == nil {
		t.Fatal("expected error for missing letsencrypt config")
	}
	err := validateCaMode(UpdateCaModeRequest{Mode: CaModeLetsencrypt, Config: map[string]any{"dns_provider": "cloudflare"}})
	if err == nil {
		t.Fatal("expected error for missing email")
	}
	err = validateCaMode(UpdateCaModeRequest{Mode: CaModeLetsencrypt, Config: map[string]any{"dns_provider": "cf", "email": "dpo@example.com"}})
	if err != nil {
		t.Fatalf("expected OK, got %v", err)
	}
}

func TestValidateCaModeCommercialRequiresKeys(t *testing.T) {
	if err := validateCaMode(UpdateCaModeRequest{Mode: CaModeCommercial, Config: map[string]any{"csr_key": "k"}}); err == nil {
		t.Fatal("expected error for missing cert_chain_key")
	}
	err := validateCaMode(UpdateCaModeRequest{Mode: CaModeCommercial, Config: map[string]any{"csr_key": "k", "cert_chain_key": "v"}})
	if err != nil {
		t.Fatalf("expected OK, got %v", err)
	}
}

func TestValidateCaModeInternalAcceptsEmpty(t *testing.T) {
	if err := validateCaMode(UpdateCaModeRequest{Mode: CaModeInternal}); err != nil {
		t.Fatalf("internal with no config should be OK, got %v", err)
	}
}

// --- validateRetention ---

func TestValidateRetentionAcceptsDefault(t *testing.T) {
	if err := validateRetention(DefaultRetentionPolicy); err != nil {
		t.Fatalf("default should pass, got %v", err)
	}
}

func TestValidateRetentionRejectsBelowAuditFloor(t *testing.T) {
	bad := DefaultRetentionPolicy
	bad.AuditYears = 4
	if err := validateRetention(bad); err == nil {
		t.Fatal("expected error for audit_years < 5")
	}
}

func TestValidateRetentionRejectsBelowKeystrokeFloor(t *testing.T) {
	bad := DefaultRetentionPolicy
	bad.KeystrokeDays = 100
	if err := validateRetention(bad); err == nil {
		t.Fatal("expected error for keystroke_days < 180")
	}
}

func TestValidateRetentionRejectsBelowDsrFloor(t *testing.T) {
	bad := DefaultRetentionPolicy
	bad.DsrDays = 365
	if err := validateRetention(bad); err == nil {
		t.Fatal("expected error for dsr_days < 3650")
	}
}

func TestValidateRetentionRejectsBelowEventFloor(t *testing.T) {
	bad := DefaultRetentionPolicy
	bad.EventDays = 30
	if err := validateRetention(bad); err == nil {
		t.Fatal("expected error for event_days < 365")
	}
}

func TestValidateRetentionRejectsBelowScreenshotFloor(t *testing.T) {
	bad := DefaultRetentionPolicy
	bad.ScreenshotDays = 7
	if err := validateRetention(bad); err == nil {
		t.Fatal("expected error for screenshot_days < 30")
	}
}

func TestValidateRetentionAllowsLiveViewZero(t *testing.T) {
	ok := DefaultRetentionPolicy
	ok.LiveViewDays = 0
	if err := validateRetention(ok); err != nil {
		t.Fatalf("live_view_days=0 should be allowed, got %v", err)
	}
}

func TestValidateRetentionRejectsNegativeLiveView(t *testing.T) {
	bad := DefaultRetentionPolicy
	bad.LiveViewDays = -1
	if err := validateRetention(bad); err == nil {
		t.Fatal("expected error for negative live_view_days")
	}
}
