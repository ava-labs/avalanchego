// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

const tracerProviderExportCreationTimeout = 5 * time.Second

type ExporterConfig struct {
	Type ExporterType `json:"type"`

	// Endpoint to send metrics to. If empty, the default endpoint will be used.
	Endpoint string `json:"endpoint"`

	// Headers to send with metrics
	Headers map[string]string `json:"headers"`

	// If true, don't use TLS
	Insecure bool `json:"insecure"`
}

func newExporter(config ExporterConfig) (sdktrace.SpanExporter, error) {
	var client otlptrace.Client
	switch config.Type {
	case GRPC:
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithHeaders(config.Headers),
			otlptracegrpc.WithTimeout(tracerExportTimeout),
		}
		if config.Endpoint != "" {
			opts = append(opts, otlptracegrpc.WithEndpoint(config.Endpoint))
		}
		if config.Insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		client = otlptracegrpc.NewClient(opts...)
	case HTTP:
		opts := []otlptracehttp.Option{
			otlptracehttp.WithHeaders(config.Headers),
			otlptracehttp.WithTimeout(tracerExportTimeout),
		}
		if config.Endpoint != "" {
			opts = append(opts, otlptracehttp.WithEndpoint(config.Endpoint))
		}
		if config.Insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		client = otlptracehttp.NewClient(opts...)
	default:
		return nil, errUnknownExporterType
	}

	ctx, cancel := context.WithTimeout(context.Background(), tracerProviderExportCreationTimeout)
	defer cancel()
	return otlptrace.New(ctx, client)
}
