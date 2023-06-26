// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
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

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/version"
)

const (
	tracerExportTimeout = 10 * time.Second
	// [tracerProviderShutdownTimeout] is longer than [tracerExportTimeout] so
	// in-flight exports can finish before the tracer provider shuts down.
	tracerProviderShutdownTimeout = 15 * time.Second
)

type Config struct {
	ExporterConfig `json:"exporterConfig"`

	// Used to flag if tracing should be performed
	Enabled bool `json:"enabled"`

	// The fraction of traces to sample.
	// If >= 1 always samples.
	// If <= 0 never samples.
	TraceSampleRate float64 `json:"traceSampleRate"`
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
	if !config.Enabled {
		return &noOpTracer{
			t: trace.NewNoopTracerProvider().Tracer(constants.AppName),
		}, nil
	}

	exporter, err := newExporter(config.ExporterConfig)
	if err != nil {
		return nil, err
	}

	tracerProviderOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithBatcher(exporter, sdktrace.WithExportTimeout(tracerExportTimeout)),
		sdktrace.WithResource(resource.NewWithAttributes(semconv.SchemaURL,
			attribute.Stringer("version", version.Current),
			semconv.ServiceNameKey.String(constants.AppName),
		)),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(config.TraceSampleRate)),
	}

	tracerProvider := sdktrace.NewTracerProvider(tracerProviderOpts...)
	return &tracer{
		Tracer: tracerProvider.Tracer(constants.AppName),
		tp:     tracerProvider,
	}, nil
}
