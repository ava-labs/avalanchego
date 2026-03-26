// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/libevm/ethapi"
	"github.com/ava-labs/libevm/params"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/strevm/cmputils"
	saerpc "github.com/ava-labs/strevm/sae/rpc"
	"github.com/ava-labs/strevm/saetest"
)

func TestGetChainConfig(t *testing.T) {
	ctx, sut := newSUT(t, 0)
	sut.testRPC(ctx, t, rpcTest{
		method: "eth_getChainConfig",
		want:   *saetest.ChainConfig(),
	})
}

func TestBaseFee(t *testing.T) {
	ctx, sut := newSUT(t, 0)
	sut.testRPC(ctx, t, rpcTest{
		method: "eth_baseFee",
		want:   hexBig(params.InitialBaseFee),
	})

	b := sut.runConsensusLoop(t)
	sut.testRPC(ctx, t, rpcTest{
		method: "eth_baseFee",
		want:   (*hexutil.Big)(b.WorstCaseBounds().LatestEndTime.BaseFee().ToBig()),
	})
}

func TestSuggestPriceOptions(t *testing.T) {
	ctx, sut := newSUT(t, 0)
	// Before any blocks with worst-case bounds, the base fee falls back to the
	// genesis base fee and the tip defaults to the minimum (no txs yet).
	sut.testRPC(ctx, t, rpcTest{
		method: "eth_suggestPriceOptions",
		want:   saerpc.NewPriceOptions(big.NewInt(params.Wei), big.NewInt(2*params.InitialBaseFee)),
	})

	b := sut.runConsensusLoop(t)

	// This just asserts the round-tripping of the PriceOptions through the RPC.
	// See testing of [saerpc.NewPriceOptions] for behavioral tests.
	tip, err := sut.rawVM.GethRPCBackends().SuggestGasTipCap(t.Context())
	require.NoErrorf(t, err, "SuggestGasTipCap()")
	doubleBaseFee := b.WorstCaseBounds().LatestEndTime.BaseFee().ToBig()
	doubleBaseFee.Lsh(doubleBaseFee, 1)
	sut.testRPC(ctx, t, rpcTest{
		method: "eth_suggestPriceOptions",
		want:   saerpc.NewPriceOptions(tip, doubleBaseFee),
	})
}

func TestNewAcceptedTransactions(t *testing.T) {
	ctx, sut := newSUT(t, 1)

	hashes := make(chan common.Hash, 1)
	{
		sub, err := sut.rpcClient.EthSubscribe(ctx, hashes, "newAcceptedTransactions")
		require.NoErrorf(t, err, "EthSubscribe(newAcceptedTransactions)")
		t.Cleanup(sub.Unsubscribe)
	}
	txs := make(chan *ethapi.RPCTransaction, 1)
	{
		sub, err := sut.rpcClient.EthSubscribe(ctx, txs, "newAcceptedTransactions", true)
		require.NoErrorf(t, err, "EthSubscribe(newAcceptedTransactions, true)")
		t.Cleanup(sub.Unsubscribe)
	}

	tx := sut.wallet.SetNonceAndSign(t, 0, &types.LegacyTx{
		To:       &zeroAddr,
		Gas:      params.TxGas,
		GasPrice: big.NewInt(1),
	})
	block := sut.runConsensusLoop(t, tx)

	t.Run("hash_only", func(t *testing.T) {
		require.Equalf(t, tx.Hash(), <-hashes, "accepted tx hash")
	})
	t.Run("full_tx", func(t *testing.T) {
		want := ethapi.NewRPCTransaction(tx, block.Hash(), block.NumberU64(), block.BuildTime(), 0, block.Header().BaseFee, saetest.ChainConfig())
		if diff := cmp.Diff(want, <-txs, cmputils.HexutilBigs(), cmputils.NilSlicesAreEmpty[hexutil.Bytes]()); diff != "" {
			t.Errorf("full tx diff (-want +got):\n%s", diff)
		}
	})
}
