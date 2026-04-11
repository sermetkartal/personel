// generator.go produces realistic event mixes for simulated agents.
//
// The distribution matches event-taxonomy.md frequencies. Each event type
// has a daily rate (events/endpoint/day); the generator uses a Poisson
// process to produce inter-arrival times, making the traffic look like real
// agent output rather than uniform bursts.
//
// Configurable distributions allow scenarios to bias toward specific event
// types (e.g., file-heavy scenarios for storage testing).
package simulator

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	pseudorand "math/rand/v2"
	"time"

	"github.com/google/uuid"
)

// EventType mirrors the event taxonomy dotted names.
type EventType string

const (
	EventTypeProcessStart            EventType = "process.start"
	EventTypeProcessStop             EventType = "process.stop"
	EventTypeProcessForegroundChange EventType = "process.foreground_change"
	EventTypeWindowTitleChanged      EventType = "window.title_changed"
	EventTypeWindowFocusLost         EventType = "window.focus_lost"
	EventTypeSessionIdleStart        EventType = "session.idle_start"
	EventTypeSessionIdleEnd          EventType = "session.idle_end"
	EventTypeSessionLock             EventType = "session.lock"
	EventTypeSessionUnlock           EventType = "session.unlock"
	EventTypeScreenshotCaptured      EventType = "screenshot.captured"
	EventTypeScreenclipCaptured      EventType = "screenclip.captured"
	EventTypeFileCreated             EventType = "file.created"
	EventTypeFileRead                EventType = "file.read"
	EventTypeFileWritten             EventType = "file.written"
	EventTypeFileDeleted             EventType = "file.deleted"
	EventTypeFileRenamed             EventType = "file.renamed"
	EventTypeFileCopied              EventType = "file.copied"
	EventTypeClipboardMetadata       EventType = "clipboard.metadata"
	EventTypeClipboardContent        EventType = "clipboard.content_encrypted"
	EventTypePrintJobSubmitted       EventType = "print.job_submitted"
	EventTypeUsbDeviceAttached       EventType = "usb.device_attached"
	EventTypeUsbDeviceRemoved        EventType = "usb.device_removed"
	EventTypeUsbPolicyBlock          EventType = "usb.mass_storage_policy_block"
	EventTypeNetworkFlowSummary      EventType = "network.flow_summary"
	EventTypeNetworkDNSQuery         EventType = "network.dns_query"
	EventTypeNetworkTLSSNI           EventType = "network.tls_sni"
	EventTypeKeystrokeWindowStats    EventType = "keystroke.window_stats"
	EventTypeKeystrokeContent        EventType = "keystroke.content_encrypted"
	EventTypeAppBlockedByPolicy      EventType = "app.blocked_by_policy"
	EventTypeWebBlockedByPolicy      EventType = "web.blocked_by_policy"
	EventTypeAgentHealthHeartbeat    EventType = "agent.health_heartbeat"
	EventTypeAgentPolicyApplied      EventType = "agent.policy_applied"
	EventTypeAgentUpdateInstalled    EventType = "agent.update_installed"
	EventTypeAgentTamperDetected     EventType = "agent.tamper_detected"
	EventTypeLiveViewStarted         EventType = "live_view.started"
	EventTypeLiveViewStopped         EventType = "live_view.stopped"
)

// taxonomyRate holds the daily frequency and average payload size from
// event-taxonomy.md. These drive the Poisson process.
type taxonomyRate struct {
	eventType   EventType
	perDayRate  float64 // events per endpoint per day
	avgSizeB    int     // average payload bytes
}

// defaultTaxonomyRates is the canonical distribution from event-taxonomy.md.
// The rates are used to compute inter-arrival times for the Poisson process.
var defaultTaxonomyRates = []taxonomyRate{
	{EventTypeProcessStart, 300, 280},
	{EventTypeProcessStop, 300, 220},
	{EventTypeProcessForegroundChange, 800, 260},
	{EventTypeWindowTitleChanged, 1500, 420},
	{EventTypeWindowFocusLost, 800, 180},
	{EventTypeSessionIdleStart, 40, 160},
	{EventTypeSessionIdleEnd, 40, 160},
	{EventTypeSessionLock, 8, 140},
	{EventTypeSessionUnlock, 8, 140},
	{EventTypeScreenshotCaptured, 60, 200},
	{EventTypeScreenclipCaptured, 4, 260},
	{EventTypeFileCreated, 400, 360},
	{EventTypeFileRead, 1200, 340},
	{EventTypeFileWritten, 600, 360},
	{EventTypeFileDeleted, 80, 340},
	{EventTypeFileRenamed, 60, 440},
	{EventTypeFileCopied, 40, 420},
	{EventTypeClipboardMetadata, 200, 200},
	{EventTypeClipboardContent, 200, 600},
	{EventTypePrintJobSubmitted, 20, 320},
	{EventTypeUsbDeviceAttached, 4, 300},
	{EventTypeUsbDeviceRemoved, 4, 220},
	{EventTypeUsbPolicyBlock, 1, 300},
	{EventTypeNetworkFlowSummary, 3000, 280},
	{EventTypeNetworkDNSQuery, 1500, 200},
	{EventTypeNetworkTLSSNI, 2000, 240},
	{EventTypeKeystrokeWindowStats, 1000, 180},
	{EventTypeKeystrokeContent, 200, 900},
	{EventTypeAppBlockedByPolicy, 3, 300},
	{EventTypeWebBlockedByPolicy, 5, 320},
	{EventTypeAgentHealthHeartbeat, 288, 220}, // every 5 min
	{EventTypeAgentPolicyApplied, 4, 260},
	{EventTypeAgentUpdateInstalled, 0, 280}, // essentially never
	{EventTypeAgentTamperDetected, 0, 340},  // essentially never
	{EventTypeLiveViewStarted, 0, 260},      // essentially never
	{EventTypeLiveViewStopped, 0, 260},      // essentially never
}

// GeneratedEvent is a single simulated event ready to be wrapped in a
// proto EventBatch message.
type GeneratedEvent struct {
	EventType   EventType
	EventID     string   // ULID-like UUID
	OccurredAt  time.Time
	PayloadSize int    // bytes of synthetic payload
	IsEncrypted bool   // for keystroke/clipboard content events
	BlobRef     string // minio:// reference for encrypted events
}

// EventGenerator generates realistic event streams for one simulated agent.
// It uses a seeded PRNG for deterministic replays.
type EventGenerator struct {
	rng        *pseudorand.Rand
	endpointID string
	tenantID   string
	rates      []taxonomyRate
	// cumulative weights for weighted random selection
	cumulativeWeights []float64
	totalWeight       float64
	seq                uint64
}

// NewEventGenerator creates a generator for the given agent.
// seed=0 uses a random seed; any other value gives deterministic output.
func NewEventGenerator(endpointID, tenantID string, seed uint64) *EventGenerator {
	if seed == 0 {
		n, _ := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
		seed = n.Uint64()
	}

	src := pseudorand.NewPCG(seed, seed^0xdeadbeefcafe)
	rng := pseudorand.New(src)

	rates := defaultTaxonomyRates

	// Build cumulative weights for O(log n) selection.
	cumulative := make([]float64, len(rates))
	var total float64
	for i, r := range rates {
		w := r.perDayRate
		if w < 0.1 {
			w = 0.1 // floor so very rare events can still be sampled in load tests
		}
		total += w
		cumulative[i] = total
	}

	return &EventGenerator{
		rng:               rng,
		endpointID:        endpointID,
		tenantID:          tenantID,
		rates:             rates,
		cumulativeWeights: cumulative,
		totalWeight:       total,
		seq:               0,
	}
}

// NextEvent samples the next event using the Poisson-weighted distribution.
// The returned event's OccurredAt is relative to baseTime.
func (g *EventGenerator) NextEvent(baseTime time.Time) GeneratedEvent {
	idx := g.weightedSample()
	rate := g.rates[idx]

	g.seq++
	eventID := uuid.New().String()

	// Poisson inter-arrival: exponential distribution with rate λ=perDayRate/day.
	// Mean inter-arrival = 1/λ. We clamp to sensible bounds.
	var interArrival time.Duration
	if rate.perDayRate > 0.01 {
		meanSeconds := (24 * 3600.0) / rate.perDayRate
		// Exponential: -ln(U) / λ where U ~ Uniform(0,1)
		u := g.rng.Float64()
		if u < 1e-10 {
			u = 1e-10
		}
		jitterSec := -math.Log(u) * meanSeconds
		// Clamp to ±5x the mean to avoid extreme outliers in test scenarios.
		maxSec := meanSeconds * 5
		if jitterSec > maxSec {
			jitterSec = maxSec
		}
		interArrival = time.Duration(jitterSec * float64(time.Second))
	} else {
		interArrival = 24 * time.Hour
	}

	isEncrypted := rate.eventType == EventTypeKeystrokeContent ||
		rate.eventType == EventTypeClipboardContent

	// Synthesize a realistic payload size with ±25% jitter.
	sizeFactor := 0.75 + 0.5*g.rng.Float64()
	payloadSize := int(float64(rate.avgSizeB) * sizeFactor)
	if payloadSize < 50 {
		payloadSize = 50
	}

	blobRef := ""
	if isEncrypted {
		ts := baseTime.Format("2006/01/02")
		blobRef = fmt.Sprintf("minio://keystroke-blobs/%s/%s/%s/%s.bin",
			g.tenantID, g.endpointID, ts, eventID)
	}

	return GeneratedEvent{
		EventType:   rate.eventType,
		EventID:     eventID,
		OccurredAt:  baseTime.Add(interArrival),
		PayloadSize: payloadSize,
		IsEncrypted: isEncrypted,
		BlobRef:     blobRef,
	}
}

// GenerateBatch generates a batch of up to maxEvents events, all occurring
// within the time window [baseTime, baseTime+window]. Returns events in
// chronological order.
func (g *EventGenerator) GenerateBatch(baseTime time.Time, window time.Duration, maxEvents int) []GeneratedEvent {
	events := make([]GeneratedEvent, 0, maxEvents)
	deadline := baseTime.Add(window)
	cursor := baseTime

	for len(events) < maxEvents {
		ev := g.NextEvent(cursor)
		if ev.OccurredAt.After(deadline) {
			break
		}
		cursor = ev.OccurredAt
		events = append(events, ev)
	}

	slog.Debug("generated event batch",
		"endpoint_id", g.endpointID,
		"count", len(events),
		"window_sec", window.Seconds(),
	)
	return events
}

// Seq returns the current monotonic sequence counter.
func (g *EventGenerator) Seq() uint64 { return g.seq }

// weightedSample picks an index using cumulative weight binary search.
func (g *EventGenerator) weightedSample() int {
	target := g.rng.Float64() * g.totalWeight
	lo, hi := 0, len(g.cumulativeWeights)-1
	for lo < hi {
		mid := (lo + hi) / 2
		if g.cumulativeWeights[mid] < target {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

// EventRatePerSecond returns the expected total event rate across all types
// for one agent, derived from the taxonomy.
func EventRatePerSecond() float64 {
	var total float64
	for _, r := range defaultTaxonomyRates {
		total += r.perDayRate
	}
	return total / (24 * 3600)
}

// TaxonomyDistribution returns the percentage breakdown of event types,
// useful for verifying simulator output matches the target distribution.
func TaxonomyDistribution() map[EventType]float64 {
	var total float64
	for _, r := range defaultTaxonomyRates {
		total += r.perDayRate
	}
	dist := make(map[EventType]float64, len(defaultTaxonomyRates))
	for _, r := range defaultTaxonomyRates {
		dist[r.eventType] = (r.perDayRate / total) * 100
	}
	return dist
}

// endpointProcesses is a realistic list of process names used in synthetic events.
var endpointProcesses = []string{
	"chrome.exe", "EXCEL.EXE", "WINWORD.EXE", "OUTLOOK.EXE",
	"Teams.exe", "slack.exe", "explorer.exe", "notepad.exe",
	"powershell.exe", "cmd.exe", "POWERPNT.EXE", "acrobat.exe",
}

// SyntheticProcessName returns a realistic process name for generated events.
func (g *EventGenerator) SyntheticProcessName() string {
	return endpointProcesses[g.rng.IntN(len(endpointProcesses))]
}

// SyntheticWindowTitle returns a realistic window title.
func (g *EventGenerator) SyntheticWindowTitle() string {
	titles := []string{
		"Monthly Report - Excel",
		"Inbox - Outlook",
		"Google Chrome",
		"HR Portal - Microsoft Edge",
		"Project Plan.xlsx - Excel",
		"Teams - Personel Corp",
		"Documents - Explorer",
	}
	return titles[g.rng.IntN(len(titles))]
}

// SyntheticFilePath returns a realistic Windows file path.
func (g *EventGenerator) SyntheticFilePath() string {
	paths := []string{
		`C:\Users\user\Documents\report.docx`,
		`C:\Users\user\Downloads\archive.zip`,
		`C:\Users\user\Desktop\notes.txt`,
		`C:\Program Files\App\data.db`,
		`C:\Users\user\AppData\Local\Temp\tmp123.tmp`,
	}
	return paths[g.rng.IntN(len(paths))]
}

// SyntheticNetworkDest returns a realistic destination IP:port pair.
func (g *EventGenerator) SyntheticNetworkDest() (string, uint32) {
	dests := [][2]any{
		{"142.250.185.46", uint32(443)},  // Google
		{"13.107.42.14", uint32(443)},    // Microsoft
		{"52.96.168.16", uint32(443)},    // Exchange Online
		{"185.199.108.153", uint32(443)}, // GitHub
		{"192.168.1.10", uint32(445)},    // internal SMB
	}
	d := dests[g.rng.IntN(len(dests))]
	return d[0].(string), d[1].(uint32)
}

// DailyEventCount returns an estimate of total events per agent per day.
func DailyEventCount() int {
	var total float64
	for _, r := range defaultTaxonomyRates {
		total += r.perDayRate
	}
	return int(total)
}
