// Package metrics declares the named, AC-tied OpenTelemetry instruments the
// roadmap calls for (docs/planning/02-tech-stack.md §6). They are *scaffolded*
// here — registered so they exist and export through the global MeterProvider —
// but have no producers yet: the features that observe them (search, read,
// publish, indexing, render) land in later tickets (M1/M2), which increment or
// record on the struct returned by New.
//
// RED metrics per route are emitted automatically by otelfiber; this package
// covers only the bespoke business instruments. Keep label cardinality bounded:
// record with route templates (/api/documents/:slug), never raw paths with ids.
package metrics

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// scopeName identifies these instruments' meter in the exported metrics.
const scopeName = "github.com/Learistocats/LookupKB/api"

// Metrics holds the process-wide business instruments. Later tickets take this
// struct (wired through the server) and Record/Add on the relevant field when
// the producing code path exists.
type Metrics struct {
	// SearchLatency records /api/search end-to-end latency in seconds (M1).
	SearchLatency metric.Float64Histogram
	// ReadLatency records published-document read latency in seconds (M1).
	ReadLatency metric.Float64Histogram
	// RenderDuration records server-side publish render latency in seconds (M2).
	RenderDuration metric.Float64Histogram
	// PublishTotal counts successful publishes (M2).
	PublishTotal metric.Int64Counter
	// IndexLagSeconds tracks Meilisearch index freshness lag in seconds (M1).
	IndexLagSeconds metric.Float64Gauge
}

// New registers the instruments on the given MeterProvider. Pass the global
// provider (otel.GetMeterProvider) so they export over OTLP; a no-op provider
// yields no-op instruments (telemetry disabled) without erroring.
func New(mp metric.MeterProvider) (*Metrics, error) {
	m := mp.Meter(scopeName)

	searchLatency, err := m.Float64Histogram(
		"search_latency",
		metric.WithDescription("End-to-end search request latency."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("register search_latency: %w", err)
	}

	readLatency, err := m.Float64Histogram(
		"read_latency",
		metric.WithDescription("Published-document read latency."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("register read_latency: %w", err)
	}

	renderDuration, err := m.Float64Histogram(
		"render_duration",
		metric.WithDescription("Server-side publish render duration."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("register render_duration: %w", err)
	}

	publishTotal, err := m.Int64Counter(
		"publish_total",
		metric.WithDescription("Count of successful document publishes."),
	)
	if err != nil {
		return nil, fmt.Errorf("register publish_total: %w", err)
	}

	indexLag, err := m.Float64Gauge(
		"index_lag_seconds",
		metric.WithDescription("Meilisearch index freshness lag."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("register index_lag_seconds: %w", err)
	}

	return &Metrics{
		SearchLatency:   searchLatency,
		ReadLatency:     readLatency,
		RenderDuration:  renderDuration,
		PublishTotal:    publishTotal,
		IndexLagSeconds: indexLag,
	}, nil
}
