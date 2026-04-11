// Package observability — structured logging initialisation.
package observability

import (
	"log/slog"
	"os"
)

// NewLogger creates a JSON-format structured logger.
func NewLogger(serviceName, version string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.MessageKey {
				a.Key = "message"
			}
			return a
		},
	})).With(
		slog.String("service", serviceName),
		slog.String("version", version),
	)
}
