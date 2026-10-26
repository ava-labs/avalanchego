// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package router

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type closableTracer struct{ oteltrace.Tracer }

func (closableTracer) Close() error { return nil }

// inboundOnlyRouter implements only HandleInbound; other methods panic.
type inboundOnlyRouter struct{ Router }

func (inboundOnlyRouter) HandleInbound(context.Context, *message.InboundMessage) {}

func TestTracedRouterRemoteParent(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	r := Trace(inboundOnlyRouter{}, closableTracer{provider.Tracer("test")})

	remote := oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    oteltrace.TraceID{1},
		SpanID:     oteltrace.SpanID{2},
		TraceFlags: oteltrace.FlagsSampled,
		Remote:     true,
	})
	msg := message.InboundAppRequest(ids.Empty, 1, time.Second, nil, ids.EmptyNodeID)

	r.HandleInbound(oteltrace.ContextWithRemoteSpanContext(t.Context(), remote), msg)
	r.HandleInbound(t.Context(), msg)

	spans := exporter.GetSpans()
	require.Len(t, spans, 2, "exported spans")
	for _, s := range spans {
		require.Equal(t, "tracedRouter.HandleInbound", s.Name, "span name")
	}
	require.Contains(t, spans[0].Attributes, attribute.Bool("remote_parent", true), "remote case attributes")
	require.Equal(t, remote.TraceID(), spans[0].Parent.TraceID(), "remote case parent TraceID")
	require.Contains(t, spans[1].Attributes, attribute.Bool("remote_parent", false), "local case attributes")
}
