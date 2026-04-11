package enricher

import (
	"testing"
)

func TestFallbackClassifier_WorkApps(t *testing.T) {
	c := NewRegexFallbackClassifier()

	cases := []struct {
		app        string
		title      string
		url        string
		wantCat    Category
		wantMinCnf float32
	}{
		// International work apps
		{"code.exe", "", "", CategoryWork, 0.85},
		{"slack.exe", "", "", CategoryWork, 0.85},
		{"teams.exe", "", "", CategoryWork, 0.85},
		{"outlook.exe", "", "", CategoryWork, 0.85},
		{"chrome.exe", "", "", CategoryWork, 0.85},
		// Turkish business apps
		{"logo.exe", "", "", CategoryWork, 0.85},
		{"mikro.exe", "", "", CategoryWork, 0.85},
		{"netsis.exe", "", "", CategoryWork, 0.85},
		{"parasut.exe", "", "", CategoryWork, 0.85},
		{"bordroplus.exe", "", "", CategoryWork, 0.85},
		// Case insensitivity
		{"CODE.exe", "", "", CategoryWork, 0.85},
		{"  logo.exe  ", "", "", CategoryWork, 0.85},
	}

	for _, tc := range cases {
		t.Run(tc.app, func(t *testing.T) {
			got := c.Classify(tc.app, tc.title, tc.url)
			if got.Category != tc.wantCat {
				t.Errorf("Classify(%q) = %s, want %s", tc.app, got.Category, tc.wantCat)
			}
			if got.Confidence < tc.wantMinCnf {
				t.Errorf("Classify(%q) confidence = %f, want >= %f", tc.app, got.Confidence, tc.wantMinCnf)
			}
			if got.Backend != "fallback" {
				t.Errorf("Classify(%q) backend = %s, want fallback", tc.app, got.Backend)
			}
		})
	}
}

func TestFallbackClassifier_DistractionApps(t *testing.T) {
	c := NewRegexFallbackClassifier()

	cases := []string{
		"netflix.exe", "spotify.exe", "steam.exe", "discord.exe",
		"telegram.exe", "whatsapp.exe", "epicgameslauncher.exe",
	}

	for _, app := range cases {
		t.Run(app, func(t *testing.T) {
			got := c.Classify(app, "", "")
			if got.Category != CategoryDistraction {
				t.Errorf("Classify(%q) = %s, want distraction", app, got.Category)
			}
		})
	}
}

func TestFallbackClassifier_Hosts(t *testing.T) {
	c := NewRegexFallbackClassifier()

	cases := []struct {
		url     string
		wantCat Category
	}{
		{"https://github.com/user/repo", CategoryWork},
		{"https://stackoverflow.com/questions/12345", CategoryWork},
		{"jira.company.com/browse/PROJ-123", CategoryWork},
		{"https://hepsiburada.com/product", CategoryPersonal},
		{"https://akbank.com.tr", CategoryPersonal},
		{"https://youtube.com/watch?v=abc", CategoryDistraction},
		{"https://twitter.com/user", CategoryDistraction},
		{"https://reddit.com/r/programming", CategoryDistraction},
	}

	for _, tc := range cases {
		t.Run(tc.url, func(t *testing.T) {
			got := c.Classify("unknown.exe", "", tc.url)
			if got.Category != tc.wantCat {
				t.Errorf("Classify(url=%q) = %s, want %s", tc.url, got.Category, tc.wantCat)
			}
		})
	}
}

func TestFallbackClassifier_Unknown(t *testing.T) {
	c := NewRegexFallbackClassifier()

	got := c.Classify("random-app.exe", "Some random window", "example.com")
	if got.Category != CategoryUnknown {
		t.Errorf("Classify(unknown) = %s, want unknown", got.Category)
	}
	if got.Confidence > 0.70 {
		t.Errorf("Classify(unknown) confidence = %f, want <= 0.70 (ADR 0017 threshold)", got.Confidence)
	}
}

func TestFallbackClassifier_WorkTitleHint(t *testing.T) {
	c := NewRegexFallbackClassifier()

	// Unknown app with a work-indicating title gets bumped to work.
	got := c.Classify("mystery.exe", "Pull Request #42 - Personel", "")
	if got.Category != CategoryWork {
		t.Errorf("Classify(work title) = %s, want work", got.Category)
	}
}

func TestFallbackClassifier_FuzzyMatch(t *testing.T) {
	c := NewRegexFallbackClassifier()

	// Outlook variant not in exact list — should still classify as work via fuzzy.
	got := c.Classify("outlook-365.exe", "", "")
	if got.Category != CategoryWork {
		t.Errorf("Classify(outlook fuzzy) = %s, want work", got.Category)
	}
	if got.Confidence > 0.70 {
		t.Errorf("Classify(fuzzy) confidence = %f, want <= 0.70 (fuzzy is low-confidence)", got.Confidence)
	}
}
