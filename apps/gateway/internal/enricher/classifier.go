package enricher

import (
	"regexp"
	"strings"
)

// Category is the classification result enum shared with the ML classifier
// service (ADR 0017). Matches the `category` field reserved in EventMeta
// (Phase 2.0/3 proto reservation).
type Category string

const (
	// CategoryWork is productive work-related activity.
	CategoryWork Category = "work"
	// CategoryPersonal is non-work personal activity (shopping, banking).
	CategoryPersonal Category = "personal"
	// CategoryDistraction is leisure / entertainment / social media.
	CategoryDistraction Category = "distraction"
	// CategoryUnknown is everything we can't confidently classify.
	CategoryUnknown Category = "unknown"
)

// ClassifyResult is the fallback classifier output. The ML service returns
// the same shape so the enricher can consume either interchangeably.
type ClassifyResult struct {
	Category   Category `json:"category"`
	Confidence float32  `json:"confidence"`
	Backend    string   `json:"backend"` // "fallback" or "llama"
}

// RegexFallbackClassifier is a deterministic rule-based classifier used when
// the ML service is unavailable or explicitly disabled. It matches app
// executables and URL hosts against pre-compiled Turkish + international
// app taxonomies.
//
// ADR 0017 mandates that this fallback is ALWAYS available, even when the
// ML service is running, so the enricher can degrade gracefully in the
// ~5-20 ms window where ml-classifier is restarting. The Go enricher calls
// the ML service first (with a 50 ms timeout); on timeout or error it
// calls this fallback and annotates the event with backend="fallback".
//
// Confidence scoring:
//   - Exact app/host match in the work list: 0.90
//   - Exact app/host match in personal/distraction lists: 0.85
//   - Substring (fuzzy) match: 0.65
//   - No match: returns (CategoryUnknown, 0.50)
type RegexFallbackClassifier struct {
	workApps        map[string]struct{}
	personalApps    map[string]struct{}
	distractionApps map[string]struct{}
	workHosts       []*regexp.Regexp
	personalHosts   []*regexp.Regexp
	distractionHosts []*regexp.Regexp
	workTitleRE     []*regexp.Regexp
}

// NewRegexFallbackClassifier builds the rule set. The lists are intentionally
// compiled in code (not config) so they ship with the gateway binary and
// cannot be tampered with at runtime. Customer-specific rules go through
// the Phase 2.3 ML service fine-tuning path instead.
func NewRegexFallbackClassifier() *RegexFallbackClassifier {
	toSet := func(names []string) map[string]struct{} {
		m := make(map[string]struct{}, len(names))
		for _, n := range names {
			m[strings.ToLower(strings.TrimSpace(n))] = struct{}{}
		}
		return m
	}

	return &RegexFallbackClassifier{
		// Work apps — international + Turkish business software
		workApps: toSet([]string{
			// Dev / engineering
			"code.exe", "code", "cursor.exe", "cursor",
			"devenv.exe", "pycharm64.exe", "goland64.exe", "idea64.exe", "webstorm64.exe",
			"sublime_text.exe", "atom.exe", "vim", "nvim", "emacs",
			"git.exe", "github desktop.exe", "sourcetree.exe", "gitkraken.exe",
			"docker desktop.exe", "rancher desktop.exe", "podman desktop.exe",
			"postman.exe", "insomnia.exe", "bruno.exe",
			// Communication (work)
			"slack.exe", "slack", "teams.exe", "ms-teams.exe", "microsoft teams.exe",
			"zoom.exe", "webex.exe", "meet.exe",
			"outlook.exe", "thunderbird.exe", "mailspring.exe",
			// Office / productivity
			"winword.exe", "excel.exe", "powerpnt.exe", "onenote.exe",
			"notion.exe", "obsidian.exe", "evernote.exe",
			"libreoffice.exe", "soffice.exe",
			// Browsers (work assumed by default)
			"chrome.exe", "firefox.exe", "msedge.exe", "brave.exe", "vivaldi.exe", "arc.exe",
			// Turkish business software
			"logo.exe", "logo netsis.exe", "tiger.exe", "tiger3.exe", "tigerhr.exe",
			"mikro.exe", "mikrogold.exe", "mikro_v16.exe",
			"netsis.exe", "netopenx.exe",
			"bordroplus.exe", "logobordro.exe",
			"parasut.exe", "paraşüt.exe",
			"eta-sql.exe", "eta-v8.exe", "eta-v9.exe",
			"luca.exe", "luca_net.exe",
			"dia.exe",
			// Design
			"figma.exe", "sketch.exe", "adobe photoshop.exe", "illustrator.exe",
			// Data
			"tableau.exe", "powerbi.exe", "metabase.exe",
		}),
		personalApps: toSet([]string{
			// Banking (personal-ish but often work-mandatory in finance roles)
			"akbank.exe", "isbank.exe", "garantibbva.exe", "ziraatbank.exe",
			// Shopping
			"hepsiburada.exe", "trendyol.exe", "n11.exe",
		}),
		distractionApps: toSet([]string{
			"netflix.exe", "spotify.exe",
			"steam.exe", "epicgameslauncher.exe", "origin.exe", "battle.net.exe",
			"discord.exe", "discord", "telegram.exe", "whatsapp.exe",
			"obs.exe", "obs64.exe",
		}),
		workHosts: compileHostRegexes([]string{
			`(?i)\bgithub\.com\b`, `(?i)\bgitlab\.com\b`, `(?i)\bbitbucket\.org\b`,
			`(?i)\bstackoverflow\.com\b`, `(?i)\bstackexchange\.com\b`,
			`(?i)\bjira\.`, `(?i)\bconfluence\.`, `(?i)\btrello\.com\b`,
			`(?i)\bslack\.com\b`, `(?i)\bzoom\.us\b`, `(?i)\bteams\.microsoft\.com\b`,
			`(?i)\boutlook\.office\.com\b`, `(?i)\bsharepoint\.com\b`, `(?i)\bonedrive\.live\.com\b`,
			`(?i)\bdocs\.google\.com\b`, `(?i)\bdrive\.google\.com\b`, `(?i)\bmeet\.google\.com\b`,
			`(?i)\bfigma\.com\b`, `(?i)\bnotion\.so\b`, `(?i)\basana\.com\b`,
			`(?i)\blogoyazilim\.com\b`, `(?i)\bmikro\.com\.tr\b`, `(?i)\bnetsis\.com\.tr\b`,
			`(?i)\bparasut\.com\b`, `(?i)\blucayazilim\.com\b`,
			`(?i)\bkvkk\.gov\.tr\b`, `(?i)\bverbis\.kvkk\.gov\.tr\b`,
			`(?i)\bbddk\.org\.tr\b`,
		}),
		personalHosts: compileHostRegexes([]string{
			`(?i)\bhepsiburada\.com\b`, `(?i)\btrendyol\.com\b`, `(?i)\bn11\.com\b`,
			`(?i)\bgittigidiyor\.com\b`, `(?i)\bamazon\.com\.tr\b`, `(?i)\bamazon\.com\b`,
			`(?i)\byemeksepeti\.com\b`, `(?i)\bgetir\.com\b`,
			`(?i)\bakbank\.com\b`, `(?i)\bisbank\.com\.tr\b`, `(?i)\bgaranti\.`,
			`(?i)\bziraatbank\.com\.tr\b`, `(?i)\byapikredi\.com\.tr\b`,
			`(?i)\bnesine\.com\b`, `(?i)\bbilyoner\.com\b`, // betting
		}),
		distractionHosts: compileHostRegexes([]string{
			`(?i)\byoutube\.com\b`, `(?i)\byoutu\.be\b`, `(?i)\btwitch\.tv\b`,
			`(?i)\bnetflix\.com\b`, `(?i)\bdisneyplus\.com\b`, `(?i)\bblutv\.com\b`,
			`(?i)\btwitter\.com\b`, `(?i)\bx\.com\b`,
			`(?i)\binstagram\.com\b`, `(?i)\bfacebook\.com\b`, `(?i)\breddit\.com\b`,
			`(?i)\btiktok\.com\b`, `(?i)\bsnapchat\.com\b`,
			`(?i)\b9gag\.com\b`, `(?i)\bimgur\.com\b`,
			`(?i)\beksisozluk\.com\b`, // not distraction for everyone, but commonly is
			`(?i)\bspotify\.com\b`, `(?i)\bsoundcloud\.com\b`,
		}),
		workTitleRE: compileHostRegexes([]string{
			`(?i)pull request`, `(?i)merge request`, `(?i)code review`,
			`(?i)jira\s*-\s*`, `(?i)sprint\s*planning`,
			`(?i)k(v)?kk\s*uyum`, `(?i)denetim\s*raporu`,
		}),
	}
}

// Classify returns a best-effort category for an activity tuple.
// Always returns a non-nil Result. Never errors.
func (c *RegexFallbackClassifier) Classify(appName, windowTitle, url string) ClassifyResult {
	appLower := strings.ToLower(strings.TrimSpace(appName))
	titleLower := strings.ToLower(windowTitle)
	urlLower := strings.ToLower(url)

	// 1. Exact app match — highest confidence.
	if _, ok := c.workApps[appLower]; ok {
		return ClassifyResult{Category: CategoryWork, Confidence: 0.90, Backend: "fallback"}
	}
	if _, ok := c.distractionApps[appLower]; ok {
		return ClassifyResult{Category: CategoryDistraction, Confidence: 0.85, Backend: "fallback"}
	}
	if _, ok := c.personalApps[appLower]; ok {
		return ClassifyResult{Category: CategoryPersonal, Confidence: 0.85, Backend: "fallback"}
	}

	// 2. Host match from URL.
	if url != "" {
		if matchesAny(c.workHosts, urlLower) {
			return ClassifyResult{Category: CategoryWork, Confidence: 0.80, Backend: "fallback"}
		}
		if matchesAny(c.distractionHosts, urlLower) {
			return ClassifyResult{Category: CategoryDistraction, Confidence: 0.80, Backend: "fallback"}
		}
		if matchesAny(c.personalHosts, urlLower) {
			return ClassifyResult{Category: CategoryPersonal, Confidence: 0.75, Backend: "fallback"}
		}
	}

	// 3. Work-indicating window title phrases (e.g. "pull request").
	if windowTitle != "" && matchesAny(c.workTitleRE, titleLower) {
		return ClassifyResult{Category: CategoryWork, Confidence: 0.70, Backend: "fallback"}
	}

	// 4. Substring fuzzy match on app name — low confidence.
	if strings.Contains(appLower, "outlook") || strings.Contains(appLower, "teams") ||
		strings.Contains(appLower, "slack") || strings.Contains(appLower, "code") {
		return ClassifyResult{Category: CategoryWork, Confidence: 0.65, Backend: "fallback"}
	}

	// 5. Unknown — below the 0.70 threshold from ADR 0017 so downstream
	// code knows to treat this as "not classified".
	return ClassifyResult{Category: CategoryUnknown, Confidence: 0.50, Backend: "fallback"}
}

// matchesAny returns true if any of the regexes match the input string.
func matchesAny(rs []*regexp.Regexp, s string) bool {
	for _, r := range rs {
		if r.MatchString(s) {
			return true
		}
	}
	return false
}

// compileHostRegexes compiles a list of regex patterns. Any pattern that
// fails to compile is skipped silently — the classifier degrades rather than
// panicking. This is a deliberate trade-off: bad rule data should never
// stop the gateway from accepting events.
func compileHostRegexes(patterns []string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		if r, err := regexp.Compile(p); err == nil {
			out = append(out, r)
		}
	}
	return out
}
