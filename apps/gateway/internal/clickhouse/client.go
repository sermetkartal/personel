package clickhouse

import (
	"context"
	"fmt"
	"log/slog"

	chdriver "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/personel/gateway/internal/config"
)

// Client wraps a ClickHouse connection and provides schema bootstrapping.
type Client struct {
	conn   driver.Conn
	logger *slog.Logger
	cfg    config.ClickHouseConfig
}

// New creates a new ClickHouse client, pings the server, and bootstraps schemas.
func New(ctx context.Context, cfg config.ClickHouseConfig, logger *slog.Logger) (*Client, error) {
	opts := &chdriver.Options{
		Addr: cfg.Addrs,
		Auth: chdriver.Auth{
			Database: cfg.Database,
			Username: cfg.Username,
			Password: cfg.Password,
		},
		Debug: false,
		Settings: chdriver.Settings{
			"max_execution_time": 60,
		},
		Compression: &chdriver.Compression{
			Method: chdriver.CompressionLZ4,
		},
	}

	if cfg.AsyncInsert {
		opts.Settings["async_insert"] = 1
		opts.Settings["wait_for_async_insert"] = 0
	}

	conn, err := chdriver.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: open connection: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		return nil, fmt.Errorf("clickhouse: ping: %w", err)
	}

	c := &Client{conn: conn, logger: logger, cfg: cfg}
	if err := c.bootstrap(ctx); err != nil {
		conn.Close()
		return nil, err
	}
	return c, nil
}

// bootstrap runs all DDL statements idempotently.
func (c *Client) bootstrap(ctx context.Context) error {
	for _, ddl := range DDLStatements {
		if err := c.conn.Exec(ctx, ddl); err != nil {
			return fmt.Errorf("clickhouse: bootstrap DDL: %w", err)
		}
	}
	c.logger.InfoContext(ctx, "clickhouse: schemas bootstrapped")
	return nil
}

// Conn returns the raw ClickHouse connection for use by the batcher.
func (c *Client) Conn() driver.Conn { return c.conn }

// Close closes the ClickHouse connection.
func (c *Client) Close() error { return c.conn.Close() }
