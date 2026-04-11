// Package load generates synthetic agent traffic to test gateway throughput.
// The generator is realistic enough to stress-test the publish path.
package load

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"math/big"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	personelv1 "github.com/personel/proto/personel/v1"
)

// SyntheticConfig controls the load generator.
type SyntheticConfig struct {
	Endpoints    int
	EventsPerSec int
	Duration     time.Duration
	TenantID     []byte // 16 bytes UUID
}

// DefaultSyntheticConfig returns a config sized for the 500-endpoint pilot target.
// At 500 endpoints with ~5000 events/sec total this is 10 events/endpoint/sec
// which is conservative vs. the 1B events/day (10k endpoints) target.
func DefaultSyntheticConfig() SyntheticConfig {
	tenantID := make([]byte, 16)
	_, _ = rand.Read(tenantID)
	return SyntheticConfig{
		Endpoints:    500,
		EventsPerSec: 5000,
		Duration:     10 * time.Second,
		TenantID:     tenantID,
	}
}

// eventTypeWeights mirrors the taxonomy frequency ratios from event-taxonomy.md.
// The weight is proportional to events/endpoint/day.
var eventTypeWeights = []struct {
	eventType string
	weight    int
}{
	{"network.flow_summary", 3000},
	{"window.title_changed", 1500},
	{"network.tls_sni", 2000},
	{"network.dns_query", 1500},
	{"keystroke.window_stats", 1000},
	{"process.foreground_change", 800},
	{"window.focus_lost", 800},
	{"process.start", 300},
	{"process.stop", 300},
	{"file.read", 1200},
	{"file.written", 600},
	{"file.created", 400},
	{"clipboard.metadata", 200},
	{"agent.health_heartbeat", 288},
	{"screenshot.captured", 60},
	{"file.deleted", 80},
	{"keystroke.content_encrypted", 200},
	{"session.idle_start", 40},
	{"session.idle_end", 40},
	{"usb.device_attached", 4},
}

// weightedEventType returns a random event type based on frequency weights.
func weightedEventType() string {
	total := 0
	for _, w := range eventTypeWeights {
		total += w.weight
	}
	n, _ := rand.Int(rand.Reader, big.NewInt(int64(total)))
	cumulative := 0
	for _, w := range eventTypeWeights {
		cumulative += w.weight
		if n.Int64() < int64(cumulative) {
			return w.eventType
		}
	}
	return "process.start"
}

// generateEventBatch creates a realistic EventBatch with n events for the given
// tenant and endpoint.
func generateEventBatch(tenantID, endpointID []byte, seq uint64, n int) *personelv1.EventBatch {
	events := make([]*personelv1.Event, n)
	now := time.Now().UTC()

	for i := 0; i < n; i++ {
		eventID := make([]byte, 16)
		_, _ = rand.Read(eventID)
		eventType := weightedEventType()

		events[i] = &personelv1.Event{
			Meta: &personelv1.EventMeta{
				EventId:  &personelv1.EventId{Value: eventID},
				EventType: eventType,
				TenantId: &personelv1.TenantId{Value: tenantID},
				EndpointId: &personelv1.EndpointId{Value: endpointID},
				OccurredAt: timestamppb.New(now.Add(-time.Duration(i) * time.Millisecond)),
				Seq:        seq + uint64(i),
				Pii:        personelv1.PiiClass_PII_CLASS_BEHAVIORAL,
				Retention:  personelv1.RetentionClass_RETENTION_CLASS_HOT,
			},
			Payload: syntheticPayload(eventType),
		}
	}

	batchIDBytes := make([]byte, 8)
	_, _ = rand.Read(batchIDBytes)
	return &personelv1.EventBatch{
		BatchId: binary.LittleEndian.Uint64(batchIDBytes),
		Events:  events,
	}
}

// syntheticPayload returns a minimal realistic payload for the given event type.
func syntheticPayload(eventType string) personelv1.isEvent_Payload {
	switch eventType {
	case "process.start":
		return &personelv1.Event_ProcessStart{
			ProcessStart: &personelv1.ProcessStart{
				Pid:       12345,
				ImagePath: `C:\Windows\System32\cmd.exe`,
				Signer:    "Microsoft Windows",
			},
		}
	case "window.title_changed":
		return &personelv1.Event_WindowTitleChanged{
			WindowTitleChanged: &personelv1.WindowTitleChanged{
				Pid:     12345,
				Title:   "Untitled - Notepad",
				ExeName: "notepad.exe",
			},
		}
	case "network.flow_summary":
		return &personelv1.Event_NetworkFlowSummary{
			NetworkFlowSummary: &personelv1.NetworkFlowSummary{
				Pid:      12345,
				ExeName:  "chrome.exe",
				DestIp:   "142.250.180.46",
				DestPort: 443,
				Protocol: "tcp",
				BytesOut: 12000,
				BytesIn:  48000,
			},
		}
	case "agent.health_heartbeat":
		return &personelv1.Event_AgentHealthHeartbeat{
			AgentHealthHeartbeat: &personelv1.AgentHealthHeartbeat{
				CpuPercent:    1.2,
				RssBytes:      128 * 1024 * 1024,
				QueueDepth:    0,
				PolicyVersion: "v1.0.0",
			},
		}
	default:
		return &personelv1.Event_ProcessStart{
			ProcessStart: &personelv1.ProcessStart{Pid: 1},
		}
	}
}

// TestSyntheticBatchGeneration ensures the generator produces valid proto.
func TestSyntheticBatchGeneration(t *testing.T) {
	tenantID := make([]byte, 16)
	endpointID := make([]byte, 16)
	_, _ = rand.Read(tenantID)
	_, _ = rand.Read(endpointID)

	batch := generateEventBatch(tenantID, endpointID, 1000, 200)
	if len(batch.Events) != 200 {
		t.Errorf("expected 200 events, got %d", len(batch.Events))
	}

	// Verify it marshals cleanly.
	data, err := proto.Marshal(batch)
	if err != nil {
		t.Fatalf("marshal batch: %v", err)
	}
	t.Logf("batch size: %d bytes for 200 events (avg %d B/event)",
		len(data), len(data)/200)

	// Verify round-trip.
	var batch2 personelv1.EventBatch
	if err := proto.Unmarshal(data, &batch2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(batch2.Events) != 200 {
		t.Errorf("round-trip: expected 200 events, got %d", len(batch2.Events))
	}
}

// BenchmarkBatchGeneration benchmarks the synthetic event generator to establish
// an upper bound on how fast the test harness can generate load.
func BenchmarkBatchGeneration(b *testing.B) {
	tenantID := make([]byte, 16)
	endpointID := make([]byte, 16)
	_, _ = rand.Read(tenantID)
	_, _ = rand.Read(endpointID)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		batch := generateEventBatch(tenantID, endpointID, uint64(i)*200, 200)
		data, _ := proto.Marshal(batch)
		b.SetBytes(int64(len(data)))
	}
}

// TestLoadGeneratorRate verifies that the generator can sustain the target
// rate without blocking. This is a smoke test, not a real load test.
func TestLoadGeneratorRate(t *testing.T) {
	t.Skip("load test: run manually with -run TestLoadGeneratorRate and -timeout 60s")

	cfg := DefaultSyntheticConfig()
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Duration)
	defer cancel()

	eventsGenerated := 0
	batchSize := 200
	interval := time.Second / time.Duration(cfg.EventsPerSec/batchSize)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	endpointID := make([]byte, 16)
	_, _ = rand.Read(endpointID)

	var seq uint64
	for {
		select {
		case <-ctx.Done():
			t.Logf("generated %d events in %v", eventsGenerated, cfg.Duration)
			if eventsGenerated < int(cfg.Duration.Seconds())*cfg.EventsPerSec/2 {
				t.Errorf("generated fewer than 50%% of target events: %d < %d",
					eventsGenerated, int(cfg.Duration.Seconds())*cfg.EventsPerSec/2)
			}
			return
		case <-ticker.C:
			batch := generateEventBatch(cfg.TenantID, endpointID, seq, batchSize)
			seq += uint64(batchSize)
			eventsGenerated += len(batch.Events)
		}
	}
}

// compile-time alias to silence unused import.
var _ = fmt.Sprintf
