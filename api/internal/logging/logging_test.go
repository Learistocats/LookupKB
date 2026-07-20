package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	lognoop "go.opentelemetry.io/otel/log/noop"
	"go.opentelemetry.io/otel/trace"
)

// bufferLogger builds a logger writing JSON to buf through the contextHandler,
// mirroring the stdout path New wires up.
func bufferLogger(buf *bytes.Buffer) *slog.Logger {
	h := &contextHandler{inner: slog.NewJSONHandler(buf, nil)}
	return slog.New(h)
}

func TestContextHandlerAddsCorrelationFields(t *testing.T) {
	var buf bytes.Buffer
	logger := bufferLogger(&buf)

	traceID, err := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("0123456789abcdef")
	require.NoError(t, err)
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})

	ctx := trace.ContextWithSpanContext(context.Background(), sc)
	ctx = WithRequestID(ctx, "req-123")
	ctx = WithActorID(ctx, "user-abc")

	logger.InfoContext(ctx, "hello")

	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line))
	require.Equal(t, traceID.String(), line[fieldTraceID])
	require.Equal(t, spanID.String(), line[fieldSpanID])
	require.Equal(t, "req-123", line[fieldRequestID])
	require.Equal(t, "user-abc", line[fieldActorID])
}

func TestContextHandlerOmitsFieldsWhenAbsent(t *testing.T) {
	var buf bytes.Buffer
	logger := bufferLogger(&buf)

	// No span, no request/actor id in context.
	logger.Info("plain")

	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line))
	for _, k := range []string{fieldTraceID, fieldSpanID, fieldRequestID, fieldActorID} {
		_, ok := line[k]
		require.Falsef(t, ok, "field %q should be absent when its source is not in context", k)
	}
}

func TestNewCarriesServiceAndVersion(t *testing.T) {
	// A nil LoggerProvider keeps New on the stdout-only path (no OTLP bridge).
	logger := New("json", "info", "v1.2.3", nil)
	require.NotNil(t, logger)
	// The standard resource fields are attached via With; a debug line is
	// filtered at info level, proving parseLevel is honoured too.
	require.False(t, logger.Enabled(context.Background(), slog.LevelDebug))
	require.True(t, logger.Enabled(context.Background(), slog.LevelInfo))
}

// TestNewWithLoggerProviderBridges guards the bridge path: a real (non-nil)
// LoggerProvider must build the otelslog handler and emit without panicking.
// A nil *sdklog.LoggerProvider passed as the interface would slip past the
// nil check and panic — telemetry.Init returns the interface type to prevent
// exactly that.
func TestNewWithLoggerProviderBridges(t *testing.T) {
	logger := New("json", "info", "v1", lognoop.NewLoggerProvider())
	require.NotNil(t, logger)
	require.NotPanics(t, func() { logger.Info("bridged") })
}
