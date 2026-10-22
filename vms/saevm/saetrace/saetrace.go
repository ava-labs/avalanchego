// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package saetrace provides helpers for creating OpenTelemetry spans inside
// the SAE VM. The VM has no tracer of its own: spans derive from the tracer
// provider of the parent span in the context, so they are no-ops when the
// caller is not tracing.
package saetrace

import (
	"context"
	"math"
	"strconv"

	"github.com/ava-labs/libevm/common"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	oteltrace "go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/ava-labs/avalanchego/vms/saevm"

// Start starts a span as a child of the span in ctx, using that span's tracer
// provider. If ctx carries no recording span, the returned span is a no-op.
func Start(ctx context.Context, name string, opts ...oteltrace.SpanStartOption) (context.Context, oteltrace.Span) {
	return oteltrace.SpanFromContext(ctx).TracerProvider().Tracer(tracerName).Start(ctx, name, opts...) //nolint:spancheck // the caller ends the returned span
}

// Uint64 returns an integer attribute if v fits in an int64, otherwise a
// decimal string attribute.
func Uint64(key string, v uint64) attribute.KeyValue {
	if v <= math.MaxInt64 {
		return attribute.Int64(key, int64(v))
	}
	return attribute.String(key, strconv.FormatUint(v, 10))
}

// Hash returns a string attribute holding the hex encoding of h.
func Hash(key string, h common.Hash) attribute.KeyValue {
	return attribute.String(key, h.Hex())
}

// RecordError records err on the span and marks it as failed. It is a no-op
// if err is nil.
func RecordError(span oteltrace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
