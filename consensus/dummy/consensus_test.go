// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"math"
	"math/big"
	"testing"

	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ethereum/go-ethereum/common"
)

var testBlockGasCostStep = big.NewInt(50_000)

func TestVerifyBlockFee(t *testing.T) {
	tests := map[string]struct {
		baseFee                 *big.Int
		parentBlockGasCost      *big.Int
		parentTime, currentTime uint64
		txs                     []*types.Transaction
		receipts                []*types.Receipt
		shouldErr               bool
	}{
		"tx only base fee": {
			baseFee:            big.NewInt(100),
			parentBlockGasCost: big.NewInt(0),
			parentTime:         10,
			currentTime:        10,
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
			parentTime:         10,
			currentTime:        10,
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
			parentTime:         10,
			currentTime:        10,
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
			parentTime:         10,
			currentTime:        10,
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
			parentTime:         10,
			currentTime:        22, // 2s target + 10
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
			parentTime:         0,
			currentTime:        math.MaxUint64,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 100, big.NewInt(100), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 1000},
			},
			shouldErr: false,
		},
		"parent time > current time": {
			baseFee:            big.NewInt(100),
			parentBlockGasCost: big.NewInt(0),
			parentTime:         11,
			currentTime:        10,
			txs:                nil,
			receipts:           nil,
			shouldErr:          true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			blockGasCost := calcBlockGasCost(
				params.DefaultFeeConfig.TargetBlockRate,
				params.DefaultFeeConfig.MinBlockGasCost,
				params.DefaultFeeConfig.MaxBlockGasCost,
				testBlockGasCostStep,
				test.parentBlockGasCost,
				test.parentTime, test.currentTime,
			)
			engine := NewFaker()
			if err := engine.verifyBlockFee(test.baseFee, blockGasCost, test.txs, test.receipts); err != nil {
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
