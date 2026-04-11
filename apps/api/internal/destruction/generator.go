// Package destruction — 6-month periodic destruction report generator.
//
// Auto-scheduled on 1 January and 1 July at 00:00 local for the preceding
// 6-month period. DPO-only download. Reports are signed with the
// control-plane Ed25519 key.
package destruction

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/oklog/ulid/v2"

	"github.com/personel/api/internal/audit"
	"github.com/personel/api/internal/minio"
	"github.com/personel/api/internal/vault"
)

// Report is the destruction report aggregate.
type Report struct {
	ID          string          `json:"id"`
	TenantID    string          `json:"tenant_id"`
	Period      string          `json:"period"` // e.g. "2026-H1"
	PeriodStart time.Time       `json:"period_start"`
	PeriodEnd   time.Time       `json:"period_end"`
	GeneratedAt time.Time       `json:"generated_at"`
	MinIOPath   string          `json:"minio_path"`
	Manifest    json.RawMessage `json:"manifest"`
	SigningKeyID string          `json:"signing_key_id"`
	Signature   []byte          `json:"signature"`
}

// Generator builds and stores destruction reports.
type Generator struct {
	pg          *pgxpool.Pool
	ch          clickhouse.Conn
	minioClient *minio.Client
	vaultClient *vault.Client
	recorder    *audit.Recorder
	pdfGen      *PDFGenerator
	log         *slog.Logger
}

// NewGenerator creates a Generator.
func NewGenerator(
	pg *pgxpool.Pool,
	ch clickhouse.Conn,
	mc *minio.Client,
	vc *vault.Client,
	rec *audit.Recorder,
	log *slog.Logger,
) *Generator {
	return &Generator{
		pg: pg, ch: ch, minioClient: mc,
		vaultClient: vc, recorder: rec,
		pdfGen: NewPDFGenerator(),
		log:    log,
	}
}

// Manifest is the structured contents of a destruction report.
type Manifest struct {
	PeriodStart         time.Time            `json:"period_start"`
	PeriodEnd           time.Time            `json:"period_end"`
	CategoryDeletions   []CategoryDeletion   `json:"category_deletions"`
	MinIODeletions      []MinIODeletion      `json:"minio_deletions"`
	KeyDestructions     []KeyDestruction     `json:"key_destructions"`
	LegalHoldEvents     []LegalHoldEvent     `json:"legal_hold_events"`
	DSRTriggeredDeletions []DSRDeletion      `json:"dsr_triggered_deletions"`
	OutstandingHolds    []OutstandingHold    `json:"outstanding_holds"`
}

type CategoryDeletion struct {
	Category  string `json:"category"`
	RowCount  int64  `json:"row_count"`
	Store     string `json:"store"`
}

type MinIODeletion struct {
	BucketPrefix string `json:"bucket_prefix"`
	Category     string `json:"category"`
	ObjectCount  int64  `json:"object_count"`
}

type KeyDestruction struct {
	KeyRef      string    `json:"key_ref"`
	KeyType     string    `json:"key_type"` // tmk_version, pe_dek
	DestroyedAt time.Time `json:"destroyed_at"`
	Reason      string    `json:"reason"`
}

type LegalHoldEvent struct {
	HoldID    string    `json:"hold_id"`
	EventType string    `json:"event_type"` // placed | released
	OccurredAt time.Time `json:"occurred_at"`
	ReasonCode string   `json:"reason_code"`
}

type DSRDeletion struct {
	DSRID      string    `json:"dsr_id"`
	DeletedAt  time.Time `json:"deleted_at"`
	Categories []string  `json:"categories"`
}

type OutstandingHold struct {
	HoldID     string    `json:"hold_id"`
	TicketID   string    `json:"ticket_id"`
	PlacedAt   time.Time `json:"placed_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// Generate builds the destruction report for the period ending periodEnd.
func (g *Generator) Generate(ctx context.Context, tenantID string, periodStart, periodEnd time.Time) (*Report, error) {
	period := formatPeriod(periodEnd)
	g.log.Info("destruction: generating report",
		slog.String("tenant_id", tenantID),
		slog.String("period", period),
	)

	manifest, err := g.buildManifest(ctx, tenantID, periodStart, periodEnd)
	if err != nil {
		return nil, fmt.Errorf("destruction: build manifest: %w", err)
	}

	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return nil, fmt.Errorf("destruction: marshal manifest: %w", err)
	}

	// Generate signed PDF.
	pdfBytes, err := g.pdfGen.Generate(manifest, tenantID, period)
	if err != nil {
		return nil, fmt.Errorf("destruction: generate pdf: %w", err)
	}

	// Sign the PDF bytes.
	sig, keyID, err := g.vaultClient.SignWithControlKey(ctx, pdfBytes)
	if err != nil {
		return nil, fmt.Errorf("destruction: sign pdf: %w", err)
	}

	// Upload PDF to MinIO.
	minioPath := fmt.Sprintf("destruction-reports/%s/%s.pdf", tenantID, period)
	if err := g.minioClient.PutObject(ctx, minioPath, pdfBytes, "application/pdf"); err != nil {
		return nil, fmt.Errorf("destruction: upload pdf: %w", err)
	}

	id := ulid.Make().String()
	now := time.Now().UTC()
	report := &Report{
		ID:          id,
		TenantID:    tenantID,
		Period:      period,
		PeriodStart: periodStart,
		PeriodEnd:   periodEnd,
		GeneratedAt: now,
		MinIOPath:   minioPath,
		Manifest:    manifestJSON,
		SigningKeyID: keyID,
		Signature:   sig,
	}

	// Persist.
	if err := g.store(ctx, report); err != nil {
		return nil, err
	}

	// Audit the generation.
	_, _ = g.recorder.AppendSystem(ctx, tenantID, audit.ActionRetentionDestructionReportGenerated,
		fmt.Sprintf("report:%s", id), map[string]any{
			"period":    period,
			"minio_key": minioPath,
			"key_id":    keyID,
		})

	return report, nil
}

func (g *Generator) buildManifest(ctx context.Context, tenantID string, from, to time.Time) (*Manifest, error) {
	m := &Manifest{PeriodStart: from, PeriodEnd: to}

	// Query ClickHouse for TTL deletions by category.
	rows, err := g.ch.Query(ctx,
		`SELECT data_category, count() AS row_count, 'clickhouse' AS store
		 FROM retention_deletion_log
		 WHERE tenant_id = ? AND deleted_at BETWEEN ? AND ?
		 GROUP BY data_category
		 ORDER BY data_category`,
		tenantID, from, to,
	)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var cd CategoryDeletion
			_ = rows.Scan(&cd.Category, &cd.RowCount, &cd.Store)
			m.CategoryDeletions = append(m.CategoryDeletions, cd)
		}
	}

	// Query Postgres for legal hold events in the period.
	lhRows, err := g.pg.Query(ctx,
		`SELECT id, event_type, placed_at, reason_code
		 FROM (
		   SELECT id, 'placed' AS event_type, placed_at, reason_code FROM legal_holds
		   WHERE tenant_id = $1::uuid AND placed_at BETWEEN $2 AND $3
		   UNION ALL
		   SELECT id, 'released' AS event_type, released_at, '' FROM legal_holds
		   WHERE tenant_id = $1::uuid AND released_at BETWEEN $2 AND $3
		 ) lh`,
		tenantID, from, to,
	)
	if err == nil {
		defer lhRows.Close()
		for lhRows.Next() {
			var e LegalHoldEvent
			_ = lhRows.Scan(&e.HoldID, &e.EventType, &e.OccurredAt, &e.ReasonCode)
			m.LegalHoldEvents = append(m.LegalHoldEvents, e)
		}
	}

	// Outstanding holds at period end.
	ohRows, err := g.pg.Query(ctx,
		`SELECT id, ticket_id, placed_at, expires_at
		 FROM legal_holds
		 WHERE tenant_id = $1::uuid AND is_active = true`,
		tenantID,
	)
	if err == nil {
		defer ohRows.Close()
		for ohRows.Next() {
			var oh OutstandingHold
			_ = ohRows.Scan(&oh.HoldID, &oh.TicketID, &oh.PlacedAt, &oh.ExpiresAt)
			m.OutstandingHolds = append(m.OutstandingHolds, oh)
		}
	}

	// DSR-triggered deletions.
	dsrRows, err := g.pg.Query(ctx,
		`SELECT id, closed_at
		 FROM dsr_requests
		 WHERE tenant_id = $1::uuid AND request_type = 'erase'
		   AND state = 'resolved' AND closed_at BETWEEN $2 AND $3`,
		tenantID, from, to,
	)
	if err == nil {
		defer dsrRows.Close()
		for dsrRows.Next() {
			var d DSRDeletion
			_ = dsrRows.Scan(&d.DSRID, &d.DeletedAt)
			m.DSRTriggeredDeletions = append(m.DSRTriggeredDeletions, d)
		}
	}

	return m, nil
}

func (g *Generator) store(ctx context.Context, r *Report) error {
	_, err := g.pg.Exec(ctx,
		`INSERT INTO destruction_reports
		 (id, tenant_id, period, period_start, period_end, generated_at, minio_path, manifest, signing_key_id, signature)
		 VALUES ($1, $2::uuid, $3, $4, $5, $6, $7, $8::jsonb, $9, $10)`,
		r.ID, r.TenantID, r.Period, r.PeriodStart, r.PeriodEnd,
		r.GeneratedAt, r.MinIOPath, r.Manifest, r.SigningKeyID, r.Signature,
	)
	if err != nil {
		return fmt.Errorf("destruction: store: %w", err)
	}
	return nil
}

// formatPeriod returns "2026-H1" or "2026-H2".
func formatPeriod(end time.Time) string {
	year := end.Year()
	if end.Month() <= 6 {
		return fmt.Sprintf("%d-H1", year)
	}
	return fmt.Sprintf("%d-H2", year)
}

// PeriodBounds returns the start and end of the 6-month period containing t.
func PeriodBounds(t time.Time) (time.Time, time.Time) {
	if t.Month() <= 6 {
		start := time.Date(t.Year()-1, 7, 1, 0, 0, 0, 0, time.UTC)
		end := time.Date(t.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
		return start, end
	}
	start := time.Date(t.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(t.Year(), 7, 1, 0, 0, 0, 0, time.UTC)
	return start, end
}
