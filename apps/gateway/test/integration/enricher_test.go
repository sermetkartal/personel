//go:build integration
// +build integration

package integration

import (
	"context"
	"testing"

	personelv1 "github.com/personel/proto/personel/v1"

	"github.com/personel/gateway/internal/enricher"
)

// TestEnricherSensitivityRoutesWindowTitle verifies that an event whose window
// title matches a SensitivityGuard regex is routed to events_sensitive_window.
func TestEnricherSensitivityRoutesWindowTitle(t *testing.T) {
	ctx := context.Background()
	_ = ctx

	guard := &personelv1.SensitivityGuard{
		WindowTitleSensitiveRegex: []string{`(?i)(sağlık|saglik|sendika|union)`},
	}

	ev := &enricher.EnrichedEvent{
		Event: &personelv1.Event{
			Meta: &personelv1.EventMeta{
				EventType: "window.title_changed",
				Retention: personelv1.RetentionClass_RETENTION_CLASS_WARM,
			},
			Payload: &personelv1.Event_WindowTitleChanged{
				WindowTitleChanged: &personelv1.WindowTitleChanged{
					Title: "Sağlık Sigortası - Chrome",
				},
			},
		},
		PayloadJSON: map[string]interface{}{
			"title": "Sağlık Sigortası - Chrome",
		},
	}

	enricher.ApplySensitivity(ev, guard)

	if !ev.Sensitive {
		t.Error("expected event to be flagged as sensitive (health keyword in title)")
	}

	router := enricher.NewRouter()
	dest := router.Route(ev)
	if dest.Table != "events_sensitive_window" {
		t.Errorf("expected routing to events_sensitive_window, got %q", dest.Table)
	}
}

// TestEnricherHeartbeatRoutesToHeartbeatTable verifies that agent.health_heartbeat
// events are routed to the compact heartbeat sink.
func TestEnricherHeartbeatRoutesToHeartbeatTable(t *testing.T) {
	ev := &enricher.EnrichedEvent{
		Event: &personelv1.Event{
			Meta: &personelv1.EventMeta{
				EventType: "agent.health_heartbeat",
			},
		},
	}

	router := enricher.NewRouter()
	dest := router.Route(ev)
	if dest.Sink != enricher.SinkClickHouseHeartbeat {
		t.Errorf("expected SinkClickHouseHeartbeat, got %v", dest.Sink)
	}
}

// TestEnricherNormalEventRoutesToEventsRaw verifies the default routing path.
func TestEnricherNormalEventRoutesToEventsRaw(t *testing.T) {
	ev := &enricher.EnrichedEvent{
		Event: &personelv1.Event{
			Meta: &personelv1.EventMeta{
				EventType: "process.start",
				Retention: personelv1.RetentionClass_RETENTION_CLASS_HOT,
			},
		},
		Sensitive: false,
	}

	router := enricher.NewRouter()
	dest := router.Route(ev)
	if dest.Table != "events_raw" {
		t.Errorf("expected events_raw, got %q", dest.Table)
	}
}

// TestEnricherGlobMatch exercises the glob matcher used for sensitive host matching.
func TestEnricherGlobMatch(t *testing.T) {
	guard := &personelv1.SensitivityGuard{
		SensitiveHostGlobs: []string{"*.saglik.gov.tr", "sendika.*.org"},
	}

	cases := []struct {
		host      string
		eventType string
		wantSens  bool
	}{
		{"hastane.saglik.gov.tr", "network.dns_query", true},
		{"www.google.com", "network.dns_query", false},
		{"sendika.example.org", "network.tls_sni", true},
		{"sendikalar.net", "network.tls_sni", false},
	}

	for _, tc := range cases {
		ev := &enricher.EnrichedEvent{
			Event: &personelv1.Event{
				Meta: &personelv1.EventMeta{EventType: tc.eventType},
			},
			PayloadJSON: map[string]interface{}{"host": tc.host},
		}
		enricher.ApplySensitivity(ev, guard)
		if ev.Sensitive != tc.wantSens {
			t.Errorf("host=%q eventType=%q: got sensitive=%v, want %v",
				tc.host, tc.eventType, ev.Sensitive, tc.wantSens)
		}
	}
}
