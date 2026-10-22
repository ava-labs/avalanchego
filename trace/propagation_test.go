// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package trace

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	oteltrace "go.opentelemetry.io/otel/trace"
)

func testSpanContext() oteltrace.SpanContext {
	return oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    oteltrace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:     oteltrace.SpanID{1, 2, 3, 4, 5, 6, 7, 8},
		TraceFlags: oteltrace.FlagsSampled,
	})
}

func TestSpanContextRoundTrip(t *testing.T) {
	sc := testSpanContext()
	b := MarshalSpanContext(sc)
	require.Len(t, b, SpanContextLen, "MarshalSpanContext()")
	got := UnmarshalSpanContext(b)
	require.True(t, got.IsValid(), "UnmarshalSpanContext().IsValid()")
	require.True(t, got.IsRemote(), "UnmarshalSpanContext().IsRemote()")
	require.Equal(t, sc.TraceID(), got.TraceID(), "TraceID")
	require.Equal(t, sc.SpanID(), got.SpanID(), "SpanID")
	require.Equal(t, sc.TraceFlags(), got.TraceFlags(), "TraceFlags")
}

func TestMarshalInvalidSpanContext(t *testing.T) {
	require.Nil(t, MarshalSpanContext(oteltrace.SpanContext{}), "MarshalSpanContext(invalid)")
}

func TestUnmarshalSpanContextRejects(t *testing.T) {
	valid := MarshalSpanContext(testSpanContext())
	mutate := func(f func(b []byte)) []byte {
		b := slices.Clone(valid)
		f(b)
		return b
	}
	tests := map[string][]byte{
		"nil":             nil,
		"empty":           {},
		"short":           valid[:SpanContextLen-1],
		"long":            append(slices.Clone(valid), 0),
		"bad version":     mutate(func(b []byte) { b[0] = 1 }),
		"bad traceid tag": mutate(func(b []byte) { b[1] = 9 }),
		"bad spanid tag":  mutate(func(b []byte) { b[18] = 9 }),
		"bad flags tag":   mutate(func(b []byte) { b[27] = 9 }),
		"zero trace id":   mutate(func(b []byte) { clear(b[2:18]) }),
		"zero span id":    mutate(func(b []byte) { clear(b[19:27]) }),
	}
	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			require.False(t, UnmarshalSpanContext(in).IsValid(), "UnmarshalSpanContext(%q).IsValid()", name)
		})
	}
}

func TestUnmarshalSpanContextMasksFlags(t *testing.T) {
	b := MarshalSpanContext(testSpanContext())
	b[28] = 0xFF
	got := UnmarshalSpanContext(b)
	require.Equal(t, oteltrace.FlagsSampled, got.TraceFlags(), "flags 0xFF masked")
	b[28] = 0x00
	got = UnmarshalSpanContext(b)
	require.True(t, got.IsValid(), "UnmarshalSpanContext().IsValid() with zero flags")
	require.Equal(t, oteltrace.TraceFlags(0), got.TraceFlags(), "flags 0x00")
}
