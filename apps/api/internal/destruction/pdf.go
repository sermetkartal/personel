// Package destruction — PDF generation for destruction reports.
// Uses gofpdf to produce a structured PDF. The caller signs the bytes.
package destruction

import (
	"bytes"
	"fmt"

	"github.com/jung-kurt/gofpdf"
)

// PDFGenerator builds destruction report PDFs.
type PDFGenerator struct{}

// NewPDFGenerator creates a PDFGenerator.
func NewPDFGenerator() *PDFGenerator {
	return &PDFGenerator{}
}

// Generate produces the PDF bytes for a destruction report manifest.
func (g *PDFGenerator) Generate(m *Manifest, tenantID, period string) ([]byte, error) {
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetAuthor("Personel Platform — Audit System", true)
	pdf.SetTitle(fmt.Sprintf("Veri İmha Raporu — %s — %s", tenantID[:8], period), true)

	pdf.AddPage()
	pdf.SetFont("Arial", "B", 16)
	pdf.Cell(0, 10, "Kisisel Veri Imha Raporu (KVKK m.7)")
	pdf.Ln(12)

	pdf.SetFont("Arial", "", 11)
	pdf.Cell(0, 8, fmt.Sprintf("Donem: %s", period))
	pdf.Ln(6)
	pdf.Cell(0, 8, fmt.Sprintf("Donem Baslangic: %s", m.PeriodStart.Format("2006-01-02")))
	pdf.Ln(6)
	pdf.Cell(0, 8, fmt.Sprintf("Donem Bitis: %s", m.PeriodEnd.Format("2006-01-02")))
	pdf.Ln(10)

	// Category deletions table.
	pdf.SetFont("Arial", "B", 12)
	pdf.Cell(0, 8, "1. Veri Kategorisi Bazinda Silinen Kayitlar")
	pdf.Ln(8)

	pdf.SetFont("Arial", "B", 10)
	pdf.CellFormat(80, 7, "Kategori", "1", 0, "L", false, 0, "")
	pdf.CellFormat(40, 7, "Satir Sayisi", "1", 0, "R", false, 0, "")
	pdf.CellFormat(40, 7, "Depo", "1", 1, "L", false, 0, "")

	pdf.SetFont("Arial", "", 10)
	for _, cd := range m.CategoryDeletions {
		pdf.CellFormat(80, 6, cd.Category, "1", 0, "L", false, 0, "")
		pdf.CellFormat(40, 6, fmt.Sprintf("%d", cd.RowCount), "1", 0, "R", false, 0, "")
		pdf.CellFormat(40, 6, cd.Store, "1", 1, "L", false, 0, "")
	}
	pdf.Ln(6)

	// Legal hold events.
	pdf.SetFont("Arial", "B", 12)
	pdf.Cell(0, 8, "2. Yasal Saklama (Legal Hold) Olaylari")
	pdf.Ln(8)

	pdf.SetFont("Arial", "B", 10)
	pdf.CellFormat(50, 7, "Hold ID", "1", 0, "L", false, 0, "")
	pdf.CellFormat(30, 7, "Islem", "1", 0, "L", false, 0, "")
	pdf.CellFormat(50, 7, "Tarih", "1", 0, "L", false, 0, "")
	pdf.CellFormat(50, 7, "Neden Kodu", "1", 1, "L", false, 0, "")

	pdf.SetFont("Arial", "", 10)
	for _, e := range m.LegalHoldEvents {
		pdf.CellFormat(50, 6, e.HoldID[:8]+"...", "1", 0, "L", false, 0, "")
		pdf.CellFormat(30, 6, e.EventType, "1", 0, "L", false, 0, "")
		pdf.CellFormat(50, 6, e.OccurredAt.Format("2006-01-02"), "1", 0, "L", false, 0, "")
		pdf.CellFormat(50, 6, e.ReasonCode, "1", 1, "L", false, 0, "")
	}
	pdf.Ln(6)

	// Outstanding holds summary.
	pdf.SetFont("Arial", "B", 12)
	pdf.Cell(0, 8, fmt.Sprintf("3. Donem Sonu Aktif Legal Hold Sayisi: %d", len(m.OutstandingHolds)))
	pdf.Ln(10)

	// DSR-triggered deletions.
	pdf.SetFont("Arial", "B", 12)
	pdf.Cell(0, 8, fmt.Sprintf("4. DSR Kaynakli Silme Talepleri: %d", len(m.DSRTriggeredDeletions)))
	pdf.Ln(10)

	// Signature notice.
	pdf.SetFont("Arial", "I", 9)
	pdf.Cell(0, 6, "Bu rapor, kontrol duzlemi Ed25519 anahtari ile imzalanmistir.")
	pdf.Ln(4)
	pdf.Cell(0, 6, "Imzayi dogrulamak icin: personel-audit-verify --report <id>")

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, fmt.Errorf("destruction: pdf output: %w", err)
	}
	return buf.Bytes(), nil
}
