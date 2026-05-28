// Package app contains the small amount of plumbing that wires every layer
// together: logger setup, DB bring-up, migrations, and graceful shutdown.
package app

import (
	"io"
	"log/slog"
	"strings"

	"orgstructure/internal/config"
)

// NewLogger returns an slog.Logger configured per cfg.
// "json" format is best for production (parseable by log shippers),
// "text" is friendlier when running locally.
func NewLogger(cfg config.LogConfig, w io.Writer) *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(cfg.Level) {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if strings.ToLower(cfg.Format) == "text" {
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
	}
	return slog.New(handler)
}
