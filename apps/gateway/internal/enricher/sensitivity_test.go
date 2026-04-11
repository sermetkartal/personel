package enricher

import (
	"testing"

	personelv1 "github.com/personel/proto/personel/v1"
)

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pattern string
		text    string
		want    bool
	}{
		{"*.saglik.gov.tr", "hastane.saglik.gov.tr", true},
		{"*.saglik.gov.tr", "saglik.gov.tr", false},  // needs a prefix
		{"*.saglik.gov.tr", "www.google.com", false},
		{"sendika.*.org", "sendika.example.org", true},
		{"sendika.*.org", "sendikalar.net", false},
		{"*", "anything", true},
		{"exact", "exact", true},
		{"exact", "not-exact", false},
	}

	for _, tc := range cases {
		t.Run(tc.pattern+"_vs_"+tc.text, func(t *testing.T) {
			got := globMatch(tc.pattern, tc.text)
			if got != tc.want {
				t.Errorf("globMatch(%q, %q) = %v, want %v", tc.pattern, tc.text, got, tc.want)
			}
		})
	}
}

func TestApplySensitivity_WindowTitle(t *testing.T) {
	guard := &personelv1.SensitivityGuard{
		WindowTitleSensitiveRegex: []string{`(?i)(sağlık|saglik|sendika)`},
	}

	cases := []struct {
		title     string
		wantSens  bool
	}{
		{"Sağlık Sigortası - Chrome", true},
		{"SAGLIK PORTALI", true},
		{"Monthly Report - Excel", false},
		{"Sendika Üye Portalı", true},
	}

	for _, tc := range cases {
		t.Run(tc.title, func(t *testing.T) {
			ev := &EnrichedEvent{
				Event: &personelv1.Event{
					Meta: &personelv1.EventMeta{EventType: "window.title_changed"},
				},
				PayloadJSON: map[string]interface{}{"title": tc.title},
			}
			ApplySensitivity(ev, guard)
			if ev.Sensitive != tc.wantSens {
				t.Errorf("title=%q: got sensitive=%v, want %v", tc.title, ev.Sensitive, tc.wantSens)
			}
		})
	}
}

func TestApplySensitivity_NilGuard(t *testing.T) {
	ev := &EnrichedEvent{
		Event: &personelv1.Event{
			Meta: &personelv1.EventMeta{EventType: "window.title_changed"},
		},
		PayloadJSON: map[string]interface{}{"title": "Sağlık"},
	}
	ApplySensitivity(ev, nil)
	if ev.Sensitive {
		t.Error("nil guard should not flag event as sensitive")
	}
}

func TestApplySensitivity_HostGlob(t *testing.T) {
	guard := &personelv1.SensitivityGuard{
		SensitiveHostGlobs: []string{"*.saglik.gov.tr"},
	}

	ev := &EnrichedEvent{
		Event: &personelv1.Event{
			Meta: &personelv1.EventMeta{EventType: "network.dns_query"},
		},
		PayloadJSON: map[string]interface{}{"host": "hastane.saglik.gov.tr"},
	}
	ApplySensitivity(ev, guard)
	if !ev.Sensitive {
		t.Error("expected sensitive flag for health gov domain")
	}
}
