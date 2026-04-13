// export_pdf.go — gofpdf implementation of the PDFEngine interface.
//
// Dependency: github.com/jung-kurt/gofpdf v1.16.2 (pure Go, MIT).
// NOT yet added to go.mod — this file will fail to compile until a
// parent session runs `go mod tidy` after this scaffold lands.
//
// Design notes:
//   - Pure Go, no cgo, ships on all 3 host OSes.
//   - Turkish characters: gofpdf uses Latin-1 by default. We register the
//     built-in "Helvetica" font with CP1254 (Turkish) encoding so İ/ş/ğ/ç/ü
//     render without requiring a font file on disk. For broader coverage
//     (ex: Arabic / Asian) a TTF font would need to be added under
//     infra/compose/api/fonts — tracked as tech debt (§10).
//   - Every page ends with the KVKK footer watermark via AddFunc(Footer).
package reports

import (
	"bytes"
	"fmt"
	"time"

	"github.com/jung-kurt/gofpdf"
)

// gofpdfEngine renders reports using gofpdf.
type gofpdfEngine struct{}

// NewGofpdfEngine returns the default PDFEngine implementation.
func NewGofpdfEngine() PDFEngine { return &gofpdfEngine{} }

// ---------------------------------------------------------------------------
// Common PDF document scaffolding
// ---------------------------------------------------------------------------

func (g *gofpdfEngine) newDoc(title, tenantID string, at time.Time) *gofpdf.Fpdf {
	pdf := gofpdf.New("P", "mm", "A4", "")
	// CP1254 = Windows Turkish. gofpdf ships a translator for it.
	tr := pdf.UnicodeTranslatorFromDescriptor("cp1254")

	pdf.SetFooterFunc(func() {
		pdf.SetY(-15)
		pdf.SetFont("Helvetica", "I", 8)
		pdf.SetTextColor(120, 120, 120)
		pdf.CellFormat(0, 10, tr(sanitiseText(footerText(tenantID, at))), "", 0, "C", false, 0, "")
	})

	pdf.AddPage()

	// Title
	pdf.SetFont("Helvetica", "B", 16)
	pdf.SetTextColor(0, 0, 0)
	pdf.CellFormat(0, 10, tr(sanitiseText(title)), "", 1, "L", false, 0, "")
	pdf.Ln(2)

	// Subtitle: tenant + render timestamp
	pdf.SetFont("Helvetica", "", 9)
	pdf.SetTextColor(80, 80, 80)
	pdf.CellFormat(0, 6, tr(fmt.Sprintf("Tenant: %s  |  Rapor üretim zamanı: %s UTC",
		tenantID, at.Format("2006-01-02 15:04:05"))), "", 1, "L", false, 0, "")
	pdf.Ln(3)
	return pdf
}

func (g *gofpdfEngine) finishDoc(pdf *gofpdf.Fpdf) ([]byte, error) {
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("gofpdf: output: %w", err)
	}
	return buf.Bytes(), nil
}

// ---------------------------------------------------------------------------
// Top Apps
// ---------------------------------------------------------------------------

func (g *gofpdfEngine) BuildTopApps(data []TopAppRow, title, tenantID string, at time.Time) ([]byte, error) {
	pdf := g.newDoc(title, tenantID, at)
	tr := pdf.UnicodeTranslatorFromDescriptor("cp1254")

	// Header row
	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetFillColor(230, 230, 230)
	pdf.CellFormat(90, 7, tr("Uygulama"), "1", 0, "L", true, 0, "")
	pdf.CellFormat(40, 7, tr("Odak Süresi (sn)"), "1", 0, "R", true, 0, "")
	pdf.CellFormat(30, 7, tr("Yüzde"), "1", 1, "R", true, 0, "")

	pdf.SetFont("Helvetica", "", 10)
	// sort by focus desc before rendering
	rows := append([]TopAppRow(nil), data...)
	sortTopAppsByFocus(rows)
	for _, r := range rows {
		pdf.CellFormat(90, 6, tr(sanitiseText(r.AppName)), "1", 0, "L", false, 0, "")
		pdf.CellFormat(40, 6, fmt.Sprintf("%d", r.FocusSeconds), "1", 0, "R", false, 0, "")
		pdf.CellFormat(30, 6, fmt.Sprintf("%.1f%%", r.FocusPct), "1", 1, "R", false, 0, "")
	}

	return g.finishDoc(pdf)
}

// ---------------------------------------------------------------------------
// Idle / Active
// ---------------------------------------------------------------------------

func (g *gofpdfEngine) BuildIdleActive(data []IdleActiveRow, title, tenantID string, at time.Time) ([]byte, error) {
	pdf := g.newDoc(title, tenantID, at)
	tr := pdf.UnicodeTranslatorFromDescriptor("cp1254")

	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetFillColor(230, 230, 230)
	pdf.CellFormat(40, 7, tr("Tarih"), "1", 0, "L", true, 0, "")
	pdf.CellFormat(60, 7, tr("Endpoint"), "1", 0, "L", true, 0, "")
	pdf.CellFormat(30, 7, tr("Aktif (sn)"), "1", 0, "R", true, 0, "")
	pdf.CellFormat(30, 7, tr("Boşta (sn)"), "1", 0, "R", true, 0, "")
	pdf.CellFormat(30, 7, tr("Oran"), "1", 1, "R", true, 0, "")

	pdf.SetFont("Helvetica", "", 10)
	for _, r := range data {
		pdf.CellFormat(40, 6, r.Date.Format("2006-01-02"), "1", 0, "L", false, 0, "")
		pdf.CellFormat(60, 6, sanitiseText(r.EndpointID), "1", 0, "L", false, 0, "")
		pdf.CellFormat(30, 6, fmt.Sprintf("%d", r.ActiveSeconds), "1", 0, "R", false, 0, "")
		pdf.CellFormat(30, 6, fmt.Sprintf("%d", r.IdleSeconds), "1", 0, "R", false, 0, "")
		pdf.CellFormat(30, 6, fmt.Sprintf("%.2f", r.ActiveRatio), "1", 1, "R", false, 0, "")
	}
	return g.finishDoc(pdf)
}

// ---------------------------------------------------------------------------
// Productivity
// ---------------------------------------------------------------------------

func (g *gofpdfEngine) BuildProductivity(data []ProductivityRow, title, tenantID string, at time.Time) ([]byte, error) {
	pdf := g.newDoc(title, tenantID, at)
	tr := pdf.UnicodeTranslatorFromDescriptor("cp1254")

	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetFillColor(230, 230, 230)
	pdf.CellFormat(50, 7, tr("Saat"), "1", 0, "L", true, 0, "")
	pdf.CellFormat(60, 7, tr("Endpoint"), "1", 0, "L", true, 0, "")
	pdf.CellFormat(30, 7, tr("Aktif (sn)"), "1", 0, "R", true, 0, "")
	pdf.CellFormat(30, 7, tr("Boşta (sn)"), "1", 1, "R", true, 0, "")

	pdf.SetFont("Helvetica", "", 10)
	for _, r := range data {
		pdf.CellFormat(50, 6, r.Hour.Format("2006-01-02 15:04"), "1", 0, "L", false, 0, "")
		pdf.CellFormat(60, 6, sanitiseText(r.EndpointID), "1", 0, "L", false, 0, "")
		pdf.CellFormat(30, 6, fmt.Sprintf("%d", r.ActiveSeconds), "1", 0, "R", false, 0, "")
		pdf.CellFormat(30, 6, fmt.Sprintf("%d", r.IdleSeconds), "1", 1, "R", false, 0, "")
	}
	return g.finishDoc(pdf)
}

// ---------------------------------------------------------------------------
// Trend
// ---------------------------------------------------------------------------

func (g *gofpdfEngine) BuildTrend(data *TrendResult, title, tenantID string, at time.Time) ([]byte, error) {
	pdf := g.newDoc(title, tenantID, at)
	tr := pdf.UnicodeTranslatorFromDescriptor("cp1254")

	if data == nil {
		pdf.SetFont("Helvetica", "I", 10)
		pdf.CellFormat(0, 6, tr("Veri bulunamadı."), "", 1, "L", false, 0, "")
		return g.finishDoc(pdf)
	}

	pdf.SetFont("Helvetica", "B", 12)
	pdf.CellFormat(0, 7, tr(fmt.Sprintf("Metrik: %s  |  Pencere: %d gün", data.Metric, data.WindowDays)), "", 1, "L", false, 0, "")
	pdf.Ln(2)

	addSnapshotSection := func(heading string, snap MetricSnapshot) {
		pdf.SetFont("Helvetica", "B", 10)
		pdf.CellFormat(0, 6, tr(heading), "", 1, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 10)
		pdf.CellFormat(0, 5, fmt.Sprintf("Ortalama: %.2f  Medyan: %.2f  P95: %.2f  Toplam: %d  Adet: %d",
			snap.Mean, snap.Median, snap.P95, snap.Sum, snap.Count), "", 1, "L", false, 0, "")
		pdf.Ln(1)
	}

	addSnapshotSection("Mevcut Dönem", data.CurrentPeriod)
	addSnapshotSection("Önceki Dönem", data.PreviousPeriod)

	pdf.SetFont("Helvetica", "B", 10)
	pdf.CellFormat(0, 6, tr("Değişim"), "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 10)
	pdf.CellFormat(0, 5, fmt.Sprintf("Delta: %.2f%%  Yön: %s  Z-Skor: %.2f  Anormal: %v",
		data.Delta*100, data.Direction, data.ZScore, data.Anomaly), "", 1, "L", false, 0, "")

	return g.finishDoc(pdf)
}

// ---------------------------------------------------------------------------
// Risk
// ---------------------------------------------------------------------------

func (g *gofpdfEngine) BuildRisk(data []RiskTierRow, title, tenantID string, at time.Time) ([]byte, error) {
	pdf := g.newDoc(title, tenantID, at)
	tr := pdf.UnicodeTranslatorFromDescriptor("cp1254")

	pdf.SetFont("Helvetica", "B", 10)
	pdf.SetFillColor(230, 230, 230)
	pdf.CellFormat(50, 7, tr("Risk Seviyesi"), "1", 0, "L", true, 0, "")
	pdf.CellFormat(50, 7, tr("Kullanıcı Sayısı"), "1", 0, "R", true, 0, "")
	pdf.CellFormat(50, 7, tr("Ortalama Skor"), "1", 1, "R", true, 0, "")

	pdf.SetFont("Helvetica", "", 10)
	for _, r := range data {
		pdf.CellFormat(50, 6, tr(sanitiseText(r.Tier)), "1", 0, "L", false, 0, "")
		pdf.CellFormat(50, 6, fmt.Sprintf("%d", r.UserCount), "1", 0, "R", false, 0, "")
		pdf.CellFormat(50, 6, fmt.Sprintf("%.2f", r.AvgScore), "1", 1, "R", false, 0, "")
	}
	return g.finishDoc(pdf)
}
