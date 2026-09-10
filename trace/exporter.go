// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

// newExporter only applies client options for the [ExporterConfig] fields
// that are actually set. The OTLP clients read the standard OTel environment
// variables (OTEL_EXPORTER_OTLP_ENDPOINT, _HEADERS, _TIMEOUT, etc.) but
// explicit options override them, so an unconditional option would mask its
// environment variable. The request timeout is always left to the client
// default (10s, the same as the previously pinned value) so that
// OTEL_EXPORTER_OTLP_TIMEOUT is honored.
func newExporter(config ExporterConfig) (sdktrace.SpanExporter, error) {
	var client otlptrace.Client
	switch config.Type {
	case GRPC:
		var opts []otlptracegrpc.Option
		if len(config.Headers) > 0 {
			opts = append(opts, otlptracegrpc.WithHeaders(config.Headers))
		}
		if config.Endpoint != "" {
			opts = append(opts, otlptracegrpc.WithEndpoint(config.Endpoint))
		}
		if config.Insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		client = otlptracegrpc.NewClient(opts...)
	case HTTP:
		var opts []otlptracehttp.Option
		if len(config.Headers) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(config.Headers))
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
