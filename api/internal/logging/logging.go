// Package logging bootstraps the single, structured slog logger used across
// the whole API image. Per docs/planning/02-tech-stack.md §6, there is exactly
// one logger and one format: no ad-hoc fmt.Println, no per-package loggers.
// A later ticket bridges this handler to OpenTelemetry (otelslog) so every
// line is trace-correlated; the schema established here stays stable.
package logging

import (
	"log/slog"
	"os"
	"strings"
)

// Standard fields attached to every log line. Trace correlation fields
// (trace_id/span_id) are added by the OTel bridge in a later ticket.
const (
	fieldService = "service"
	fieldVersion = "version"
)

// New builds the process-wide slog.Logger.
//
// format is "json" (default, production) or "text". level is one of
// debug|info|warn|error. version is the build version stamped into every line.
func New(format, level, version string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	var handler slog.Handler
	if strings.EqualFold(format, "text") {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	return slog.New(handler).With(
		slog.String(fieldService, "lookupkb-api"),
		slog.String(fieldVersion, version),
	)
}

// parseLevel maps a level string to slog.Level, defaulting to info.
func parseLevel(level string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
