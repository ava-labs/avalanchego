// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"

	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

// TestExporterHonorsOTelEnv checks that, when the equivalent avalanchego flags
// are unset, the OTLP exporter's endpoint and headers can be configured with
// the standard OTel environment variables.
func TestExporterHonorsOTelEnv(t *testing.T) {
	type export struct {
		path       string
		testHeader string
	}
	received := make(chan export, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case received <- export{path: r.URL.Path, testHeader: r.Header.Get("x-test")}:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	// The http:// scheme also marks the endpoint insecure, exercising env
	// handling with the tracing-insecure flag left false.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", server.URL)
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "x-test=abc")

	tracer, err := New(Config{
		ExporterConfig: ExporterConfig{
			Type: HTTP,
			// Endpoint and Headers deliberately unset.
		},
		TraceSampleRate: 1,
		AppName:         "avalanchego-test",
	})
	require.NoError(t, err, "New()")

	_, span := tracer.Start(t.Context(), "test")
	span.End()
	// Close shuts down the tracer provider, flushing the ended span to the
	// test server.
	require.NoError(t, tracer.Close(), "Close()")

	select {
	case got := <-received:
		require.Equal(t, "/v1/traces", got.path, "OTEL_EXPORTER_OTLP_ENDPOINT must determine the export URL")
		require.Equal(t, "abc", got.testHeader, "OTEL_EXPORTER_OTLP_HEADERS must be sent with exports")
	default:
		t.Fatal("no export received by the endpoint in OTEL_EXPORTER_OTLP_ENDPOINT")
	}
}

// newTestTracer returns a [Tracer] exporting over HTTP to an in-process
// server that accepts and discards everything.
func newTestTracer(t *testing.T, sampleRate float64) Tracer {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	tracer, err := New(Config{
		ExporterConfig: ExporterConfig{
			Type:     HTTP,
			Endpoint: strings.TrimPrefix(server.URL, "http://"),
			Insecure: true,
		},
		TraceSampleRate: sampleRate,
		AppName:         "avalanchego-test",
	})
	require.NoError(t, err, "New()")
	t.Cleanup(func() {
		require.NoError(t, tracer.Close(), "Close()")
	})
	return tracer
}

func TestBatcherOptionsHonorOTelEnv(t *testing.T) {
	require.Len(t, batcherOptions(), 1, "the default export timeout must be pinned when OTEL_BSP_EXPORT_TIMEOUT is unset")

	t.Setenv("OTEL_BSP_EXPORT_TIMEOUT", "30000")
	require.Empty(t, batcherOptions(), "OTEL_BSP_EXPORT_TIMEOUT must take precedence over the default export timeout")
}

func TestSamplerFromConfig(t *testing.T) {
	tracer := newTestTracer(t, 1)

	_, span := tracer.Start(t.Context(), "test")
	defer span.End()
	require.True(t, span.SpanContext().IsSampled(), "TraceSampleRate=1 must sample every span")
}

func TestSamplerEnvOverride(t *testing.T) {
	t.Setenv("OTEL_TRACES_SAMPLER", "always_off")
	tracer := newTestTracer(t, 1)

	_, span := tracer.Start(t.Context(), "test")
	defer span.End()
	require.False(t, span.SpanContext().IsSampled(), "OTEL_TRACES_SAMPLER=always_off must take precedence over TraceSampleRate")
}

func TestNewResourceDefaults(t *testing.T) {
	res, err := newResource("avalanchego-test", "v1.2.3")
	require.NoError(t, err, "newResource()")

	attrs := attribute.NewSet(res.Attributes()...)
	name, ok := attrs.Value(semconv.ServiceNameKey)
	require.True(t, ok, "resource must have a service.name attribute")
	require.Equal(t, "avalanchego-test", name.AsString(), "service.name defaults to the app name")

	version, ok := attrs.Value("version")
	require.True(t, ok, "resource must have a version attribute")
	require.Equal(t, "v1.2.3", version.AsString(), "version attribute")
}

func TestNewResourceHonorsOTelEnv(t *testing.T) {
	t.Setenv("OTEL_SERVICE_NAME", "custom-name")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment=prod")

	res, err := newResource("avalanchego-test", "v1.2.3")
	require.NoError(t, err, "newResource()")

	attrs := attribute.NewSet(res.Attributes()...)
	name, ok := attrs.Value(semconv.ServiceNameKey)
	require.True(t, ok, "resource must have a service.name attribute")
	require.Equal(t, "custom-name", name.AsString(), "OTEL_SERVICE_NAME overrides the default service.name")

	env, ok := attrs.Value("deployment.environment")
	require.True(t, ok, "OTEL_RESOURCE_ATTRIBUTES must be merged into the resource")
	require.Equal(t, "prod", env.AsString(), "deployment.environment from OTEL_RESOURCE_ATTRIBUTES")

	version, ok := attrs.Value("version")
	require.True(t, ok, "default attributes must survive env merging")
	require.Equal(t, "v1.2.3", version.AsString(), "version attribute")
}
