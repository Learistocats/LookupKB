// Package telemetry bootstraps the OpenTelemetry Go SDK (traces + metrics +
// logs) at service startup and exports everything over OTLP/gRPC to the
// otel-collector (docs/planning/01-architecture.md §6). It installs the global
// TracerProvider, MeterProvider, LoggerProvider and text-map propagator so the
// rest of the code base instruments through the OTel globals (otelfiber,
// otelpgx, otelslog, internal/metrics) with no further wiring.
//
// When no OTLP endpoint is configured the SDK is not initialised: Init returns a
// nil LoggerProvider and a no-op shutdown, so the API boots without a collector
// (the same "boots without the dependency" pattern as an empty DatabaseURL). The
// OTel globals then stay at their built-in no-op defaults.
package telemetry

import (
	"context"
	"errors"
	"fmt"

	"github.com/Learistocats/LookupKB/api/internal/config"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	otellog "go.opentelemetry.io/otel/log"
	logglobal "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// serviceName is the resource attribute value tying every trace/metric/log back
// to this service across deploys.
const serviceName = "lookupkb-api"

// ShutdownFunc flushes and stops all providers. It is safe to call once, ideally
// on SIGTERM before the process exits, so telemetry from the last requests is
// not silently dropped.
type ShutdownFunc func(context.Context) error

// Init installs the OpenTelemetry SDK when cfg has an OTLP endpoint configured.
//
// It returns the LoggerProvider (so logging.New can bridge slog to it) and a
// shutdown function. When telemetry is disabled it returns (nil, no-op, nil) —
// the return type is the interface, not *sdklog.LoggerProvider, so a disabled
// run yields a genuinely nil provider (a typed-nil pointer would read as a
// non-nil interface and make the slog bridge panic).
// The bounded connection is lazy: the gRPC exporters do not block startup if the
// collector is not yet reachable — they buffer and retry.
func Init(ctx context.Context, cfg config.Config, version string) (otellog.LoggerProvider, ShutdownFunc, error) {
	if !cfg.OTelEnabled() {
		return nil, func(context.Context) error { return nil }, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion(version),
			// A per-process id keeps signals attributable when the service is
			// horizontally scaled (01-architecture.md §5.5).
			semconv.ServiceInstanceID(uuid.NewString()),
		),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("build otel resource: %w", err)
	}

	endpoint := cfg.OTelExporterEndpoint

	traceExp, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpointURL(endpoint))
	if err != nil {
		return nil, nil, fmt.Errorf("create otlp trace exporter: %w", err)
	}
	metricExp, err := otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithEndpointURL(endpoint))
	if err != nil {
		return nil, nil, fmt.Errorf("create otlp metric exporter: %w", err)
	}
	logExp, err := otlploggrpc.New(ctx, otlploggrpc.WithEndpointURL(endpoint))
	if err != nil {
		return nil, nil, fmt.Errorf("create otlp log exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
		sdkmetric.WithResource(res),
	)
	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
		sdklog.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	logglobal.SetLoggerProvider(lp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	shutdown := func(ctx context.Context) error {
		return errors.Join(
			tp.Shutdown(ctx),
			mp.Shutdown(ctx),
			lp.Shutdown(ctx),
		)
	}
	return lp, shutdown, nil
}
