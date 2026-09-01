// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestBlockBuildTracing(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tracer := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)).Tracer("test")

	ctx, sut := newSUT(t, 1)

	tx := sut.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
		To:        &common.Address{},
		Gas:       params.TxGas,
		GasFeeCap: big.NewInt(1),
	})
	sut.sendTxsAndWaitUntilPending(t, tx)

	// In production the caller span comes from [tracedvm.NewBlockVM]; block
	// building must record its spans as children of whatever span is in ctx.
	buildCtx, caller := tracer.Start(ctx, "test.build_block")
	_, err := sut.BuildBlock(buildCtx)
	caller.End()
	require.NoError(t, err, "BuildBlock()")

	spans := recorder.Ended()
	var buildSpan sdktrace.ReadOnlySpan
	for _, s := range spans {
		if s.Name() == "saevm.builder.build_block" {
			buildSpan = s
			break
		}
	}
	require.NotNil(t, buildSpan, "span %q must be recorded by BuildBlock()", "saevm.builder.build_block")
	require.Equal(t, caller.SpanContext().SpanID(), buildSpan.Parent().SpanID(), "build_block span must be a child of the caller's span")

	attrs := attribute.NewSet(buildSpan.Attributes()...)
	gotTxs, ok := attrs.Value("saevm.builder.included_txs")
	require.True(t, ok, "build_block span attribute saevm.builder.included_txs")
	require.Equal(t, int64(1), gotTxs.AsInt64(), "saevm.builder.included_txs attribute")

	wantChildren := []string{
		"saevm.hook.build_header",
		"saevm.builder.worstcase_replay",
		"saevm.builder.select_transactions",
		"saevm.hook.potential_end_of_block_ops",
		"saevm.hook.build_block",
	}
	var gotChildren []string
	for _, s := range spans {
		if s.Parent().SpanID() == buildSpan.SpanContext().SpanID() {
			gotChildren = append(gotChildren, s.Name())
		}
	}
	require.Subset(t, gotChildren, wantChildren, "build_block child spans")
}
