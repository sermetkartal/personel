// Command cold-export is the Faz 7 / item #76 cold tier export tool.
//
// It is a scaffold — the real Parquet writing + MinIO upload are marked
// TODO Phase 3.1 and left unimplemented. The binary builds cleanly and
// can be invoked to validate env parsing, partition enumeration, and
// audit wiring end-to-end against a real ClickHouse instance. In dry-run
// mode (the default) nothing is dropped, so the operator can rehearse
// the job before flipping PERSONEL_COLD_EXPORT_DRY_RUN=false.
//
// Flow:
//  1. Connect to ClickHouse.
//  2. For each configured table, enumerate partitions older than
//     PERSONEL_COLD_EXPORT_DAYS (default 91) using system.parts.
//  3. For each (tenant, partition): export to Parquet → MinIO, then
//     `ALTER TABLE ... DROP PARTITION ...`.
//  4. Emit one audit log entry per tenant/partition pair on completion.
//
// Env:
//   PERSONEL_COLD_EXPORT_DAYS       (default 91)
//   PERSONEL_COLD_EXPORT_DRY_RUN    (default true — MUST be explicit false to drop)
//   PERSONEL_COLD_EXPORT_TABLES     (csv, default "events_raw")
//   PERSONEL_CLICKHOUSE_DSN         clickhouse://user:pass@host:9000/db
//   PERSONEL_COLD_EXPORT_MINIO_BUCKET  default "backups"
//   PERSONEL_COLD_EXPORT_MINIO_PREFIX  default "cold"
//
// Audit records are written to the `audit.append_event` Postgres procedure
// via the admin API (future work). For the scaffold, audit entries are
// just logged to stderr.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "github.com/ClickHouse/clickhouse-go/v2" // driver registration

	"github.com/personel/gateway/internal/observability"
)

const (
	defaultDays       = 91
	defaultBucket     = "backups"
	defaultPrefix     = "cold"
	defaultTablesCSV  = "events_raw"
	envDays           = "PERSONEL_COLD_EXPORT_DAYS"
	envDryRun         = "PERSONEL_COLD_EXPORT_DRY_RUN"
	envTables         = "PERSONEL_COLD_EXPORT_TABLES"
	envDSN            = "PERSONEL_CLICKHOUSE_DSN"
	envMinIOBucket    = "PERSONEL_COLD_EXPORT_MINIO_BUCKET"
	envMinIOPrefix    = "PERSONEL_COLD_EXPORT_MINIO_PREFIX"
	operationTimeout  = 30 * time.Minute
	auditEntryAction  = "cold_export.partition_drop"
)

// Config holds the runtime configuration parsed from env vars.
type Config struct {
	Days         int
	DryRun       bool
	Tables       []string
	DSN          string
	MinIOBucket  string
	MinIOPrefix  string
}

// PartitionRow is the subset of system.parts we read during enumeration.
type PartitionRow struct {
	Table     string
	Partition string
	MinDate   time.Time
	MaxDate   time.Time
	Rows      uint64
	Bytes     uint64
}

func main() {
	flag.Parse()

	logger := observability.InitLogger(os.Stderr, slog.LevelInfo)
	slog.SetDefault(logger)

	cfg, err := loadConfig()
	if err != nil {
		logger.Error("cold-export: config error", slog.String("error", err.Error()))
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	defer cancel()
	ctx = installSignalHandler(ctx, logger)

	logger.Info("cold-export: starting",
		slog.Int("days", cfg.Days),
		slog.Bool("dry_run", cfg.DryRun),
		slog.Any("tables", cfg.Tables),
	)

	db, err := sql.Open("clickhouse", cfg.DSN)
	if err != nil {
		logger.Error("cold-export: open clickhouse failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		logger.Error("cold-export: ping clickhouse failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	dropped := 0
	skipped := 0
	for _, table := range cfg.Tables {
		parts, err := enumerateExpiringPartitions(ctx, db, table, cfg.Days)
		if err != nil {
			logger.Error("cold-export: enumerate failed",
				slog.String("table", table),
				slog.String("error", err.Error()),
			)
			continue
		}
		logger.Info("cold-export: partitions to process",
			slog.String("table", table),
			slog.Int("count", len(parts)),
		)
		for _, p := range parts {
			if err := exportPartitionToParquet(ctx, cfg, p); err != nil {
				logger.Error("cold-export: export failed",
					slog.String("table", p.Table),
					slog.String("partition", p.Partition),
					slog.String("error", err.Error()),
				)
				skipped++
				continue
			}
			if cfg.DryRun {
				logger.Info("cold-export: DRY RUN — would drop partition",
					slog.String("table", p.Table),
					slog.String("partition", p.Partition),
					slog.Uint64("rows", p.Rows),
					slog.Uint64("bytes", p.Bytes),
				)
				skipped++
				continue
			}
			if err := dropPartition(ctx, db, p); err != nil {
				logger.Error("cold-export: drop partition failed",
					slog.String("table", p.Table),
					slog.String("partition", p.Partition),
					slog.String("error", err.Error()),
				)
				skipped++
				continue
			}
			writeAuditEntry(logger, cfg, p)
			dropped++
		}
	}

	logger.Info("cold-export: done",
		slog.Int("dropped", dropped),
		slog.Int("skipped", skipped),
	)
}

func loadConfig() (*Config, error) {
	c := &Config{
		Days:        defaultDays,
		DryRun:      true,
		Tables:      strings.Split(defaultTablesCSV, ","),
		MinIOBucket: defaultBucket,
		MinIOPrefix: defaultPrefix,
	}

	if v := os.Getenv(envDays); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return nil, fmt.Errorf("invalid %s=%q", envDays, v)
		}
		c.Days = n
	}

	if v := os.Getenv(envDryRun); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid %s=%q", envDryRun, v)
		}
		c.DryRun = parsed
	}

	if v := os.Getenv(envTables); v != "" {
		c.Tables = nil
		for _, t := range strings.Split(v, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				c.Tables = append(c.Tables, t)
			}
		}
	}

	c.DSN = os.Getenv(envDSN)
	if c.DSN == "" {
		return nil, errors.New("PERSONEL_CLICKHOUSE_DSN is required")
	}

	if v := os.Getenv(envMinIOBucket); v != "" {
		c.MinIOBucket = v
	}
	if v := os.Getenv(envMinIOPrefix); v != "" {
		c.MinIOPrefix = v
	}

	return c, nil
}

// enumerateExpiringPartitions queries system.parts for all active parts of
// the given table whose max_date is older than `days` ago. This groups
// by partition to produce one row per expiring partition.
func enumerateExpiringPartitions(ctx context.Context, db *sql.DB, table string, days int) ([]PartitionRow, error) {
	const q = `
SELECT partition,
       min(min_date) AS min_date,
       max(max_date) AS max_date,
       sum(rows)     AS rows,
       sum(bytes_on_disk) AS bytes
FROM system.parts
WHERE database = currentDatabase()
  AND table = ?
  AND active = 1
GROUP BY partition
HAVING max_date < today() - ?
ORDER BY partition
`
	rows, err := db.QueryContext(ctx, q, table, days)
	if err != nil {
		return nil, fmt.Errorf("query system.parts: %w", err)
	}
	defer rows.Close()

	var out []PartitionRow
	for rows.Next() {
		var p PartitionRow
		p.Table = table
		if err := rows.Scan(&p.Partition, &p.MinDate, &p.MaxDate, &p.Rows, &p.Bytes); err != nil {
			return nil, fmt.Errorf("scan partition row: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// exportPartitionToParquet is the TODO Phase 3.1 hook where we would:
//  1. Run `SELECT * FROM {table} WHERE _partition_id = {partition}
//     INTO OUTFILE 's3://...' FORMAT Parquet` via the ClickHouse-native
//     s3() table function (needs S3 credentials in merge_tree config) OR
//  2. Read parts via CH client, stream into github.com/xitongsys/parquet-go,
//     PUT to MinIO with sha256 + size check.
//
// For the scaffold we just log the intent.
func exportPartitionToParquet(_ context.Context, cfg *Config, p PartitionRow) error {
	// TODO Phase 3.1: real Parquet export to MinIO
	slog.Info("cold-export: TODO Parquet export",
		slog.String("table", p.Table),
		slog.String("partition", p.Partition),
		slog.String("target", fmt.Sprintf("s3://%s/%s/%s/%s.parquet",
			cfg.MinIOBucket, cfg.MinIOPrefix, p.Table, p.Partition)),
		slog.Uint64("rows", p.Rows),
		slog.Uint64("bytes", p.Bytes),
	)
	return nil
}

func dropPartition(ctx context.Context, db *sql.DB, p PartitionRow) error {
	// Partition literal must be quoted for string partition values.
	// For YYYYMM partitions (Personel's scheme) it is numeric but we
	// still wrap it defensively — ClickHouse accepts both.
	q := fmt.Sprintf("ALTER TABLE %s DROP PARTITION '%s'", p.Table, p.Partition)
	_, err := db.ExecContext(ctx, q)
	if err != nil {
		return fmt.Errorf("drop partition: %w", err)
	}
	return nil
}

func writeAuditEntry(logger *slog.Logger, cfg *Config, p PartitionRow) {
	// TODO Phase 3.1: call admin API POST /v1/system/cold-export-runs
	// so the drop is recorded in the hash-chained audit log and picked
	// up by the SOC 2 evidence locker as an A1.2 / P7.1 artefact.
	logger.Info("cold-export: AUDIT",
		slog.String("action", auditEntryAction),
		slog.String("table", p.Table),
		slog.String("partition", p.Partition),
		slog.Uint64("rows", p.Rows),
		slog.Uint64("bytes", p.Bytes),
		slog.String("minio_bucket", cfg.MinIOBucket),
		slog.String("minio_prefix", cfg.MinIOPrefix),
		slog.Int("retention_days", cfg.Days),
	)
}

func installSignalHandler(parent context.Context, logger *slog.Logger) context.Context {
	ctx, cancel := context.WithCancel(parent)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		select {
		case <-sigCh:
			logger.Warn("cold-export: signal received, aborting")
			cancel()
		case <-parent.Done():
		}
	}()
	return ctx
}
