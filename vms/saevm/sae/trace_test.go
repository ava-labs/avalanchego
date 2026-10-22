// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/vms/saevm/saetrace/saetracetest"
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
