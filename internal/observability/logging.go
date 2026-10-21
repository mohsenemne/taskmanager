package observability

import (
	"log/slog"
	"os"
)

// NewLogger returns the service logger: structured JSON on stdout, at the
// configured level.
func NewLogger(level slog.Level) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))
}
