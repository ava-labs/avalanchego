// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/ava-labs/avalanchego/snow"
	"github.com/ava-labs/avalanchego/vms/saevm/saetrace"
	"github.com/ava-labs/avalanchego/vms/saevm/saetrace/saetracetest"

	oteltrace "go.opentelemetry.io/otel/trace"
)

func TestSendTxSpan(t *testing.T) {
	ctx, sut := newSUT(t, 1)
	tp, exp := saetracetest.NewTestProvider(t)
	ctx, root := tp.Tracer("t").Start(ctx, "root")
	tx := sut.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{To: &common.Address{}, Gas: 21_000, GasPrice: big.NewInt(1)})
	require.NoError(t, sut.rawVM.mempool.SendTx(ctx, tx), "SendTx()")
	root.End()
	require.Equal(t, root.SpanContext().SpanID(), saetracetest.ParentOf(t, exp.GetSpans(), "txgossip.SendTx").SpanID(), "SendTx parent")
}

func TestStateRPCSpan(t *testing.T) {
	ctx, sut := newSUT(t, 1)
	tp, exp := saetracetest.NewTestProvider(t)
	ctx, root := tp.Tracer("t").Start(ctx, "root")
	_, _, err := sut.rawVM.GethRPCBackends().StateAndHeaderByNumber(ctx, rpc.LatestBlockNumber)
	require.NoError(t, err, "StateAndHeaderByNumber()")
	root.End()
	spans := exp.GetSpans()
	saetracetest.ParentOf(t, spans, "saerpc.StateAndHeaderByNumberOrHash")
	require.Equal(t, root.SpanContext().TraceID(), saetracetest.ParentOf(t, spans, "saerpc.stateAtBlock").TraceID(), "stateAtBlock in same trace")
}

func TestBuildAndVerifySpans(t *testing.T) {
	ctx, sut := newSUT(t, 1)
	tp, exp := saetracetest.NewTestProvider(t)
	ctx, root := tp.Tracer("t").Start(ctx, "root")
	tx := sut.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{To: &common.Address{}, Gas: 21_000, GasPrice: big.NewInt(1)})
	sut.sendTxsAndWaitUntilPending(t, tx)
	blk, err := sut.BuildBlock(ctx)
	require.NoError(t, err, "BuildBlock()")
	require.NoError(t, blk.Verify(ctx), "Verify()")
	root.End()
	spans := exp.GetSpans()

	// The build path is the first buildWithTxs span to end.
	build := saetracetest.ParentOf(t, spans, "saevm.buildWithTxs")
	require.Equal(t, root.SpanContext().SpanID(), build.SpanID(), "buildWithTxs parent")
	verifyID, rebuildParent := oteltrace.SpanID{}, oteltrace.SpanID{}
	for _, s := range spans {
		switch s.Name {
		case "saevm.buildWithTxs":
			require.Len(t, s.Events, 1, "one tx event")
			require.Contains(t, s.Events[0].Attributes, saetrace.Hash("tx_hash", tx.Hash()), "tx_hash event attribute")
			require.Contains(t, s.Attributes, attribute.Int("tx_count", 1), "tx_count")
			require.Contains(t, s.Attributes, attribute.Int("txs_skipped", 0), "txs_skipped")
		case "saevm.VerifyBlock":
			verifyID = s.SpanContext.SpanID()
			require.Contains(t, s.Attributes, attribute.Bool("bootstrapping", false), "bootstrapping")
		case "saevm.rebuild":
			rebuildParent = s.Parent.SpanID()
		}
	}
	require.NotEqual(t, oteltrace.SpanID{}, verifyID, "VerifyBlock span present")
	require.Equal(t, verifyID, rebuildParent, "rebuild parent is VerifyBlock")
}

func TestAcceptSpan(t *testing.T) {
	ctx, sut := newSUT(t, 1)
	tp, exp := saetracetest.NewTestProvider(t)
	ctx, root := tp.Tracer("t").Start(ctx, "root")
	tx := sut.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{To: &common.Address{}, Gas: 21_000, GasPrice: big.NewInt(1)})
	blk := sut.buildAndParseBlock(t, sut.genesis, tx)
	require.NoError(t, blk.Verify(ctx), "Verify()")
	require.NoError(t, blk.Accept(ctx), "Accept()")
	root.End()
	accept := saetracetest.ParentOf(t, exp.GetSpans(), "saevm.AcceptBlock")
	require.Equal(t, root.SpanContext().SpanID(), accept.SpanID(), "AcceptBlock parent")
}

func TestWaitUntilExecutedSpanEndsOnCancel(t *testing.T) {
	blocking := common.Address{'b', 'l', 'o', 'c', 'k'}
	opt, unblock := withBlockingPrecompile(blocking)
	ctx, sut := newSUT(t, 1, opt)
	// Registered after the SUT's cleanup so that it runs first.
	t.Cleanup(func() {
		unblock()
		require.NoError(t, sut.SetState(ctx, snow.NormalOp), "SetState(NormalOp)")
	})

	tp, exp := saetracetest.NewTestProvider(t)
	ctx, root := tp.Tracer("t").Start(ctx, "root")
	tx := sut.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{To: &blocking, Gas: 100_000, GasPrice: big.NewInt(1)})
	blk := sut.buildAndParseBlock(t, sut.genesis, tx)
	require.NoError(t, blk.Verify(ctx), "Verify()")
	require.NoError(t, sut.SetState(ctx, snow.Bootstrapping), "SetState(Bootstrapping)")

	acceptCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	time.AfterFunc(100*time.Millisecond, cancel)
	require.NotNil(t, blk.Accept(acceptCtx), "Accept() with cancelled context")
	root.End()

	var found bool
	for _, s := range exp.GetSpans() {
		if s.Name == "saevm.WaitUntilExecuted" {
			found = true
			require.Equal(t, codes.Error, s.Status.Code, "WaitUntilExecuted status")
		}
	}
	require.True(t, found, "WaitUntilExecuted span present")
}
