// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/version"
)

const (
	tracerProviderExportCreationTimeout = 5 * time.Second
	tracerExportTimeout                 = 10 * time.Second
	// [tracerProviderShutdownTimeout] is longer than [tracerExportTimeout] so
	// in-flight exports can finish before the tracer provider shuts down.
	tracerProviderShutdownTimeout = 15 * time.Second
)

var (
	_ otel.ErrorHandler = &otelErrHandler{}

	errUnknownExporterType = errors.New("unknown exporter type")

	// [tracerProvider] shares the same lifetime as a [node.Node].
	// [InitTracer] is called when the node executes Dispatch()
	// and [ShutdownTracer] is called when the node executes Shutdown().
	// The default value is a no-op tracer provider so it's safe
	// to use even if [InitTracer] is never called.
	tracerProvider trace.TracerProvider = trace.NewNoopTracerProvider()
)

func newResource() *resource.Resource {
	return resource.NewWithAttributes(semconv.SchemaURL,
		attribute.String("version", version.Current.String()),
		semconv.ServiceNameKey.String(constants.AppName),
	)
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

func newExporter(config ExporterConfig) (sdktrace.SpanExporter, error) {
	var client otlptrace.Client
	switch config.Type {
	case GRPC:
		client = otlptracegrpc.NewClient(
			otlptracegrpc.WithEndpoint(config.Endpoint),
			otlptracegrpc.WithHeaders(config.Headers),
			otlptracegrpc.WithTimeout(tracerExportTimeout),
		)
	case HTTP:
		client = otlptracehttp.NewClient(
			otlptracehttp.WithEndpoint(config.Endpoint),
			otlptracehttp.WithHeaders(config.Headers),
			otlptracehttp.WithTimeout(tracerExportTimeout),
		)
	default:
		return nil, errUnknownExporterType
	}

	ctx, cancel := context.WithTimeout(context.Background(), tracerProviderExportCreationTimeout)
	defer cancel()
	return otlptrace.New(ctx, client)

}

type TraceConfig struct {
	ExporterConfig `json:"exporterConfig"`

	// If false, use a no-op tracer. All other fields are ignored.
	Enabled bool `json:"enabled"`

	// The fraction of traces to sample.
	// If >= 1 always samples.
	// If <= 0 never samples.
	TraceSampleRate float64 `json:"traceSampleRate"`
}

// Logs an error with [log] when opentelemetry encounters an error
// (e.g. when the exporter fails to send a span).
type otelErrHandler struct {
	log logging.Logger
}

func (h otelErrHandler) Handle(err error) {
	h.log.Info("opentelemetry error", zap.Error(err))
}

// Initialize the tracer.
// If this is never called, we use a no-op tracer.
func InitTracer(log logging.Logger, config TraceConfig) error {
	if !config.Enabled {
		// [tracerProvider] is a no-op tracer provider by default.
		return nil
	}

	// Handle opentelemetry errors by logging them
	otel.SetErrorHandler(otelErrHandler{log: log})

	exporter, err := newExporter(config.ExporterConfig)
	if err != nil {
		return err
	}

	tracerProviderOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithBatcher(exporter, sdktrace.WithExportTimeout(tracerExportTimeout)),
		sdktrace.WithResource(newResource()),
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(config.TraceSampleRate)),
	}

	tracerProvider = sdktrace.NewTracerProvider(tracerProviderOpts...)
	return nil
}

// This should be called before AvalancheGo exits.
// If [tracerProvider] is a no-op tracer provider, this is a no-op.
func ShutdownTracer() error {
	tp, ok := tracerProvider.(*sdktrace.TracerProvider)
	if !ok {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), tracerProviderShutdownTimeout)
	defer cancel()
	return tp.Shutdown(ctx)
}

func Tracer() trace.Tracer {
	return tracerProvider.Tracer(constants.AppName)
}
