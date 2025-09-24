// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"math"
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/plugin/evm/customheader"
)

var testFeeConfig = commontype.FeeConfig{
	MinBlockGasCost:          big.NewInt(0),
	MaxBlockGasCost:          big.NewInt(1_000_000),
	TargetBlockRate:          2,
	BlockGasCostStep:         big.NewInt(50_000),
	TargetGas:                big.NewInt(10_000_000),
	BaseFeeChangeDenominator: big.NewInt(12),
}

func TestVerifyBlockFee(t *testing.T) {
	tests := map[string]struct {
		baseFee            *big.Int
		parentBlockGasCost *big.Int
		timeElapsed        uint64
		txs                []*types.Transaction
		receipts           []*types.Receipt
		shouldErr          bool
	}{
		"tx only base fee": {
			baseFee:            big.NewInt(100),
			parentBlockGasCost: big.NewInt(0),
			timeElapsed:        0,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 100, big.NewInt(100), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 1000},
			},
			shouldErr: true,
		},
		"tx covers exactly block fee": {
			baseFee:            big.NewInt(100),
			parentBlockGasCost: big.NewInt(0),
			timeElapsed:        0,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 100_000, big.NewInt(200), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 100_000},
			},
			shouldErr: false,
		},
		"txs share block fee": {
			baseFee:            big.NewInt(100),
			parentBlockGasCost: big.NewInt(0),
			timeElapsed:        0,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 100_000, big.NewInt(200), nil),
				types.NewTransaction(1, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 100_000, big.NewInt(100), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 100_000},
				{GasUsed: 100_000},
			},
			shouldErr: false,
		},
		"txs split block fee": {
			baseFee:            big.NewInt(100),
			parentBlockGasCost: big.NewInt(0),
			timeElapsed:        0,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 100_000, big.NewInt(150), nil),
				types.NewTransaction(1, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 100_000, big.NewInt(150), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 100_000},
				{GasUsed: 100_000},
			},
			shouldErr: false,
		},
		"tx only base fee after full time window": {
			baseFee:            big.NewInt(100),
			parentBlockGasCost: big.NewInt(500_000),
			timeElapsed:        testFeeConfig.TargetBlockRate + 10,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 100, big.NewInt(100), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 1000},
			},
			shouldErr: false,
		},
		"tx only base fee after large time window": {
			baseFee:            big.NewInt(100),
			parentBlockGasCost: big.NewInt(100_000),
			timeElapsed:        math.MaxUint64,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 100, big.NewInt(100), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 1000},
			},
			shouldErr: false,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			blockGasCost := customheader.BlockGasCostWithStep(
				testFeeConfig,
				test.parentBlockGasCost,
				testFeeConfig.BlockGasCostStep.Uint64(),
				test.timeElapsed,
			)
			bigBlockGasCost := new(big.Int).SetUint64(blockGasCost)

			engine := NewFaker()
			if err := engine.verifyBlockFee(test.baseFee, bigBlockGasCost, test.txs, test.receipts); err != nil {
				if !test.shouldErr {
					t.Fatalf("Unexpected error: %s", err)
				}
			} else {
				if test.shouldErr {
					t.Fatal("Should have failed verification")
				}
			}
		})
	}
}
