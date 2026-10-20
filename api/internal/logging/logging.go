// Package logging bootstraps the single, structured slog logger used across the
// whole API image. There is exactly one logger and one format: no ad-hoc
// fmt.Println, no per-package loggers.
//
// A contextHandler wraps the stdout JSON (or text) handler so every line carries
// the standard schema: time, level, msg, service, version, plus request_id and
// actor_id (from the request context). actor_id is the internal user uuid, never
// the email (redacted PII).
package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"
)

// Standard fields attached to every log line.
const (
	fieldService   = "service"
	fieldVersion   = "version"
	fieldRequestID = "request_id"
	fieldActorID   = "actor_id"
)

// ctxKey is the private type for context values this package reads back onto
// every log line. Values are set once by middleware and picked up by the
// contextHandler without re-plumbing (the "set once, available everywhere"
// approach from the ticket).
type ctxKey int

const (
	requestIDKey ctxKey = iota
	actorIDKey
)

// WithRequestID returns a context carrying the per-request id so every log line
// emitted under it gains a request_id field. Set once by the request-id
// middleware.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// WithActorID returns a context carrying the verified caller's internal user id
// so every downstream log line gains an actor_id field. Set once by the auth
// middleware; the value is the internal uuid, never the email (redacted PII).
func WithActorID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, actorIDKey, id)
}

func requestIDFromCtx(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

func actorIDFromCtx(ctx context.Context) string {
	id, _ := ctx.Value(actorIDKey).(string)
	return id
}

// New builds the process-wide slog.Logger.
//
// format is "json" (default, production) or "text". level is one of
// debug|info|warn|error. version is the build version stamped into every line.
func New(format, level, version string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	var stdout slog.Handler
	if strings.EqualFold(format, "text") {
		stdout = slog.NewTextHandler(os.Stdout, opts)
	} else {
		stdout = slog.NewJSONHandler(os.Stdout, opts)
	}

	return slog.New(&contextHandler{inner: stdout}).With(
		slog.String(fieldService, "lookupkb-api"),
		slog.String(fieldVersion, version),
	)
}

// contextHandler enriches every record with the request_id/actor_id set by
// middleware before delegating to the wrapped handler.
type contextHandler struct {
	inner slog.Handler
}

func (h *contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := requestIDFromCtx(ctx); id != "" {
		r.AddAttrs(slog.String(fieldRequestID, id))
	}
	if id := actorIDFromCtx(ctx); id != "" {
		r.AddAttrs(slog.String(fieldActorID, id))
	}
	return h.inner.Handle(ctx, r)
}

func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{inner: h.inner.WithGroup(name)}
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
