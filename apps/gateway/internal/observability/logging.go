// Package observability sets up structured logging, Prometheus metrics, and
// OpenTelemetry tracing for the gateway and enricher binaries.
package observability

import (
	"context"
	"io"
	"log/slog"
	"os"
)

// InitLogger creates a JSON structured logger writing to w (typically os.Stderr).
// If w is nil, os.Stderr is used. The returned *slog.Logger is intended to be
// passed through the application rather than stored as a global.
func InitLogger(w io.Writer, level slog.Level) *slog.Logger {
	if w == nil {
		w = os.Stderr
	}
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level:     level,
		AddSource: true,
	})
	return slog.New(h)
}

// ParseLogLevel converts a string log level to slog.Level. Defaults to Info.
func ParseLogLevel(s string) slog.Level {
	switch s {
	case "debug", "DEBUG":
		return slog.LevelDebug
	case "warn", "WARN", "warning", "WARNING":
		return slog.LevelWarn
	case "error", "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// WithRequestAttrs returns a child logger with standard request attributes.
// These fields are included on every log line produced inside a stream handler.
func WithRequestAttrs(logger *slog.Logger, tenantID, endpointID, traceID string) *slog.Logger {
	return logger.With(
		slog.String("tenant_id", tenantID),
		slog.String("endpoint_id", endpointID),
		slog.String("trace_id", traceID),
	)
}

// contextKey is an unexported type for context keys in this package.
type contextKey int

const loggerKey contextKey = 0

// WithLogger stores the logger in the context.
func WithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

// FromContext retrieves the logger from context. Falls back to the default
// slog logger if none was stored.
func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}
