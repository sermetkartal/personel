package reports

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/personel/api/internal/auth"
)

// ---------------------------------------------------------------------------
// Fakes — substitute the gofpdf / excelize engines so tests don't pull the
// new deps. Production wiring uses NewGofpdfEngine() / NewExcelizeEngine().
// ---------------------------------------------------------------------------

type fakePDFEngine struct {
	lastSize int
}

func (f *fakePDFEngine) BuildTopApps(_ []TopAppRow, _ string, _ string, _ time.Time) ([]byte, error) {
	b := []byte("%PDF-1.4\n% fake top apps\n%%EOF\n")
	f.lastSize = len(b)
	return b, nil
}
func (f *fakePDFEngine) BuildIdleActive(_ []IdleActiveRow, _ string, _ string, _ time.Time) ([]byte, error) {
	return []byte("%PDF-1.4\n% fake idle active\n%%EOF\n"), nil
}
func (f *fakePDFEngine) BuildProductivity(_ []ProductivityRow, _ string, _ string, _ time.Time) ([]byte, error) {
	return []byte("%PDF-1.4\n% fake productivity\n%%EOF\n"), nil
}
func (f *fakePDFEngine) BuildTrend(_ *TrendResult, _ string, _ string, _ time.Time) ([]byte, error) {
	return []byte("%PDF-1.4\n% fake trend\n%%EOF\n"), nil
}
func (f *fakePDFEngine) BuildRisk(_ []RiskTierRow, _ string, _ string, _ time.Time) ([]byte, error) {
	return []byte("%PDF-1.4\n% fake risk\n%%EOF\n"), nil
}

type fakeXLSXEngine struct{}

// XLSX files are ZIP archives — first two bytes "PK" + a signature.
var fakeXLSXPayload = []byte{0x50, 0x4B, 0x03, 0x04, 0x00, 0x00, 'f', 'a', 'k', 'e'}

func (f *fakeXLSXEngine) BuildTopApps(_ []TopAppRow, _ string, _ time.Time) ([]byte, error) {
	return fakeXLSXPayload, nil
}
func (f *fakeXLSXEngine) BuildIdleActive(_ []IdleActiveRow, _ string, _ time.Time) ([]byte, error) {
	return fakeXLSXPayload, nil
}
func (f *fakeXLSXEngine) BuildProductivity(_ []ProductivityRow, _ string, _ time.Time) ([]byte, error) {
	return fakeXLSXPayload, nil
}
func (f *fakeXLSXEngine) BuildTrend(_ *TrendResult, _ string, _ time.Time) ([]byte, error) {
	return fakeXLSXPayload, nil
}
func (f *fakeXLSXEngine) BuildRisk(_ []RiskTierRow, _ string, _ time.Time) ([]byte, error) {
	return fakeXLSXPayload, nil
}

// fakeCH is a tiny in-memory CHReporter for the 3 CH-backed report types.
type fakeCH struct{}

func (f *fakeCH) TopApps(_ context.Context, _ string, _, _ time.Time, limit int) ([]TopAppRow, error) {
	return []TopAppRow{
		{AppName: "Code.exe", FocusSeconds: 3600, FocusPct: 60},
		{AppName: "chrome.exe", FocusSeconds: 1800, FocusPct: 30},
	}, nil
}

func (f *fakeCH) IdleActive(_ context.Context, _ string, _, _ time.Time, _ []string) ([]IdleActiveRow, error) {
	return []IdleActiveRow{
		{Date: time.Now(), EndpointID: "ep-1", ActiveSeconds: 3600, IdleSeconds: 900, ActiveRatio: 0.8},
	}, nil
}

func (f *fakeCH) ProductivityTimeline(_ context.Context, _ string, _, _ time.Time, _ []string) ([]ProductivityRow, error) {
	return []ProductivityRow{
		{Hour: time.Now(), EndpointID: "ep-1", ActiveSeconds: 3000, IdleSeconds: 600},
	}, nil
}

// fakeTrend is an in-memory TrendReporter.
type fakeTrend struct{ err error }

func (f *fakeTrend) TrendReport(_ context.Context, _ string, metric MetricName, window int) (*TrendResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &TrendResult{
		Metric:         metric,
		WindowDays:     window,
		CurrentPeriod:  MetricSnapshot{Mean: 60, Median: 60, P95: 70, Sum: 420, Count: 7},
		PreviousPeriod: MetricSnapshot{Mean: 50, Median: 50, P95: 60, Sum: 350, Count: 7},
		Delta:          0.2,
		Direction:      "up",
		ZScore:         1.1,
		Anomaly:        false,
	}, nil
}

// fakeRisk implements RiskReporter. Left nil in most tests.
type fakeRisk struct{}

func (f *fakeRisk) RiskTiers(_ context.Context, _ string, _, _ time.Time) ([]RiskTierRow, error) {
	return []RiskTierRow{
		{Tier: "low", UserCount: 40, AvgScore: 12.5},
		{Tier: "high", UserCount: 3, AvgScore: 78.0},
	}, nil
}

// fakeMinio captures PutObject calls and returns deterministic presigned URLs.
type fakeMinio struct {
	objects map[string][]byte
	failPut bool
}

func (f *fakeMinio) PutObject(_ context.Context, bucket, key string, data []byte, _ string) error {
	if f.failPut {
		return errors.New("put failed")
	}
	if f.objects == nil {
		f.objects = map[string][]byte{}
	}
	f.objects[bucket+"/"+key] = data
	return nil
}

func (f *fakeMinio) PresignedGetURL(_ context.Context, bucket, key string, _ time.Duration) (string, error) {
	return "https://fake-minio/" + bucket + "/" + key + "?sig=fake", nil
}

// helper: build an Exporter with fakes.
func newTestExporter(withRisk bool, maxBytes int64) (*Exporter, *fakeMinio, *fakePDFEngine) {
	pdf := &fakePDFEngine{}
	minio := &fakeMinio{}
	deps := ExporterDeps{
		CH:       &fakeCH{},
		Trend:    &fakeTrend{},
		Minio:    minio,
		PDF:      pdf,
		XLSX:     &fakeXLSXEngine{},
		Log:      slog.Default(),
		MaxBytes: maxBytes,
	}
	if withRisk {
		deps.Risk = &fakeRisk{}
	}
	return NewExporter(deps), minio, pdf
}

func adminPrincipal() *auth.Principal {
	return &auth.Principal{
		UserID:   "user-1",
		TenantID: "tenant-1",
		Roles:    []auth.Role{auth.RoleAdmin},
	}
}

// ---------------------------------------------------------------------------
// Happy-path tests
// ---------------------------------------------------------------------------

func TestExport_TopAppsPDF_StartsWithPDFMagic(t *testing.T) {
	e, mc, _ := newTestExporter(false, 0)
	res, err := e.Export(context.Background(), adminPrincipal(), ExportRequest{
		ReportType: "top_apps",
		Format:     FormatPDF,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if res == nil || res.Key == "" {
		t.Fatalf("empty result")
	}
	// Verify stored object starts with %PDF-
	key := ExportBucket + "/" + res.Key
	bytes, ok := mc.objects[key]
	if !ok {
		t.Fatalf("object not stored under key %s", key)
	}
	if !strings.HasPrefix(string(bytes), "%PDF-") {
		t.Errorf("stored bytes missing PDF magic: %q", string(bytes[:5]))
	}
	if res.SizeBytes != int64(len(bytes)) {
		t.Errorf("size_bytes mismatch: result=%d actual=%d", res.SizeBytes, len(bytes))
	}
	if res.SHA256 == "" {
		t.Errorf("empty sha256")
	}
}

func TestExport_TopAppsExcel_StartsWithZipMagic(t *testing.T) {
	e, mc, _ := newTestExporter(false, 0)
	res, err := e.Export(context.Background(), adminPrincipal(), ExportRequest{
		ReportType: "top_apps",
		Format:     FormatExcel,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	bytes := mc.objects[ExportBucket+"/"+res.Key]
	if len(bytes) < 2 || bytes[0] != 'P' || bytes[1] != 'K' {
		t.Errorf("stored bytes missing PK (ZIP) magic")
	}
}

func TestExport_TrendPDF(t *testing.T) {
	e, _, _ := newTestExporter(false, 0)
	res, err := e.Export(context.Background(), adminPrincipal(), ExportRequest{
		ReportType: "trend",
		Format:     FormatPDF,
		Params: map[string]any{
			"metric": "active_minutes",
			"window": "7",
		},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !strings.Contains(res.Key, "trend") {
		t.Errorf("key should include report type: %s", res.Key)
	}
	if !strings.HasPrefix(res.Key, "tenant-1/") {
		t.Errorf("key should be tenant-scoped: %s", res.Key)
	}
}

func TestExport_RiskRequiresReporter(t *testing.T) {
	e, _, _ := newTestExporter(false, 0) // no risk
	_, err := e.Export(context.Background(), adminPrincipal(), ExportRequest{
		ReportType: "risk",
		Format:     FormatPDF,
	})
	if !errors.Is(err, ErrRiskUnavailable) {
		t.Errorf("err = %v, want ErrRiskUnavailable", err)
	}
}

func TestExport_RiskXLSX_Works(t *testing.T) {
	e, mc, _ := newTestExporter(true, 0)
	res, err := e.Export(context.Background(), adminPrincipal(), ExportRequest{
		ReportType: "risk",
		Format:     FormatExcel,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if _, ok := mc.objects[ExportBucket+"/"+res.Key]; !ok {
		t.Errorf("risk export not stored")
	}
}

// ---------------------------------------------------------------------------
// Error-path tests
// ---------------------------------------------------------------------------

func TestExport_UnknownReportType(t *testing.T) {
	e, _, _ := newTestExporter(false, 0)
	_, err := e.Export(context.Background(), adminPrincipal(), ExportRequest{
		ReportType: "bogus",
		Format:     FormatPDF,
	})
	if !errors.Is(err, ErrUnknownReport) {
		t.Errorf("err = %v, want ErrUnknownReport", err)
	}
}

func TestExport_UnknownFormat(t *testing.T) {
	e, _, _ := newTestExporter(false, 0)
	_, err := e.Export(context.Background(), adminPrincipal(), ExportRequest{
		ReportType: "top_apps",
		Format:     ExportFormat("html"),
	})
	if !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("err = %v, want ErrUnknownFormat", err)
	}
}

func TestExport_SizeCapEnforced(t *testing.T) {
	// 5-byte cap — any payload larger than that fails.
	e, _, _ := newTestExporter(false, 5)
	_, err := e.Export(context.Background(), adminPrincipal(), ExportRequest{
		ReportType: "top_apps",
		Format:     FormatPDF,
	})
	if !errors.Is(err, ErrExportTooLarge) {
		t.Errorf("err = %v, want ErrExportTooLarge", err)
	}
}

func TestExport_MissingPrincipalRejected(t *testing.T) {
	e, _, _ := newTestExporter(false, 0)
	_, err := e.Export(context.Background(), nil, ExportRequest{
		ReportType: "top_apps",
		Format:     FormatPDF,
	})
	if err == nil {
		t.Errorf("expected error for nil principal")
	}
}

func TestExport_TenantScopedKey(t *testing.T) {
	e, _, _ := newTestExporter(false, 0)
	p := &auth.Principal{UserID: "u", TenantID: "tenant-isolated", Roles: []string{"admin"}}
	res, err := e.Export(context.Background(), p, ExportRequest{
		ReportType: "top_apps",
		Format:     FormatPDF,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !strings.HasPrefix(res.Key, "tenant-isolated/") {
		t.Errorf("key not tenant-scoped: %s", res.Key)
	}
}

// ---------------------------------------------------------------------------
// Helper tests
// ---------------------------------------------------------------------------

func TestSanitiseText_StripsControl(t *testing.T) {
	in := "hello\x00world\x01\n"
	got := sanitiseText(in)
	if strings.ContainsAny(got, "\x00\x01") {
		t.Errorf("control bytes remained: %q", got)
	}
	if !strings.Contains(got, "hello") || !strings.Contains(got, "world") {
		t.Errorf("legitimate chars dropped: %q", got)
	}
}

func TestFooterText_ContainsWatermark(t *testing.T) {
	f := footerText("tenant-abc", time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC))
	if !strings.Contains(f, "KURUM İÇİ") {
		t.Errorf("missing Turkish watermark: %s", f)
	}
	if !strings.Contains(f, "tenant-abc") {
		t.Errorf("missing tenant id: %s", f)
	}
}

func TestStripKeystrokeFields_NoOp(t *testing.T) {
	rows := []TopAppRow{{AppName: "x", FocusSeconds: 1, FocusPct: 0}}
	got := stripKeystrokeFields(rows)
	if len(got) != 1 {
		t.Errorf("rows count changed: %d", len(got))
	}
}
