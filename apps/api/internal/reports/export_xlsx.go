// export_xlsx.go — excelize/v2 implementation of the XLSXEngine interface.
//
// Dependency: github.com/xuri/excelize/v2 v2.8.x (pure Go, BSD).
// NOT yet added to go.mod — this file will fail to compile until a parent
// session runs `go mod tidy` after this scaffold lands.
//
// Every workbook has two sheets:
//   Sheet 1 ("Rapor")    — the data
//   Sheet 2 ("Metadata") — tenant_id, generated_at, watermark notice
package reports

import (
	"bytes"
	"fmt"
	"time"

	"github.com/xuri/excelize/v2"
)

type excelizeEngine struct{}

// NewExcelizeEngine returns the default XLSXEngine implementation.
func NewExcelizeEngine() XLSXEngine { return &excelizeEngine{} }

const (
	dataSheet = "Rapor"
	metaSheet = "Metadata"
)

// newWorkbook creates a workbook pre-populated with the metadata sheet.
// Callers receive a handle + the empty data sheet name.
func newWorkbook(tenantID string, at time.Time, reportLabel string) *excelize.File {
	f := excelize.NewFile()
	// Rename default sheet to "Rapor"
	_ = f.SetSheetName("Sheet1", dataSheet)
	// Metadata sheet
	_, _ = f.NewSheet(metaSheet)
	_ = f.SetCellValue(metaSheet, "A1", "Rapor")
	_ = f.SetCellValue(metaSheet, "B1", reportLabel)
	_ = f.SetCellValue(metaSheet, "A2", "Tenant")
	_ = f.SetCellValue(metaSheet, "B2", tenantID)
	_ = f.SetCellValue(metaSheet, "A3", "Üretim Zamanı (UTC)")
	_ = f.SetCellValue(metaSheet, "B3", at.Format("2006-01-02 15:04:05"))
	_ = f.SetCellValue(metaSheet, "A4", "Sınıflandırma")
	_ = f.SetCellValue(metaSheet, "B4", "KURUM İÇİ — HARİCİ DAĞITIM YASAK")
	return f
}

func finishWorkbook(f *excelize.File) ([]byte, error) {
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("excelize: write: %w", err)
	}
	return buf.Bytes(), nil
}

// writeHeader writes a bold header row starting at row 1.
func writeHeader(f *excelize.File, sheet string, headers []string) error {
	for i, h := range headers {
		cell := fmt.Sprintf("%s1", columnLetter(i))
		if err := f.SetCellValue(sheet, cell, h); err != nil {
			return err
		}
	}
	return nil
}

// columnLetter returns the Excel column letter for zero-based index.
// Handles up to ZZ. Good enough for our small report schemas.
func columnLetter(idx int) string {
	if idx < 26 {
		return string(rune('A' + idx))
	}
	return string(rune('A'+(idx/26)-1)) + string(rune('A'+(idx%26)))
}

// ---------------------------------------------------------------------------
// Top Apps
// ---------------------------------------------------------------------------

func (e *excelizeEngine) BuildTopApps(data []TopAppRow, tenantID string, at time.Time) ([]byte, error) {
	f := newWorkbook(tenantID, at, "En Çok Kullanılan Uygulamalar")
	if err := writeHeader(f, dataSheet, []string{"Uygulama", "Odak Süresi (sn)", "Yüzde"}); err != nil {
		return nil, err
	}
	rows := append([]TopAppRow(nil), data...)
	sortTopAppsByFocus(rows)
	for i, r := range rows {
		row := i + 2
		_ = f.SetCellValue(dataSheet, fmt.Sprintf("A%d", row), sanitiseText(r.AppName))
		_ = f.SetCellValue(dataSheet, fmt.Sprintf("B%d", row), r.FocusSeconds)
		_ = f.SetCellValue(dataSheet, fmt.Sprintf("C%d", row), r.FocusPct)
	}
	return finishWorkbook(f)
}

// ---------------------------------------------------------------------------
// Idle / Active
// ---------------------------------------------------------------------------

func (e *excelizeEngine) BuildIdleActive(data []IdleActiveRow, tenantID string, at time.Time) ([]byte, error) {
	f := newWorkbook(tenantID, at, "Boşta / Aktif Süre")
	if err := writeHeader(f, dataSheet, []string{"Tarih", "Endpoint", "Aktif (sn)", "Boşta (sn)", "Oran"}); err != nil {
		return nil, err
	}
	for i, r := range data {
		row := i + 2
		_ = f.SetCellValue(dataSheet, fmt.Sprintf("A%d", row), r.Date.Format("2006-01-02"))
		_ = f.SetCellValue(dataSheet, fmt.Sprintf("B%d", row), sanitiseText(r.EndpointID))
		_ = f.SetCellValue(dataSheet, fmt.Sprintf("C%d", row), r.ActiveSeconds)
		_ = f.SetCellValue(dataSheet, fmt.Sprintf("D%d", row), r.IdleSeconds)
		_ = f.SetCellValue(dataSheet, fmt.Sprintf("E%d", row), r.ActiveRatio)
	}
	return finishWorkbook(f)
}

// ---------------------------------------------------------------------------
// Productivity
// ---------------------------------------------------------------------------

func (e *excelizeEngine) BuildProductivity(data []ProductivityRow, tenantID string, at time.Time) ([]byte, error) {
	f := newWorkbook(tenantID, at, "Üretkenlik Zaman Serisi")
	if err := writeHeader(f, dataSheet, []string{"Saat", "Endpoint", "Aktif (sn)", "Boşta (sn)"}); err != nil {
		return nil, err
	}
	for i, r := range data {
		row := i + 2
		_ = f.SetCellValue(dataSheet, fmt.Sprintf("A%d", row), r.Hour.Format(time.RFC3339))
		_ = f.SetCellValue(dataSheet, fmt.Sprintf("B%d", row), sanitiseText(r.EndpointID))
		_ = f.SetCellValue(dataSheet, fmt.Sprintf("C%d", row), r.ActiveSeconds)
		_ = f.SetCellValue(dataSheet, fmt.Sprintf("D%d", row), r.IdleSeconds)
	}
	return finishWorkbook(f)
}

// ---------------------------------------------------------------------------
// Trend
// ---------------------------------------------------------------------------

func (e *excelizeEngine) BuildTrend(data *TrendResult, tenantID string, at time.Time) ([]byte, error) {
	f := newWorkbook(tenantID, at, "Eğilim Analizi")
	if err := writeHeader(f, dataSheet, []string{"Alan", "Değer"}); err != nil {
		return nil, err
	}
	if data == nil {
		_ = f.SetCellValue(dataSheet, "A2", "veri_yok")
		_ = f.SetCellValue(dataSheet, "B2", "-")
		return finishWorkbook(f)
	}

	kv := [][2]any{
		{"metric", string(data.Metric)},
		{"window_days", data.WindowDays},
		{"current_mean", data.CurrentPeriod.Mean},
		{"current_median", data.CurrentPeriod.Median},
		{"current_p95", data.CurrentPeriod.P95},
		{"current_sum", data.CurrentPeriod.Sum},
		{"current_count", data.CurrentPeriod.Count},
		{"previous_mean", data.PreviousPeriod.Mean},
		{"previous_median", data.PreviousPeriod.Median},
		{"previous_p95", data.PreviousPeriod.P95},
		{"previous_sum", data.PreviousPeriod.Sum},
		{"previous_count", data.PreviousPeriod.Count},
		{"delta", data.Delta},
		{"direction", data.Direction},
		{"z_score", data.ZScore},
		{"anomaly", data.Anomaly},
	}
	for i, pair := range kv {
		row := i + 2
		_ = f.SetCellValue(dataSheet, fmt.Sprintf("A%d", row), pair[0])
		_ = f.SetCellValue(dataSheet, fmt.Sprintf("B%d", row), pair[1])
	}
	return finishWorkbook(f)
}

// ---------------------------------------------------------------------------
// Risk
// ---------------------------------------------------------------------------

func (e *excelizeEngine) BuildRisk(data []RiskTierRow, tenantID string, at time.Time) ([]byte, error) {
	f := newWorkbook(tenantID, at, "Risk Puanı Dağılımı")
	if err := writeHeader(f, dataSheet, []string{"Risk Seviyesi", "Kullanıcı Sayısı", "Ortalama Skor"}); err != nil {
		return nil, err
	}
	for i, r := range data {
		row := i + 2
		_ = f.SetCellValue(dataSheet, fmt.Sprintf("A%d", row), sanitiseText(r.Tier))
		_ = f.SetCellValue(dataSheet, fmt.Sprintf("B%d", row), r.UserCount)
		_ = f.SetCellValue(dataSheet, fmt.Sprintf("C%d", row), r.AvgScore)
	}
	return finishWorkbook(f)
}
