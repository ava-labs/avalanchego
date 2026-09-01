// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package saetrace provides OpenTelemetry helpers shared by the SAE packages
// that record spans.
package saetrace

import (
	"context"
	"math"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	oteltrace "go.opentelemetry.io/otel/trace"
)

// TracerFrom returns a [oteltrace.Tracer], named for the instrumentation
// scope, from the provider of the span in ctx. If ctx carries no span this is
// a no-op tracer, so callers without tracing pay no cost.
func TracerFrom(ctx context.Context, scope string) oteltrace.Tracer {
	return oteltrace.SpanFromContext(ctx).TracerProvider().Tracer(scope)
}

// Traced runs fn in a child span of ctx with the given name, recording any
// returned error on the span. The span's tracer is derived from ctx as
// documented on [TracerFrom].
func Traced(ctx context.Context, scope, name string, fn func(context.Context) error) error {
	ctx, span := TracerFrom(ctx, scope).Start(ctx, name)
	defer span.End()
	if err := fn(ctx); err != nil {
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

// Int64Attr returns a span attribute for v, saturating at [math.MaxInt64],
// which is acceptable for observability data.
func Int64Attr(key string, v uint64) attribute.KeyValue {
	if v > math.MaxInt64 {
		return attribute.Int64(key, math.MaxInt64)
	}
	return attribute.Int64(key, int64(v)) //#nosec G115 -- bounded above
}
