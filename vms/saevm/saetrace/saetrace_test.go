// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package saetrace

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"

	"github.com/ava-labs/avalanchego/vms/saevm/saetrace/saetracetest"
)

func TestStartIsNoopWithoutParent(t *testing.T) {
	_, span := Start(t.Context(), "x")
	require.False(t, span.IsRecording(), "span.IsRecording() without a parent provider")
}

func TestStartUsesParentProvider(t *testing.T) {
	tp, exp := saetracetest.NewTestProvider(t)
	ctx, root := tp.Tracer("t").Start(t.Context(), "root")
	_, child := Start(ctx, "child")
	child.End()
	root.End()
	require.Equal(t, root.SpanContext().SpanID(), saetracetest.ParentOf(t, exp.GetSpans(), "child").SpanID(), "child parent")
}

func TestUint64(t *testing.T) {
	require.Equal(t, attribute.Int64("k", 7), Uint64("k", 7), "small value")
	require.Equal(t, attribute.String("k", "18446744073709551615"), Uint64("k", math.MaxUint64), "overflowing value")
}
