package enricher

import "strings"

// Sink identifies where an event should be written.
type Sink int

const (
	// SinkClickHouse is the default sink for metadata events.
	SinkClickHouse Sink = iota
	// SinkClickHouseHeartbeat is for agent.health_heartbeat events (separate table).
	SinkClickHouseHeartbeat
	// SinkDrop discards the event (RETENTION_CLASS_PURGE).
	SinkDrop
)

// Destination describes the routing decision for a single event.
type Destination struct {
	Sink       Sink
	Table      string // ClickHouse table name
	BlobBucket string // MinIO bucket (for blob-reference events, set by the DLP/blob pipeline)
	BlobPrefix string // MinIO key prefix
}

// Router decides where to write each event based on event_type, sensitivity flag,
// and retention class.
type Router struct{}

// NewRouter creates a Router.
func NewRouter() *Router { return &Router{} }

// Route returns the Destination for the given enriched event.
func (r *Router) Route(ev *EnrichedEvent) Destination {
	eventType := ev.Event.GetMeta().GetEventType()

	// Heartbeat goes to its own compact table.
	if eventType == "agent.health_heartbeat" {
		return Destination{Sink: SinkClickHouseHeartbeat, Table: "agent_heartbeats"}
	}

	// Events with RETENTION_CLASS_PURGE are dropped immediately.
	rc := ev.Event.GetMeta().GetRetention()
	if rc.String() == "RETENTION_CLASS_PURGE" {
		return Destination{Sink: SinkDrop}
	}

	// Blob-reference events (screenshot.captured, keystroke.content_encrypted, etc.)
	// go to ClickHouse for the metadata row but also carry a blob_ref that points
	// to the MinIO object already uploaded by the agent. No additional MinIO write
	// is needed at the enricher level for those.

	// Sensitive-flagged events go to the dedicated sensitive tables.
	if ev.Sensitive {
		table := sensitiveTable(eventType)
		return Destination{Sink: SinkClickHouse, Table: table}
	}

	return Destination{Sink: SinkClickHouse, Table: "events_raw"}
}

// sensitiveTable maps an event_type to its sensitive ClickHouse table.
// Falls back to events_raw if no specific sensitive table is defined for the type.
func sensitiveTable(eventType string) string {
	switch {
	case eventType == "window.title_changed" || eventType == "window.focus_lost":
		return "events_sensitive_window"
	case strings.HasPrefix(eventType, "file."):
		return "events_sensitive_file"
	case eventType == "keystroke.window_stats":
		return "events_sensitive_keystroke_meta"
	case eventType == "clipboard.metadata":
		return "events_sensitive_clipboard_meta"
	default:
		// For event types without a dedicated sensitive table, still write to
		// events_raw with sensitive=true so the TTL clause picks a shorter lifetime.
		return "events_raw"
	}
}
