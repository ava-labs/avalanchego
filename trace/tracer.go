// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import (
	"context"
	"errors"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/version"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	oteltrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
)

var errUnknownExporterType = errors.New("unknown exporter type")

var tracer trace.Tracer = trace.NewNoopTracerProvider().Tracer(constants.AppName)

func newResource() *resource.Resource {
	return resource.NewWithAttributes(semconv.SchemaURL,
		attribute.String("version", version.Current.String()),
		semconv.ServiceNameKey.String(constants.AppName),
	)
}

func newExporter(config ExporterConfig) (oteltrace.SpanExporter, error) {
	switch config.Type {
	case GRPC:
		client := otlptracegrpc.NewClient(
			otlptracegrpc.WithEndpoint(config.Endpoint),
			otlptracegrpc.WithHeaders(config.Headers),
		)
		return otlptrace.New(context.Background(), client)
	case HTTP:
		client := otlptracehttp.NewClient(
			otlptracehttp.WithEndpoint(config.Endpoint),
			otlptracehttp.WithHeaders(config.Headers),
		)
		return otlptrace.New(context.Background(), client)
	default:
		return nil, errUnknownExporterType
	}
}

type ExporterType byte

const (
	GRPC ExporterType = iota + 1
	HTTP
)

func (t ExporterType) String() string {
	switch t {
	case GRPC:
		return "grpc"
	case HTTP:
		return "http"
	default:
		return "unknown"
	}
}

type ExporterConfig struct {
	Type ExporterType `json:"type"`

	// Endpoint to send metrics to
	Endpoint string `json:"endpoint"`

	// Headers to send with metrics
	Headers map[string]string `json:"headers"`
}

type TraceConfig struct {
	ExporterConfig

	// If false, use a no-op tracer.
	// In this case, all other fields are ignored.
	Enabled bool `json:"enabled"`

	// The fraction of traces to sample.
	// If >= 1, always samples.
	// If <= 0, never samples.
	TraceSampleRate float64 `json:"alwaysSample"`
}

// Initialize the tracer.
// If this is never called, we use a no-op tracer.
func InitTracer(config TraceConfig) error {
	if !config.Enabled {
		// Use a no-op tracer, which is the default values of [tracer]
		return nil
	}

	exporter, err := newExporter(config.ExporterConfig)
	if err != nil {
		return err
	}

	tracerProviderOpts := []oteltrace.TracerProviderOption{
		oteltrace.WithBatcher(exporter),
		oteltrace.WithResource(newResource()),
		oteltrace.WithSampler(oteltrace.TraceIDRatioBased(config.TraceSampleRate)),
	}

	tracerProvider := oteltrace.NewTracerProvider(tracerProviderOpts...)
	tracer = tracerProvider.Tracer(constants.AppName)
	return nil
}

func Tracer() trace.Tracer {
	return tracer
}
