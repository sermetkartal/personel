package enricher

import (
	"testing"

	personelv1 "github.com/personel/proto/personel/v1"
)

func TestRouterHeartbeat(t *testing.T) {
	r := NewRouter()
	ev := &EnrichedEvent{
		Event: &personelv1.Event{
			Meta: &personelv1.EventMeta{EventType: "agent.health_heartbeat"},
		},
	}
	dest := r.Route(ev)
	if dest.Sink != SinkClickHouseHeartbeat {
		t.Errorf("got %v want SinkClickHouseHeartbeat", dest.Sink)
	}
}

func TestRouterNormalEvent(t *testing.T) {
	r := NewRouter()
	ev := &EnrichedEvent{
		Event: &personelv1.Event{
			Meta: &personelv1.EventMeta{
				EventType: "process.start",
				Retention: personelv1.RetentionClass_RETENTION_CLASS_HOT,
			},
		},
		Sensitive: false,
	}
	dest := r.Route(ev)
	if dest.Table != "events_raw" {
		t.Errorf("got table=%q want events_raw", dest.Table)
	}
}

func TestRouterSensitiveWindowEvent(t *testing.T) {
	r := NewRouter()
	ev := &EnrichedEvent{
		Event: &personelv1.Event{
			Meta: &personelv1.EventMeta{
				EventType: "window.title_changed",
				Retention: personelv1.RetentionClass_RETENTION_CLASS_WARM,
			},
		},
		Sensitive: true,
	}
	dest := r.Route(ev)
	if dest.Table != "events_sensitive_window" {
		t.Errorf("got table=%q want events_sensitive_window", dest.Table)
	}
}

func TestRouterSensitiveFileEvent(t *testing.T) {
	r := NewRouter()
	ev := &EnrichedEvent{
		Event: &personelv1.Event{
			Meta: &personelv1.EventMeta{
				EventType: "file.written",
				Retention: personelv1.RetentionClass_RETENTION_CLASS_WARM,
			},
		},
		Sensitive: true,
	}
	dest := r.Route(ev)
	if dest.Table != "events_sensitive_file" {
		t.Errorf("got table=%q want events_sensitive_file", dest.Table)
	}
}
