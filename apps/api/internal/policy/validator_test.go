package policy

import (
	"errors"
	"testing"
)

// TestValidateBundle covers the structural invariants mandated by ADR 0013 A5.
// Table-driven; 6+ cases as required by the quality bar.
func TestValidateBundle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		bundle            *BundleInvariants
		wantErr           error  // non-nil when ValidateBundle must return this exact error
		wantFieldErrKey   string // non-empty when we expect a specific field error key
		wantNoFieldErrors bool   // true when we expect (nil, nil)
	}{
		{
			name: "valid: dlp disabled, content disabled",
			bundle: &BundleInvariants{
				DLPEnabled: false,
				Keystroke:  KeystrokeConfig{ContentEnabled: false},
				Retention:  RetentionConfig{DefaultDays: 90, SensitiveDays: 14},
			},
			wantNoFieldErrors: true,
		},
		{
			name: "valid: dlp enabled, content enabled",
			bundle: &BundleInvariants{
				DLPEnabled: true,
				Keystroke:  KeystrokeConfig{ContentEnabled: true},
				Retention:  RetentionConfig{DefaultDays: 90, SensitiveDays: 14},
			},
			wantNoFieldErrors: true,
		},
		{
			// ADR 0013 A5 invariant: the core safety rule
			name: "INVALID: dlp disabled but content enabled",
			bundle: &BundleInvariants{
				DLPEnabled: false,
				Keystroke:  KeystrokeConfig{ContentEnabled: true},
				Retention:  RetentionConfig{DefaultDays: 90, SensitiveDays: 14},
			},
			wantErr: ErrInvalidInvariantDLPKeystroke,
		},
		{
			name: "INVALID: sensitive retention >= default retention",
			bundle: &BundleInvariants{
				DLPEnabled: false,
				Keystroke:  KeystrokeConfig{ContentEnabled: false},
				Retention:  RetentionConfig{DefaultDays: 90, SensitiveDays: 90},
			},
			wantFieldErrKey: "retention.sensitive_days",
		},
		{
			name: "INVALID: sensitive retention longer than default",
			bundle: &BundleInvariants{
				DLPEnabled: false,
				Keystroke:  KeystrokeConfig{ContentEnabled: false},
				Retention:  RetentionConfig{DefaultDays: 30, SensitiveDays: 60},
			},
			wantFieldErrKey: "retention.sensitive_days",
		},
		{
			name: "INVALID: empty screenshot_exclude_apps entry",
			bundle: &BundleInvariants{
				DLPEnabled:            false,
				Keystroke:             KeystrokeConfig{ContentEnabled: false},
				ScreenshotExcludeApps: []string{"notepad.exe", ""},
			},
			wantFieldErrKey: "screenshot_exclude_apps[1]",
		},
		{
			name: "INVALID: bad regex in window_title_sensitive_regex",
			bundle: &BundleInvariants{
				DLPEnabled:                false,
				Keystroke:                 KeystrokeConfig{ContentEnabled: false},
				WindowTitleSensitiveRegex: []string{"valid.*", "[invalid"},
			},
			wantFieldErrKey: "window_title_sensitive_regex[1]",
		},
		{
			name: "valid: dlp enabled, content disabled (explicit opt-in without content)",
			bundle: &BundleInvariants{
				DLPEnabled: true,
				Keystroke:  KeystrokeConfig{ContentEnabled: false},
				Retention:  RetentionConfig{DefaultDays: 60, SensitiveDays: 14},
			},
			wantNoFieldErrors: true,
		},
		{
			name: "valid: no retention values set (both zero => skip check)",
			bundle: &BundleInvariants{
				DLPEnabled: false,
				Keystroke:  KeystrokeConfig{ContentEnabled: false},
			},
			wantNoFieldErrors: true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fieldErrs, err := ValidateBundle(tc.bundle)

			// Case 1: expect a typed error (invariant violation).
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Errorf("ValidateBundle() error = %v, want %v", err, tc.wantErr)
				}
				return
			}

			// Case 2: expect a specific field error key.
			if tc.wantFieldErrKey != "" {
				if err != nil {
					t.Errorf("ValidateBundle() unexpected error = %v", err)
					return
				}
				if _, ok := fieldErrs[tc.wantFieldErrKey]; !ok {
					t.Errorf("ValidateBundle() missing field error for key %q; got %v", tc.wantFieldErrKey, fieldErrs)
				}
				return
			}

			// Case 3: expect clean validation.
			if tc.wantNoFieldErrors {
				if err != nil {
					t.Errorf("ValidateBundle() unexpected error = %v", err)
				}
				if len(fieldErrs) != 0 {
					t.Errorf("ValidateBundle() unexpected field errors = %v", fieldErrs)
				}
			}
		})
	}
}
