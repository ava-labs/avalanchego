// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import (
	"context"

	"go.opentelemetry.io/otel/trace"

	"github.com/ava-labs/avalanchego/utils/constants"
)

var Noop Tracer = noOpTracer{
	t: trace.NewNoopTracerProvider().Tracer(constants.AppName),
}

// noOpTracer is an implementation of trace.Tracer that does nothing.
type noOpTracer struct {
	t trace.Tracer
}

func (n noOpTracer) Start(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return n.t.Start(ctx, spanName, opts...) //nolint:spancheck
}

func (noOpTracer) Close() error {
	return nil
}
