// Package policy — validation for policy rules, SensitivityGuard regexes,
// app exclusion lists, and the ADR 0013 A5 policy-bundle invariants.
package policy

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// PolicyRules is the decoded rule set for a policy.
type PolicyRules struct {
	// Basic controls
	ScreenshotIntervalSeconds int      `json:"screenshot_interval_seconds"`
	AppBlockList              []string `json:"app_block_list"`
	URLBlockList              []string `json:"url_block_list"`
	USBBlockEnabled           bool     `json:"usb_block_enabled"`
	KeystrokeEnabled          bool     `json:"keystroke_enabled"`
	ScreenshotEnabled         bool     `json:"screenshot_enabled"`
	NetworkFlowEnabled        bool     `json:"network_flow_enabled"`
	FileEventEnabled          bool     `json:"file_event_enabled"`

	// KVKK SensitivityGuard (mvp-scope.md)
	SensitivityGuard SensitivityGuard `json:"sensitivity_guard"`
}

// SensitivityGuard encodes the KVKK m.6 guard fields.
type SensitivityGuard struct {
	// Regex list applied to window titles; matching events route to sensitive bucket.
	WindowTitleSensitiveRegex []string `json:"window_title_sensitive_regex"`
	// Glob list of hostnames that flag navigation events as sensitive.
	SensitiveHostGlobs []string `json:"sensitive_host_globs"`
	// Process image name globs that suppress screenshot capture when foreground.
	ScreenshotExcludeApps []string `json:"screenshot_exclude_apps"`
	// When true, a kvkk_m6:* DLP match sets the sensitive flag.
	AutoFlagOnM6DLPMatch bool `json:"auto_flag_on_m6_dlp_match"`
}

// ErrInvalidInvariantDLPKeystroke is returned by ValidateBundle when a bundle
// asserts dlp_enabled=false AND keystroke.content_enabled=true, which violates
// the structural invariant mandated by ADR 0013 Amendment A5.
var ErrInvalidInvariantDLPKeystroke = errors.New(
	"policy: invariant violation: keystroke.content_enabled=true requires dlp_enabled=true",
)

// KeystrokeConfig holds DLP-related keystroke settings inside a PolicyBundle.
type KeystrokeConfig struct {
	ContentEnabled bool `json:"content_enabled"`
}

// RetentionConfig holds retention periods. Sensitive retention MUST be shorter
// than default retention.
type RetentionConfig struct {
	DefaultDays   int `json:"default_days"`
	SensitiveDays int `json:"sensitive_days"`
}

// BundleInvariants is the subset of policy fields checked for structural
// invariants before signing. It is separate from PolicyBundle (publisher.go)
// which is the signed wire format — the two are related but not the same type.
// ValidateBundle is called by Service.Push before calling Publisher.
type BundleInvariants struct {
	DLPEnabled bool            `json:"dlp_enabled"`
	Keystroke  KeystrokeConfig `json:"keystroke"`
	Retention  RetentionConfig `json:"retention"`

	// SensitivityGuard inline fields.
	ScreenshotExcludeApps     []string `json:"screenshot_exclude_apps"`
	WindowTitleSensitiveRegex []string `json:"window_title_sensitive_regex"`
}

// ValidateBundle enforces the structural invariants of a PolicyBundle prior to
// signing. Any violation surfaces as a typed error so the handler can return
// HTTP 422 with httpx.WriteValidationError.
//
// Invariants enforced (ADR 0013 A5):
//  1. dlp_enabled=false AND keystroke.content_enabled=true → rejected with
//     ErrInvalidInvariantDLPKeystroke.
//  2. retention.sensitive_days MUST be < retention.default_days when both > 0.
//  3. screenshot_exclude_apps entries must be non-empty process names.
//  4. window_title_sensitive_regex entries must be valid Go regex.
func ValidateBundle(b *BundleInvariants) (map[string]string, error) {
	if b == nil {
		return nil, errors.New("policy: bundle is nil")
	}

	// --- Invariant 1 (ADR 0013 A5) ---
	if !b.DLPEnabled && b.Keystroke.ContentEnabled {
		return nil, ErrInvalidInvariantDLPKeystroke
	}

	errs := make(map[string]string)

	// --- Invariant 2: sensitive retention < default retention ---
	if b.Retention.DefaultDays > 0 && b.Retention.SensitiveDays > 0 {
		if b.Retention.SensitiveDays >= b.Retention.DefaultDays {
			errs["retention.sensitive_days"] = fmt.Sprintf(
				"hassas veri saklama süresi (%d gün) varsayılan saklama süresinden (%d gün) kısa olmalıdır",
				b.Retention.SensitiveDays, b.Retention.DefaultDays,
			)
		}
	}

	// --- Invariant 3: screenshot_exclude_apps must be non-empty process names ---
	for i, entry := range b.ScreenshotExcludeApps {
		if strings.TrimSpace(entry) == "" {
			errs[fmt.Sprintf("screenshot_exclude_apps[%d]", i)] = "uygulama adı boş olamaz"
		}
	}

	// --- Invariant 4: window_title_sensitive_regex must be valid Go regex ---
	for i, pattern := range b.WindowTitleSensitiveRegex {
		if _, err := regexp.Compile(pattern); err != nil {
			errs[fmt.Sprintf("window_title_sensitive_regex[%d]", i)] =
				fmt.Sprintf("geçersiz regex: %v", err)
		}
	}

	if len(errs) > 0 {
		return errs, nil
	}
	return nil, nil
}

// Validate validates the policy rules JSON and returns field-level errors.
func Validate(rulesJSON json.RawMessage) (map[string]string, error) {
	var rules PolicyRules
	if err := json.Unmarshal(rulesJSON, &rules); err != nil {
		return nil, fmt.Errorf("policy: rules JSON: %w", err)
	}

	errs := make(map[string]string)

	// Validate SensitivityGuard regexes.
	for i, pattern := range rules.SensitivityGuard.WindowTitleSensitiveRegex {
		if _, err := regexp.Compile(pattern); err != nil {
			errs[fmt.Sprintf("sensitivity_guard.window_title_sensitive_regex[%d]", i)] =
				fmt.Sprintf("invalid regex: %v", err)
		}
	}

	// Validate screenshot interval.
	if rules.ScreenshotEnabled && rules.ScreenshotIntervalSeconds < 10 {
		errs["screenshot_interval_seconds"] = "minimum interval is 10 seconds"
	}
	if rules.ScreenshotIntervalSeconds > 3600 {
		errs["screenshot_interval_seconds"] = "maximum interval is 3600 seconds"
	}

	// Validate app block list entries are non-empty strings.
	for i, entry := range rules.AppBlockList {
		if strings.TrimSpace(entry) == "" {
			errs[fmt.Sprintf("app_block_list[%d]", i)] = "entry must not be empty"
		}
	}

	// Validate screenshot_exclude_apps are non-empty glob patterns.
	for i, entry := range rules.SensitivityGuard.ScreenshotExcludeApps {
		if strings.TrimSpace(entry) == "" {
			errs[fmt.Sprintf("sensitivity_guard.screenshot_exclude_apps[%d]", i)] = "entry must not be empty"
		}
	}

	if len(errs) > 0 {
		return errs, nil
	}
	return nil, nil
}
