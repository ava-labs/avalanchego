// Copyright (C) 2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sae

import (
	"math"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common/hexutil"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethclient"
	"github.com/ava-labs/libevm/params"
	"github.com/ava-labs/libevm/rpc"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/strevm/gastime"
	saeparams "github.com/ava-labs/strevm/params"
)

func TestGasPriceAPIs(t *testing.T) {
	tests := []struct {
		name    string
		txTips  []uint64 // one tx per block
		wantTip uint64
	}{
		{
			name:    "genesis",
			wantTip: params.Wei,
		},
		{
			name:    "after_block_with_tip",
			txTips:  []uint64{100},
			wantTip: 100,
		},
		{
			name:    "multiple_blocks",
			txTips:  []uint64{100, 110, 120, 130},
			wantTip: 110, // because suggestion is 40th percentil
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, sut := newSUT(t, 1)
			for _, tip := range tt.txTips {
				sut.runConsensusLoop(t, sut.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
					To:        &zeroAddr,
					Gas:       params.TxGas,
					GasTipCap: new(big.Int).SetUint64(tip),
					GasFeeCap: new(big.Int).SetUint64(math.MaxUint64),
				}))
			}

			b := sut.lastAcceptedBlock(t)
			require.NoError(t, b.WaitUntilExecuted(ctx), "last-accepted %T.WaitUntilExecuted()", b)
			baseFee := b.ExecutedBaseFee()
			sut.testRPC(ctx, t,
				rpcTest{
					method: "eth_maxPriorityFeePerGas",
					want:   hexutil.Uint64(tt.wantTip),
				},
				rpcTest{
					method: "eth_gasPrice",
					want:   hexutil.Uint64(tt.wantTip + baseFee.Uint64()),
				},
			)
		})
	}
}

func TestFeeHistory(t *testing.T) {
	// The actual implementation is tested extensively in the [gasprice] package
	// so we are merely demonstrating correct plumbing.

	ctx, sut := newSUT(t, 1)
	tips := []uint64{100, 200, 300}
	for _, tip := range tips {
		sut.runConsensusLoop(t, sut.wallet.SetNonceAndSign(t, 0, &types.DynamicFeeTx{
			To:        &zeroAddr,
			Gas:       params.TxGas,
			GasTipCap: new(big.Int).SetUint64(tip),
			GasFeeCap: new(big.Int).SetUint64(math.MaxUint64),
		}))
	}
	require.NoError(t, sut.lastAcceptedBlock(t).WaitUntilExecuted(ctx), "last-accepted Block.WaitUntilExecuted()")

	gasRate := sut.hooks.Target * gastime.TargetToRate
	blockGasLimit := gasRate * saeparams.TauSeconds * saeparams.Lambda // by definition
	gasUsedRatio := float64(params.TxGas) / float64(blockGasLimit)

	baseFee := hexBig(1)

	numBlocks := hexutil.Uint64(2)   // to fetch
	rewardPercentile := []float64{0} // only one tip anyway

	sut.testRPC(ctx, t, rpcTest{
		method: "eth_feeHistory",
		args:   []any{numBlocks, rpc.BlockNumber(3), rewardPercentile},
		want: ethclient.FeeHistoryResult{
			OldestBlock: hexBig(2),
			Reward: [][]*hexutil.Big{
				{hexBigU(tips[1])},
				{hexBigU(tips[2])},
			},
			BaseFee: []*hexutil.Big{
				baseFee,
				baseFee,
				baseFee, // the extra value is for the next block; the weird API signature is inherited from geth
			},
			GasUsedRatio: []float64{gasUsedRatio, gasUsedRatio},
		},
	})
}
