// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
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

	tracerProviderOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithBatcher(exporter, sdktrace.WithExportTimeout(tracerExportTimeout)),
		sdktrace.WithResource(resource.NewWithAttributes(semconv.SchemaURL,
			attribute.String("version", config.Version),
			semconv.ServiceNameKey.String(config.AppName),
		)),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(config.TraceSampleRate)),
	}

	tracerProvider := sdktrace.NewTracerProvider(tracerProviderOpts...)
	return &tracer{
		Tracer: tracerProvider.Tracer(config.AppName),
		tp:     tracerProvider,
	}, nil
}
