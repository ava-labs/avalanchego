// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	oteltrace "go.opentelemetry.io/otel/trace"
)

func TestSamplerIgnoresRemoteSampledFlag(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := newTracerProvider(exp, Config{TraceSampleRate: 0}) // never sample by ratio
	t.Cleanup(func() { _ = tp.Shutdown(t.Context()) })

	parent := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    oteltrace.TraceID{1},
		SpanID:     oteltrace.SpanID{1},
		TraceFlags: oteltrace.FlagsSampled,
		Remote:     true,
	})
	ctx := oteltrace.ContextWithRemoteSpanContext(t.Context(), parent)
	_, span := tp.Tracer("t").Start(ctx, "child")
	span.End()
	require.NoError(t, tp.ForceFlush(t.Context()), "ForceFlush()")
	require.Empty(t, exp.GetSpans(), "a remote sampled parent must not force sampling")
}

func TestSamplerSamplesRemoteParentAtFullRate(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := newTracerProvider(exp, Config{TraceSampleRate: 1})
	t.Cleanup(func() { _ = tp.Shutdown(t.Context()) })

	parent := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    oteltrace.TraceID{1},
		SpanID:     oteltrace.SpanID{1},
		TraceFlags: oteltrace.FlagsSampled,
		Remote:     true,
	})
	ctx := oteltrace.ContextWithRemoteSpanContext(t.Context(), parent)
	_, span := tp.Tracer("t").Start(ctx, "child")
	span.End()
	require.NoError(t, tp.ForceFlush(t.Context()), "ForceFlush()")
	spans := exp.GetSpans()
	require.Len(t, spans, 1, "spans exported at full sample rate")
	require.Equal(t, parent.TraceID(), spans[0].Parent.TraceID(), "child must join the remote trace")
}

func TestResourceAttributesOnSpans(t *testing.T) {
	exp := tracetest.NewInMemoryExporter()
	tp := newTracerProvider(exp, Config{
		TraceSampleRate:    1,
		AppName:            "app",
		Version:            "v",
		ResourceAttributes: map[string]string{"network_uuid": "abc", "node_id": "NodeID-1"},
	})
	t.Cleanup(func() { require.NoError(t, tp.Shutdown(context.WithoutCancel(t.Context())), "Shutdown()") })
	_, span := tp.Tracer("t").Start(t.Context(), "s")
	span.End()
	require.NoError(t, tp.ForceFlush(t.Context()), "ForceFlush()")
	spans := exp.GetSpans()
	require.Len(t, spans, 1, "exported spans")
	attrs := spans[0].Resource.Attributes()
	require.Contains(t, attrs, attribute.String("network_uuid", "abc"), "network_uuid resource attribute")
	require.Contains(t, attrs, attribute.String("node_id", "NodeID-1"), "node_id resource attribute")
	require.Contains(t, attrs, semconv.ServiceNameKey.String("app"), "service.name kept")
}
