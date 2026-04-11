// Package clickhouse — read-only ClickHouse client.
package clickhouse

import (
	"context"
	"crypto/tls"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// Config holds ClickHouse connection parameters.
type Config struct {
	Addr      string
	Database  string
	Username  string
	Password  string
	TLSEnable bool
}

// New creates a read-only ClickHouse connection.
func New(cfg Config) (clickhouse.Conn, error) {
	options := &clickhouse.Options{
		Addr: []string{cfg.Addr},
		Auth: clickhouse.Auth{
			Database: cfg.Database,
			Username: cfg.Username,
			Password: cfg.Password,
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 30,
			"readonly":           1, // enforce read-only
		},
	}
	if cfg.TLSEnable {
		options.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	conn, err := clickhouse.Open(options)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: open: %w", err)
	}

	if err := conn.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("clickhouse: ping: %w", err)
	}

	return conn, nil
}
