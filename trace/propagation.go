// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import (
	oteltrace "go.opentelemetry.io/otel/trace"
)

// SpanContextLen is the length of a binary-encoded span context.
//
// Layout (W3C binary trace context, version 0):
//
//	[0]     version (0)
//	[1]     field id 0, [2:18]  trace ID
//	[18]    field id 1, [19:27] span ID
//	[27]    field id 2, [28]    trace flags
const SpanContextLen = 29

const (
	traceIDField = 0
	spanIDField  = 1
	flagsField   = 2
)

// MarshalSpanContext returns the binary encoding of sc, or nil if sc is not
// valid.
func MarshalSpanContext(sc oteltrace.SpanContext) []byte {
	if !sc.IsValid() {
		return nil
	}
	b := make([]byte, SpanContextLen)
	b[1] = traceIDField
	tid := sc.TraceID()
	copy(b[2:18], tid[:])
	b[18] = spanIDField
	sid := sc.SpanID()
	copy(b[19:27], sid[:])
	b[27] = flagsField
	b[28] = byte(sc.TraceFlags())
	return b
}

// UnmarshalSpanContext decodes a span context produced by MarshalSpanContext.
// Any malformed input yields an invalid (zero) SpanContext. The result is
// marked remote.
func UnmarshalSpanContext(b []byte) oteltrace.SpanContext {
	if len(b) != SpanContextLen || b[0] != 0 ||
		b[1] != traceIDField || b[18] != spanIDField || b[27] != flagsField {
		return oteltrace.SpanContext{}
	}
	return oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    oteltrace.TraceID(b[2:18]),
		SpanID:     oteltrace.SpanID(b[19:27]),
		TraceFlags: oteltrace.TraceFlags(b[28]) & oteltrace.FlagsSampled,
		Remote:     true,
	})
}
