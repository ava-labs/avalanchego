// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import (
	"context"
	"io"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/trace"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

const (
	tracerExportTimeout = 10 * time.Second
	// [tracerProviderShutdownTimeout] is longer than [tracerExportTimeout] so
	// in-flight exports can finish before the tracer provider shuts down.
	tracerProviderShutdownTimeout = 15 * time.Second
)

type Config struct {
	ExporterConfig `json:"exporterConfig"`

	// The fraction of traces to sample.
	// If >= 1 always samples.
	// If <= 0 never samples.
	TraceSampleRate float64 `json:"traceSampleRate"`

	AppName string `json:"appName"`
	Version string `json:"version"`
}

type Tracer interface {
	trace.Tracer
	io.Closer
}

type tracer struct {
	trace.Tracer

	tp *sdktrace.TracerProvider
}

func (t *tracer) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), tracerProviderShutdownTimeout)
	defer cancel()
	return t.tp.Shutdown(ctx)
}

func New(config Config) (Tracer, error) {
	if config.ExporterConfig.Type == Disabled {
		return Noop, nil
	}

	exporter, err := newExporter(config.ExporterConfig)
	if err != nil {
		return nil, err
	}

	tracerProvider := newTracerProvider(exporter, config)
	return &tracer{
		Tracer: tracerProvider.Tracer(config.AppName),
		tp:     tracerProvider,
	}, nil
}

func newTracerProvider(exporter sdktrace.SpanExporter, config Config) *sdktrace.TracerProvider {
	return sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithExportTimeout(tracerExportTimeout)),
		sdktrace.WithResource(resource.NewWithAttributes(semconv.SchemaURL,
			attribute.String("version", config.Version),
			semconv.ServiceNameKey.String(config.AppName),
		)),
		// Deliberately NOT ParentBased: remote span contexts arrive from any peer
		// and must not be able to force sampling. Ratio-by-trace-ID also makes
		// every node with the same rate reach the same decision, so cross-node
		// traces are complete. See docs/superpowers/specs/2026-10-21-end-to-end-tracing-design.md.
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(config.TraceSampleRate)),
	)
}
