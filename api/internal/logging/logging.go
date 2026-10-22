// Package logging builds the single structured slog logger for the API. A
// contextHandler adds request_id and actor_id from the request context to every
// line; actor_id is the internal user uuid, never the email.
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

// ctxKey is the private type for context values read back onto every log line.
type ctxKey int

const (
	requestIDKey ctxKey = iota
	actorIDKey
)

// WithRequestID returns a context whose log lines gain a request_id field.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// WithActorID returns a context whose log lines gain an actor_id field (the
// internal user id, never the email).
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
