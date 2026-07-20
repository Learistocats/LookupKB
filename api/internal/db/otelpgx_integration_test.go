//go:build integration

package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/Learistocats/LookupKB/api/internal/config"
	appdb "github.com/Learistocats/LookupKB/api/internal/db"
)

// TestPoolEmitsDBChildSpan proves the otelpgx wiring in NewPool: a query run
// under a recording request span produces a client DB child span in the SAME
// trace. This is what makes DB calls attributable per request (01-architecture
// §6). It also documents the boundary — otelpgx only spans a query when the
// context already carries a recording span (so the /readyz background-context
// Ping deliberately produces nothing).
func TestPoolEmitsDBChildSpan(t *testing.T) {
	ctx := context.Background()

	pgc := startTestPostgres(t, ctx)

	dsn, err := pgc.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	// Record spans synchronously into memory and install the provider as global
	// BEFORE NewPool, since otelpgx.NewTracer captures the global provider at
	// construction time.
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	t.Cleanup(func() { _ = tp.Shutdown(ctx) })

	cfg := config.Config{
		DatabaseURL:       dsn,
		DBMaxConns:        4,
		DBMaxConnLifetime: time.Hour,
		DBMaxConnIdleTime: 30 * time.Minute,
		DBConnectTimeout:  5 * time.Second,
	}
	pool, err := appdb.NewPool(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	// A recording parent span stands in for the per-request HTTP span.
	reqCtx, parent := tp.Tracer("test").Start(ctx, "parent-request")
	var n int
	require.NoError(t, pool.QueryRow(reqCtx, "SELECT 1").Scan(&n))
	require.Equal(t, 1, n)
	parent.End()

	// Find the client DB span and assert it is a child of the request span.
	var dbSpan sdktrace.ReadOnlySpan
	for _, s := range sr.Ended() {
		if s.SpanKind() == trace.SpanKindClient {
			dbSpan = s
			break
		}
	}
	require.NotNil(t, dbSpan, "otelpgx must emit a client DB span for the query")
	require.Equal(t, parent.SpanContext().TraceID(), dbSpan.SpanContext().TraceID(),
		"DB span must share the request's trace")
	require.Equal(t, parent.SpanContext().SpanID(), dbSpan.Parent().SpanID(),
		"DB span must be a child of the request span")
}
