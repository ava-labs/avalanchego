// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

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
