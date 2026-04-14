package statuspage

import (
	"context"
	"testing"
)

func TestSeverityRank(t *testing.T) {
	if severityRank("major_outage") <= severityRank("degraded") {
		t.Error("major_outage should rank higher than degraded")
	}
	if severityRank("operational") >= severityRank("degraded") {
		t.Error("operational should rank lower than degraded")
	}
	if severityRank("unknown") != -1 {
		t.Error("unknown status should rank -1")
	}
}

func TestStaticHealthSource(t *testing.T) {
	hs := StaticHealthSource{Map: map[string]string{
		"api":      "operational",
		"postgres": "degraded",
	}}
	if hs.Health("api") != "operational" {
		t.Error("api should be operational")
	}
	if hs.Health("postgres") != "degraded" {
		t.Error("postgres should be degraded")
	}
	// Unknown component defaults to operational
	if hs.Health("unknown") != "operational" {
		t.Error("unknown should default to operational")
	}
}

func TestGetPublicStatusWithNilPool(t *testing.T) {
	// With nil pool the service still builds a payload from the
	// health source. The uptime map is empty but components should
	// default to 100% when they're operational.
	svc := NewService(nil, nil, nil, StaticHealthSource{
		Map: map[string]string{
			"api":      "operational",
			"postgres": "degraded",
		},
	})
	status, err := svc.GetPublicStatus(context.Background())
	if err != nil {
		t.Fatalf("GetPublicStatus: %v", err)
	}
	if len(status.Components) != len(Components) {
		t.Errorf("expected %d components, got %d", len(Components), len(status.Components))
	}
	if status.Overall != "degraded" {
		t.Errorf("overall = %q, want degraded (worst component)", status.Overall)
	}
	// First component is api, should be operational with 100% uptime
	var apiComp *ComponentStatus
	for i := range status.Components {
		if status.Components[i].Name == "api" {
			apiComp = &status.Components[i]
			break
		}
	}
	if apiComp == nil {
		t.Fatal("api component missing")
	}
	if apiComp.Status != "operational" {
		t.Errorf("api status = %q", apiComp.Status)
	}
	if apiComp.UptimeSevenDay != 100 {
		t.Errorf("api uptime = %v, want 100", apiComp.UptimeSevenDay)
	}
}

func TestCreateIncidentValidation(t *testing.T) {
	svc := NewService(nil, nil, nil, nil)
	ctx := context.Background()

	// Missing severity
	if _, err := svc.CreateIncident(ctx, "actor", CreateIncidentRequest{
		Component: "api", Title: "x",
	}); err == nil {
		t.Error("expected validation error for missing severity")
	}

	// Missing component
	if _, err := svc.CreateIncident(ctx, "actor", CreateIncidentRequest{
		Severity: SeverityP1, Title: "x",
	}); err == nil {
		t.Error("expected validation error for missing component")
	}

	// Missing title
	if _, err := svc.CreateIncident(ctx, "actor", CreateIncidentRequest{
		Severity: SeverityP1, Component: "api",
	}); err == nil {
		t.Error("expected validation error for missing title")
	}
}

func TestPublisherStubs(t *testing.T) {
	sp := &StatuspageIOPublisher{PageID: "page123"}
	if sp.Name() != "statuspage.io" {
		t.Errorf("Name = %s", sp.Name())
	}
	if err := sp.Publish(context.Background(), PublicStatus{}); err != ErrNotConfigured {
		t.Errorf("Publish: expected ErrNotConfigured, got %v", err)
	}

	ins := &InstatusPublisher{PageID: "pg"}
	if ins.Name() != "instatus" {
		t.Errorf("Name = %s", ins.Name())
	}
	if err := ins.Publish(context.Background(), PublicStatus{}); err != ErrNotConfigured {
		t.Errorf("Publish: expected ErrNotConfigured, got %v", err)
	}
}
