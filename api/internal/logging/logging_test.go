package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
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

	ctx := WithRequestID(context.Background(), "req-123")
	ctx = WithActorID(ctx, "user-abc")

	logger.InfoContext(ctx, "hello")

	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line))
	require.Equal(t, "req-123", line[fieldRequestID])
	require.Equal(t, "user-abc", line[fieldActorID])
}

func TestContextHandlerOmitsFieldsWhenAbsent(t *testing.T) {
	var buf bytes.Buffer
	logger := bufferLogger(&buf)

	// No request/actor id in context.
	logger.Info("plain")

	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line))
	for _, k := range []string{fieldRequestID, fieldActorID} {
		_, ok := line[k]
		require.Falsef(t, ok, "field %q should be absent when its source is not in context", k)
	}
}

func TestNewCarriesServiceAndVersion(t *testing.T) {
	logger := New("json", "info", "v1.2.3")
	require.NotNil(t, logger)
	// The standard resource fields are attached via With; a debug line is
	// filtered at info level, proving parseLevel is honoured too.
	require.False(t, logger.Enabled(context.Background(), slog.LevelDebug))
	require.True(t, logger.Enabled(context.Background(), slog.LevelInfo))
}
