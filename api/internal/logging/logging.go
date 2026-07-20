// Package logging bootstraps the single, structured slog logger used across the
// whole API image. Per docs/planning/02-tech-stack.md §6 there is exactly one
// logger and one format: no ad-hoc fmt.Println, no per-package loggers.
//
// The logger fans every line out to two sinks through one context-aware handler:
//   - a JSON (or text) handler on stdout — the grep-verifiable log stream that
//     the container runtime captures, and
//   - the otelslog bridge to the OpenTelemetry LoggerProvider — OTLP logs the
//     collector forwards to the backend (Loki), natively trace-correlated.
//
// A single contextHandler wraps both so every line — regardless of sink —
// carries the standard schema: time, level, msg, service, version, plus
// trace_id/span_id (from the active span) and request_id/actor_id (from the
// request context). actor_id is the internal user id, never PII (06-security.md
// §7).
package logging

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/trace"
)

// scopeName identifies this logger's instrumentation scope in emitted OTLP logs.
const scopeName = "github.com/Learistocats/LookupKB/api"

// Standard fields attached to every log line.
const (
	fieldService   = "service"
	fieldVersion   = "version"
	fieldTraceID   = "trace_id"
	fieldSpanID    = "span_id"
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
// lp is the OpenTelemetry LoggerProvider to bridge logs to; when nil the logger
// writes only to stdout (used by the migrate/healthcheck subcommands, which run
// before — or without — the telemetry SDK).
func New(format, level, version string, lp otellog.LoggerProvider) *slog.Logger {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}

	var stdout slog.Handler
	if strings.EqualFold(format, "text") {
		stdout = slog.NewTextHandler(os.Stdout, opts)
	} else {
		stdout = slog.NewJSONHandler(os.Stdout, opts)
	}

	handlers := []slog.Handler{stdout}
	if lp != nil {
		// The bridge natively stamps trace context onto the OTLP record; the
		// contextHandler adds the same fields as attributes so the stdout sink
		// carries them too. The redundancy on the OTLP side is harmless.
		handlers = append(handlers, otelslog.NewHandler(scopeName, otelslog.WithLoggerProvider(lp)))
	}

	handler := &contextHandler{inner: newMultiHandler(handlers...)}

	return slog.New(handler).With(
		slog.String(fieldService, "lookupkb-api"),
		slog.String(fieldVersion, version),
	)
}

// contextHandler enriches every record with fields derived from the context —
// trace_id/span_id from the active span and request_id/actor_id set by
// middleware — before delegating to the wrapped handler(s).
type contextHandler struct {
	inner slog.Handler
}

func (h *contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String(fieldTraceID, sc.TraceID().String()),
			slog.String(fieldSpanID, sc.SpanID().String()),
		)
	}
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

// multiHandler fans a record out to several handlers. Kept tiny and local to
// avoid a dependency; it dispatches Enabled/Handle/WithAttrs/WithGroup to each.
type multiHandler struct {
	handlers []slog.Handler
}

func newMultiHandler(handlers ...slog.Handler) *multiHandler {
	return &multiHandler{handlers: handlers}
}

func (h *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, sub := range h.handlers {
		if sub.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, sub := range h.handlers {
		if !sub.Enabled(ctx, r.Level) {
			continue
		}
		if err := sub.Handle(ctx, r.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (h *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make([]slog.Handler, len(h.handlers))
	for i, sub := range h.handlers {
		next[i] = sub.WithAttrs(attrs)
	}
	return &multiHandler{handlers: next}
}

func (h *multiHandler) WithGroup(name string) slog.Handler {
	next := make([]slog.Handler, len(h.handlers))
	for i, sub := range h.handlers {
		next[i] = sub.WithGroup(name)
	}
	return &multiHandler{handlers: next}
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
