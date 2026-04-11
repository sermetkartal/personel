package enricher

import (
	"regexp"
	"strings"

	personelv1 "github.com/personel/proto/personel/v1"
)

// ApplySensitivity evaluates the SensitivityGuard policy against the enriched
// event and sets Sensitive=true when any rule matches. Called during enrichment
// before routing.
//
// Rules applied (from data-retention-matrix.md §Sensitive-Flagged and policy.proto):
//  1. window_title_sensitive_regex — match against window title in payload.
//  2. sensitive_host_globs — match against destination host in network events.
//  3. screenshot_exclude_apps — note: this suppresses capture at the agent level;
//     server-side we just honour the flag if present.
func ApplySensitivity(ev *EnrichedEvent, guard *personelv1.SensitivityGuard) {
	if guard == nil {
		return
	}

	eventType := ev.Event.GetMeta().GetEventType()

	// Rule 1: window title regex check.
	if len(guard.WindowTitleSensitiveRegex) > 0 {
		title := extractWindowTitle(ev)
		if title != "" && matchesAnyRegex(title, guard.WindowTitleSensitiveRegex) {
			ev.Sensitive = true
			return
		}
	}

	// Rule 2: sensitive host globs on network events.
	if len(guard.SensitiveHostGlobs) > 0 {
		if isNetworkEvent(eventType) {
			host := extractNetworkHost(ev)
			if host != "" && matchesAnyGlob(host, guard.SensitiveHostGlobs) {
				ev.Sensitive = true
				return
			}
		}
	}
}

// extractWindowTitle retrieves the window title from the enriched event's payload map.
func extractWindowTitle(ev *EnrichedEvent) string {
	if ev.PayloadJSON == nil {
		return ""
	}
	// window.title_changed and window.focus_lost carry a "title" field.
	if t, ok := ev.PayloadJSON["title"].(string); ok {
		return t
	}
	return ""
}

// extractNetworkHost extracts the destination host from network-related events.
func extractNetworkHost(ev *EnrichedEvent) string {
	if ev.PayloadJSON == nil {
		return ""
	}
	// network.flow_summary uses dest_ip; network.dns_query and network.tls_sni use host.
	if h, ok := ev.PayloadJSON["host"].(string); ok {
		return h
	}
	if h, ok := ev.PayloadJSON["dest_ip"].(string); ok {
		return h
	}
	return ""
}

// isNetworkEvent returns true for event types in the network category.
func isNetworkEvent(eventType string) bool {
	return strings.HasPrefix(eventType, "network.")
}

// matchesAnyRegex tests s against all patterns; returns true on first match.
func matchesAnyRegex(s string, patterns []string) bool {
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

// matchesAnyGlob tests s against all glob patterns using simple * wildcard matching.
// Only the * wildcard is supported (no ?, no character classes). This matches the
// policy.proto SensitiveHostGlobs semantics.
func matchesAnyGlob(s string, patterns []string) bool {
	for _, p := range patterns {
		if globMatch(p, s) {
			return true
		}
	}
	return false
}

// globMatch performs a simple glob match: * matches any sequence of characters
// (including empty), no other metacharacters are supported.
func globMatch(pattern, text string) bool {
	// Recursive descent matcher.
	for len(pattern) > 0 {
		if pattern[0] == '*' {
			// Skip consecutive stars.
			for len(pattern) > 0 && pattern[0] == '*' {
				pattern = pattern[1:]
			}
			if len(pattern) == 0 {
				return true
			}
			for i := 0; i <= len(text); i++ {
				if globMatch(pattern, text[i:]) {
					return true
				}
			}
			return false
		}
		if len(text) == 0 {
			return false
		}
		if pattern[0] != text[0] {
			return false
		}
		pattern = pattern[1:]
		text = text[1:]
	}
	return len(text) == 0
}
