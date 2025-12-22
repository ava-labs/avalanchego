// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
//
// This file is a derived work, based on the go-ethereum library whose original
// notices appear below.
//
// It is distributed under a license compatible with the licensing terms of the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********
// Copyright 2021 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package gasprice

import (
	"context"
	"errors"
	"math/big"
	"testing"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/core"
	"github.com/ava-labs/libevm/core/types"
	ethparams "github.com/ava-labs/libevm/params"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/params"
	"github.com/ava-labs/avalanchego/graft/evm/rpc"
	"github.com/ava-labs/libevm/common"
)

func TestFeeHistory(t *testing.T) {
	cases := []struct {
		pending      bool
		maxCallBlock uint64
		maxBlock     uint64
		count        uint64
		last         rpc.BlockNumber
		percent      []float64
		expFirst     uint64
		expCount     int
		expErr       error
	}{
		// Standard go-ethereum tests
		{false, 0, 1000, 10, 30, nil, 21, 10, nil},
		{false, 0, 1000, 10, 30, []float64{0, 10}, 21, 10, nil},
		{false, 0, 1000, 10, 30, []float64{20, 10}, 0, 0, errInvalidPercentile},
		{false, 0, 1000, 1000000000, 30, nil, 0, 31, nil},
		{false, 0, 1000, 1000000000, rpc.LatestBlockNumber, nil, 0, 33, nil},
		{false, 0, 1000, 10, 40, nil, 0, 0, errRequestBeyondHead},
		{true, 0, 1000, 10, 40, nil, 0, 0, errRequestBeyondHead},
		{false, 0, 2, 100, rpc.LatestBlockNumber, []float64{0, 10}, 31, 2, nil},
		{false, 0, 2, 100, 32, []float64{0, 10}, 31, 2, nil},
		{false, 0, 1000, 1, rpc.PendingBlockNumber, nil, 0, 0, nil},
		{false, 0, 1000, 2, rpc.PendingBlockNumber, nil, 32, 1, nil},
		{true, 0, 1000, 2, rpc.PendingBlockNumber, nil, 32, 1, nil},
		{true, 0, 1000, 2, rpc.PendingBlockNumber, []float64{0, 10}, 32, 1, nil},

		// Modified tests
		{false, 0, 2, 100, rpc.LatestBlockNumber, nil, 31, 2, nil},    // apply block lookback limits even if only headers required
		{false, 0, 10, 10, 30, nil, 23, 8, nil},                       // limit lookback based on maxHistory from latest block
		{false, 0, 33, 1000000000, 10, nil, 0, 11, nil},               // handle truncation edge case
		{false, 0, 2, 10, 20, nil, 0, 0, errBeyondHistoricalLimit},    // query behind historical limit
		{false, 10, 30, 100, rpc.LatestBlockNumber, nil, 23, 10, nil}, // ensure [MaxCallBlockHistory] is honored
	}
	for i, c := range cases {
		config := Config{
			MaxCallBlockHistory: c.maxCallBlock,
			MaxBlockHistory:     c.maxBlock,
		}
		tip := big.NewInt(1 * params.GWei)
		backend := newTestBackendFakerEngine(t, 32, func(i int, b *core.BlockGen) {
			signer := types.LatestSigner(params.TestChainConfig)

			b.SetCoinbase(common.Address{1})

			baseFee := b.BaseFee()
			feeCap := new(big.Int).Add(baseFee, tip)

			var tx *types.Transaction
			txdata := &types.DynamicFeeTx{
				ChainID:   params.TestChainConfig.ChainID,
				Nonce:     b.TxNonce(addr),
				To:        &common.Address{},
				Gas:       ethparams.TxGas,
				GasFeeCap: feeCap,
				GasTipCap: tip,
				Data:      []byte{},
			}
			tx = types.NewTx(txdata)
			tx, err := types.SignTx(tx, signer, key)
			if err != nil {
				t.Fatalf("failed to create tx: %v", err)
			}
			b.AddTx(tx)
		})
		oracle, err := NewOracle(backend, config)
		require.NoError(t, err)

		first, reward, baseFee, ratio, err := oracle.FeeHistory(context.Background(), c.count, c.last, c.percent)
		backend.teardown()
		expReward := c.expCount
		if len(c.percent) == 0 {
			expReward = 0
		}
		expBaseFee := c.expCount

		if first.Uint64() != c.expFirst {
			t.Fatalf("Test case %d: first block mismatch, want %d, got %d", i, c.expFirst, first)
		}
		if len(reward) != expReward {
			t.Fatalf("Test case %d: reward array length mismatch, want %d, got %d", i, expReward, len(reward))
		}
		if len(baseFee) != expBaseFee {
			t.Fatalf("Test case %d: baseFee array length mismatch, want %d, got %d", i, expBaseFee, len(baseFee))
		}
		if len(ratio) != c.expCount {
			t.Fatalf("Test case %d: gasUsedRatio array length mismatch, want %d, got %d", i, c.expCount, len(ratio))
		}
		if err != c.expErr && !errors.Is(err, c.expErr) {
			t.Fatalf("Test case %d: error mismatch, want %v, got %v", i, c.expErr, err)
		}
	}
}
