// Package policy — validation for policy rules, SensitivityGuard regexes,
// and app exclusion lists.
package policy

import (
	"encoding/json"
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
