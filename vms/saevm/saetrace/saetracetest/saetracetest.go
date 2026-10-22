// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package saetracetest provides test helpers for code instrumented with
// [saetrace].
package saetracetest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// NewTestProvider returns a provider that samples every span and synchronously
// exports it to the returned exporter. It is shut down when the test ends.
func NewTestProvider(tb testing.TB) (*sdktrace.TracerProvider, *tracetest.InMemoryExporter) {
	tb.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp), sdktrace.WithSampler(sdktrace.AlwaysSample()))
	tb.Cleanup(func() {
		require.NoError(tb, tp.Shutdown(context.Background()), "TracerProvider.Shutdown()")
	})
	return tp, exp
}

// ParentOf returns the parent span context of the first span called name,
// failing the test if there is none.
func ParentOf(tb testing.TB, spans tracetest.SpanStubs, name string) oteltrace.SpanContext {
	tb.Helper()
	var names []string
	for _, s := range spans {
		if s.Name == name {
			return s.Parent
		}
		names = append(names, s.Name)
	}
	tb.Fatalf("no span named %q; spans present: %q", name, names)
	return oteltrace.SpanContext{}
}
