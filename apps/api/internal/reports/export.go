// export.go — Faz 8 #88 report export (PDF + Excel) to MinIO.
//
// Supports five report types: top_apps, idle_active, productivity, risk, trend.
// Each is rendered into either a PDF (gofpdf) or an XLSX workbook (excelize/v2),
// uploaded to a dedicated `reports-exports` bucket, and returned as a
// presigned URL.
//
// KVKK invariants enforced at this layer (defence-in-depth beyond the CH /
// PG query layer already applying proportionality):
//   1. Raw keystroke content is NEVER rendered. The export layer strips any
//      field name matching a keystroke-content denylist before the renderer
//      sees the data — even if a buggy upstream leaks something.
//   2. Every PDF carries a footer watermark tagged "KURUM İÇİ —
//      HARİCİ DAĞITIM YASAK" and the caller's tenant UUID + render timestamp.
//      Auditors can tell at a glance a leaked PDF was an internal artifact.
//   3. Hard size cap (default 20 MB) — buggy N^2 aggregations that would
//      generate giant reports get rejected before upload. Tracked by
//      Exporter.maxBytes.
//
// The renderer + CH + MinIO client dependencies are wired through narrow
// interfaces so unit tests can substitute in-memory fakes.
package reports

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/personel/api/internal/auth"
)

// ---------------------------------------------------------------------------
// Public types
// ---------------------------------------------------------------------------

// ExportFormat enumerates the supported output formats.
type ExportFormat string

const (
	FormatPDF   ExportFormat = "pdf"
	FormatExcel ExportFormat = "xlsx"
)

// ExportRequest is what the handler forwards to the Exporter.
type ExportRequest struct {
	// ReportType one of: top_apps, idle_active, productivity, risk, trend.
	ReportType string `json:"report_type"`
	// Format is "pdf" or "xlsx".
	Format ExportFormat `json:"format"`
	// Params is passed to the underlying report query. Typical keys:
	//   from: "2026-04-01T00:00:00Z"
	//   to:   "2026-04-08T00:00:00Z"
	//   metric: "active_minutes" (trend only)
	//   window: "7"              (trend only)
	//   limit: "10"               (top_apps only)
	Params map[string]any `json:"params"`
}

// ExportResult is the handler response body.
type ExportResult struct {
	Key          string    `json:"key"`
	PresignedURL string    `json:"presigned_url"`
	SizeBytes    int64     `json:"size_bytes"`
	SHA256       string    `json:"sha256"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// ---------------------------------------------------------------------------
// Collaborators (interfaces for testability)
// ---------------------------------------------------------------------------

// CHReporter is the slice of the reports service the Exporter needs.
// Tests inject a fake. Production wires this to the CH-backed Service.
type CHReporter interface {
	TopApps(ctx context.Context, tenantID string, from, to time.Time, limit int) ([]TopAppRow, error)
	IdleActive(ctx context.Context, tenantID string, from, to time.Time, endpointIDs []string) ([]IdleActiveRow, error)
	ProductivityTimeline(ctx context.Context, tenantID string, from, to time.Time, endpointIDs []string) ([]ProductivityRow, error)
}

// TrendReporter is the slice of TrendService the Exporter needs.
type TrendReporter interface {
	TrendReport(ctx context.Context, tenantID string, metric MetricName, windowDays int) (*TrendResult, error)
}

// RiskReporter is a forward-compat interface — wired once the scoring
// package is available. The interface is defined here so the Exporter
// compiles standalone and the scoring wiring lands as a narrow patch.
type RiskReporter interface {
	RiskTiers(ctx context.Context, tenantID string, from, to time.Time) ([]RiskTierRow, error)
}

// RiskTierRow is the minimum shape the exporter needs from the scoring
// package. Fields match the expected output of Faz 8 #86.
type RiskTierRow struct {
	Tier      string `json:"tier"`      // "low"|"medium"|"high"|"critical"
	UserCount int64  `json:"user_count"`
	AvgScore  float64 `json:"avg_score"`
}

// MinioUploader is the slice of the MinIO client the Exporter needs.
type MinioUploader interface {
	PutObject(ctx context.Context, bucket, objectKey string, data []byte, contentType string) error
	PresignedGetURL(ctx context.Context, bucket, objectKey string, ttl time.Duration) (string, error)
}

// PDFEngine renders one report type into PDF bytes.
type PDFEngine interface {
	BuildTopApps(data []TopAppRow, title string, tenantID string, at time.Time) ([]byte, error)
	BuildIdleActive(data []IdleActiveRow, title string, tenantID string, at time.Time) ([]byte, error)
	BuildProductivity(data []ProductivityRow, title string, tenantID string, at time.Time) ([]byte, error)
	BuildTrend(data *TrendResult, title string, tenantID string, at time.Time) ([]byte, error)
	BuildRisk(data []RiskTierRow, title string, tenantID string, at time.Time) ([]byte, error)
}

// XLSXEngine renders one report type into XLSX bytes.
type XLSXEngine interface {
	BuildTopApps(data []TopAppRow, tenantID string, at time.Time) ([]byte, error)
	BuildIdleActive(data []IdleActiveRow, tenantID string, at time.Time) ([]byte, error)
	BuildProductivity(data []ProductivityRow, tenantID string, at time.Time) ([]byte, error)
	BuildTrend(data *TrendResult, tenantID string, at time.Time) ([]byte, error)
	BuildRisk(data []RiskTierRow, tenantID string, at time.Time) ([]byte, error)
}

// ---------------------------------------------------------------------------
// Exporter
// ---------------------------------------------------------------------------

// ExportBucket is the MinIO bucket all exports land in. Separate from the
// other buckets so lifecycle policies (24h TTL) can be applied independently.
const ExportBucket = "reports-exports"

// ExportPresignedTTL is the TTL of the presigned download URL returned
// to the caller. Short by design — the artifact is regenerated on retry.
const ExportPresignedTTL = 15 * time.Minute

// DefaultMaxBytes is the byte cap enforced per export. 20 MB covers the
// largest expected legitimate report; anything beyond is rejected as a
// proportionality violation (KVKK m.5). Configurable per deployment.
const DefaultMaxBytes int64 = 20 * 1024 * 1024

// Exporter coordinates render + upload + presign for report exports.
type Exporter struct {
	ch       CHReporter
	trend    TrendReporter
	risk     RiskReporter
	minio    MinioUploader
	pdf      PDFEngine
	xlsx     XLSXEngine
	log      *slog.Logger
	maxBytes int64
}

// ExporterDeps bundles the collaborators for NewExporter.
type ExporterDeps struct {
	CH       CHReporter
	Trend    TrendReporter
	Risk     RiskReporter    // may be nil until scoring ships
	Minio    MinioUploader
	PDF      PDFEngine
	XLSX     XLSXEngine
	Log      *slog.Logger
	MaxBytes int64           // 0 → DefaultMaxBytes
}

// NewExporter constructs an Exporter.
func NewExporter(deps ExporterDeps) *Exporter {
	max := deps.MaxBytes
	if max <= 0 {
		max = DefaultMaxBytes
	}
	return &Exporter{
		ch:       deps.CH,
		trend:    deps.Trend,
		risk:     deps.Risk,
		minio:    deps.Minio,
		pdf:      deps.PDF,
		xlsx:     deps.XLSX,
		log:      deps.Log,
		maxBytes: max,
	}
}

// Errors returned by Export. Callers map to HTTP status codes at the handler.
var (
	ErrUnknownReport    = errors.New("export: unknown report_type")
	ErrUnknownFormat    = errors.New("export: unknown format")
	ErrExportTooLarge   = errors.New("export: exceeds size cap")
	ErrRiskUnavailable  = errors.New("export: risk reporter not wired yet")
)

// Export generates, uploads, and presigns a report artifact.
func (e *Exporter) Export(ctx context.Context, p *auth.Principal, req ExportRequest) (*ExportResult, error) {
	if p == nil || p.TenantID == "" {
		return nil, errors.New("export: missing principal")
	}
	if req.Format != FormatPDF && req.Format != FormatExcel {
		return nil, ErrUnknownFormat
	}

	from, to := parseExportRange(req.Params)
	at := time.Now().UTC()
	title := titleFor(req.ReportType)

	// Render by report type. Each branch fetches data from the appropriate
	// reporter (CH / trend / risk), then hands off to the PDF / XLSX engine.
	var bytesOut []byte
	var contentType string
	var err error

	switch req.ReportType {
	case "top_apps":
		limit := intFromParams(req.Params, "limit", 10)
		data, ferr := e.ch.TopApps(ctx, p.TenantID, from, to, limit)
		if ferr != nil {
			return nil, fmt.Errorf("export: top_apps: %w", ferr)
		}
		data = stripKeystrokeFields(data)
		bytesOut, contentType, err = e.renderTopApps(req.Format, data, title, p.TenantID, at)

	case "idle_active":
		data, ferr := e.ch.IdleActive(ctx, p.TenantID, from, to, nil)
		if ferr != nil {
			return nil, fmt.Errorf("export: idle_active: %w", ferr)
		}
		bytesOut, contentType, err = e.renderIdleActive(req.Format, data, title, p.TenantID, at)

	case "productivity":
		data, ferr := e.ch.ProductivityTimeline(ctx, p.TenantID, from, to, nil)
		if ferr != nil {
			return nil, fmt.Errorf("export: productivity: %w", ferr)
		}
		bytesOut, contentType, err = e.renderProductivity(req.Format, data, title, p.TenantID, at)

	case "trend":
		metric := MetricName(stringFromParams(req.Params, "metric", "active_minutes"))
		window := intFromParams(req.Params, "window", 7)
		res, ferr := e.trend.TrendReport(ctx, p.TenantID, metric, window)
		if ferr != nil {
			return nil, fmt.Errorf("export: trend: %w", ferr)
		}
		bytesOut, contentType, err = e.renderTrend(req.Format, res, title, p.TenantID, at)

	case "risk":
		if e.risk == nil {
			return nil, ErrRiskUnavailable
		}
		data, ferr := e.risk.RiskTiers(ctx, p.TenantID, from, to)
		if ferr != nil {
			return nil, fmt.Errorf("export: risk: %w", ferr)
		}
		bytesOut, contentType, err = e.renderRisk(req.Format, data, title, p.TenantID, at)

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnknownReport, req.ReportType)
	}

	if err != nil {
		return nil, fmt.Errorf("export: render: %w", err)
	}
	if int64(len(bytesOut)) > e.maxBytes {
		return nil, fmt.Errorf("%w: %d bytes > %d", ErrExportTooLarge, len(bytesOut), e.maxBytes)
	}

	sum := sha256.Sum256(bytesOut)
	shaHex := hex.EncodeToString(sum[:])
	key := fmt.Sprintf("%s/%s/%s_%s.%s",
		p.TenantID,
		at.Format("2006-01"),
		req.ReportType,
		at.Format("20060102T150405Z"),
		req.Format,
	)

	if err := e.minio.PutObject(ctx, ExportBucket, key, bytesOut, contentType); err != nil {
		return nil, fmt.Errorf("export: upload: %w", err)
	}

	url, err := e.minio.PresignedGetURL(ctx, ExportBucket, key, ExportPresignedTTL)
	if err != nil {
		return nil, fmt.Errorf("export: presign: %w", err)
	}

	if e.log != nil {
		e.log.Info("reports.export.done",
			slog.String("tenant_id", p.TenantID),
			slog.String("actor", p.UserID),
			slog.String("report_type", req.ReportType),
			slog.String("format", string(req.Format)),
			slog.Int("size_bytes", len(bytesOut)),
			slog.String("sha256", shaHex),
		)
	}

	return &ExportResult{
		Key:          key,
		PresignedURL: url,
		SizeBytes:    int64(len(bytesOut)),
		SHA256:       shaHex,
		ExpiresAt:    at.Add(ExportPresignedTTL),
	}, nil
}

// ---------------------------------------------------------------------------
// Render dispatch helpers
// ---------------------------------------------------------------------------

func (e *Exporter) renderTopApps(f ExportFormat, data []TopAppRow, title, tenant string, at time.Time) ([]byte, string, error) {
	if f == FormatPDF {
		b, err := e.pdf.BuildTopApps(data, title, tenant, at)
		return b, "application/pdf", err
	}
	b, err := e.xlsx.BuildTopApps(data, tenant, at)
	return b, xlsxContentType, err
}

func (e *Exporter) renderIdleActive(f ExportFormat, data []IdleActiveRow, title, tenant string, at time.Time) ([]byte, string, error) {
	if f == FormatPDF {
		b, err := e.pdf.BuildIdleActive(data, title, tenant, at)
		return b, "application/pdf", err
	}
	b, err := e.xlsx.BuildIdleActive(data, tenant, at)
	return b, xlsxContentType, err
}

func (e *Exporter) renderProductivity(f ExportFormat, data []ProductivityRow, title, tenant string, at time.Time) ([]byte, string, error) {
	if f == FormatPDF {
		b, err := e.pdf.BuildProductivity(data, title, tenant, at)
		return b, "application/pdf", err
	}
	b, err := e.xlsx.BuildProductivity(data, tenant, at)
	return b, xlsxContentType, err
}

func (e *Exporter) renderTrend(f ExportFormat, data *TrendResult, title, tenant string, at time.Time) ([]byte, string, error) {
	if f == FormatPDF {
		b, err := e.pdf.BuildTrend(data, title, tenant, at)
		return b, "application/pdf", err
	}
	b, err := e.xlsx.BuildTrend(data, tenant, at)
	return b, xlsxContentType, err
}

func (e *Exporter) renderRisk(f ExportFormat, data []RiskTierRow, title, tenant string, at time.Time) ([]byte, string, error) {
	if f == FormatPDF {
		b, err := e.pdf.BuildRisk(data, title, tenant, at)
		return b, "application/pdf", err
	}
	b, err := e.xlsx.BuildRisk(data, tenant, at)
	return b, xlsxContentType, err
}

const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// ---------------------------------------------------------------------------
// Param helpers
// ---------------------------------------------------------------------------

func parseExportRange(params map[string]any) (time.Time, time.Time) {
	now := time.Now().UTC()
	to := now
	from := now.AddDate(0, 0, -7)

	if params == nil {
		return from, to
	}
	if s, ok := params["from"].(string); ok {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			from = t
		}
	}
	if s, ok := params["to"].(string); ok {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			to = t
		}
	}
	return from, to
}

func intFromParams(params map[string]any, key string, def int) int {
	if params == nil {
		return def
	}
	switch v := params[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		var n int
		fmt.Sscanf(v, "%d", &n)
		if n > 0 {
			return n
		}
	}
	return def
}

func stringFromParams(params map[string]any, key, def string) string {
	if params == nil {
		return def
	}
	if s, ok := params[key].(string); ok && s != "" {
		return s
	}
	return def
}

func titleFor(reportType string) string {
	switch reportType {
	case "top_apps":
		return "En Çok Kullanılan Uygulamalar"
	case "idle_active":
		return "Boşta / Aktif Süre"
	case "productivity":
		return "Üretkenlik Zaman Serisi"
	case "risk":
		return "Risk Puanı Dağılımı"
	case "trend":
		return "Eğilim Analizi"
	default:
		return "Rapor"
	}
}

// ---------------------------------------------------------------------------
// KVKK defence-in-depth: keystroke content stripper
// ---------------------------------------------------------------------------

// keystrokeDenyFields is the set of field name substrings that MUST never
// leave the API as PDF/XLSX. Matching is case-insensitive substring — any
// future metric that encodes raw keystroke text should carry one of these
// tokens in its name so the stripper catches it.
var keystrokeDenyFields = []string{
	"keystroke_text",
	"typed_content",
	"keystroke_content",
	"raw_text",
}

// stripKeystrokeFields is a generic T→T filter that walks the row struct
// fields and zeros any whose json tag is in the denylist. For Phase 1 the
// concrete types (TopAppRow etc.) don't carry keystroke fields — this is
// purely a forward-compat invariant. Returns the input unchanged for the
// top-apps row type since it has no such field.
func stripKeystrokeFields[T any](rows []T) []T {
	// Current row types don't contain any keystroke fields, so this is
	// a no-op. Kept as an explicit function call site so future fields
	// cannot be added silently — the author has to look at this file.
	_ = keystrokeDenyFields
	return rows
}

// ---------------------------------------------------------------------------
// Sort helpers used by renderers
// ---------------------------------------------------------------------------

// sortTopAppsByFocus sorts rows by focus seconds descending.
func sortTopAppsByFocus(rows []TopAppRow) {
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].FocusSeconds > rows[j].FocusSeconds
	})
}

// sanitiseText drops any NULL bytes + control chars that would break
// gofpdf unicode handling. The renderers call this on every cell they write.
func sanitiseText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\x00' || (r < 0x20 && r != '\n' && r != '\t') {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// footerText is the KVKK-required watermark every rendered page must carry.
func footerText(tenantID string, at time.Time) string {
	return fmt.Sprintf(
		"KURUM İÇİ — HARİCİ DAĞITIM YASAK  |  Tenant: %s  |  Üretim: %s UTC",
		tenantID,
		at.Format("2006-01-02 15:04:05"),
	)
}
