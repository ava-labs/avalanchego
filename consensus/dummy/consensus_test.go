// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"math"
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ethereum/go-ethereum/common"
)

func TestVerifyBlockFee(t *testing.T) {
	tests := map[string]struct {
		baseFee                *big.Int
		maxGasBlockFee         *big.Int
		blockFeeDuration       uint64
		timeElapsed            uint64
		txs                    []*types.Transaction
		receipts               []*types.Receipt
		extraStateContribution *big.Int
		shouldErr              bool
	}{
		"tx only base fee": {
			baseFee:          big.NewInt(100),
			maxGasBlockFee:   big.NewInt(1000),
			blockFeeDuration: 1,
			timeElapsed:      0,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 100, big.NewInt(100), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 1000},
			},
			extraStateContribution: nil,
			shouldErr:              true,
		},
		"tx covers exactly block fee": {
			baseFee:          big.NewInt(100),
			maxGasBlockFee:   big.NewInt(1000),
			blockFeeDuration: 1,
			timeElapsed:      0,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 1000, big.NewInt(200), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 1000},
			},
			extraStateContribution: nil,
			shouldErr:              false,
		},
		"txs share block fee": {
			baseFee:          big.NewInt(100),
			maxGasBlockFee:   big.NewInt(1000),
			blockFeeDuration: 1,
			timeElapsed:      0,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 1000, big.NewInt(200), nil),
				types.NewTransaction(1, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 1000, big.NewInt(100), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 1000},
				{GasUsed: 1000},
			},
			extraStateContribution: nil,
			shouldErr:              false,
		},
		"txs split block fee": {
			baseFee:          big.NewInt(100),
			maxGasBlockFee:   big.NewInt(1000),
			blockFeeDuration: 1,
			timeElapsed:      0,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 1000, big.NewInt(150), nil),
				types.NewTransaction(1, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 1000, big.NewInt(150), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 1000},
				{GasUsed: 1000},
			},
			extraStateContribution: nil,
			shouldErr:              false,
		},
		"split block fee with extra state contribution": {
			baseFee:          big.NewInt(100),
			maxGasBlockFee:   big.NewInt(1000),
			blockFeeDuration: 1,
			timeElapsed:      0,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 1000, big.NewInt(150), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 1000},
			},
			extraStateContribution: big.NewInt(500),
			shouldErr:              false,
		},
		"extra state contribution insufficient": {
			baseFee:                big.NewInt(100),
			maxGasBlockFee:         big.NewInt(1000),
			blockFeeDuration:       1,
			timeElapsed:            0,
			txs:                    nil,
			receipts:               nil,
			extraStateContribution: big.NewInt(999),
			shouldErr:              true,
		},
		"negative extra state contribution": {
			baseFee:                big.NewInt(100),
			maxGasBlockFee:         big.NewInt(1000),
			blockFeeDuration:       1,
			timeElapsed:            0,
			txs:                    nil,
			receipts:               nil,
			extraStateContribution: big.NewInt(-1),
			shouldErr:              true,
		},
		"extra state contribution covers block fee": {
			baseFee:                big.NewInt(100),
			maxGasBlockFee:         big.NewInt(1000),
			blockFeeDuration:       1,
			timeElapsed:            0,
			txs:                    nil,
			receipts:               nil,
			extraStateContribution: big.NewInt(1000),
			shouldErr:              false,
		},
		"extra state contribution covers more than block fee": {
			baseFee:                big.NewInt(100),
			maxGasBlockFee:         big.NewInt(1000),
			blockFeeDuration:       1,
			timeElapsed:            0,
			txs:                    nil,
			receipts:               nil,
			extraStateContribution: big.NewInt(1001),
			shouldErr:              false,
		},
		"tx only base fee after full time window": {
			baseFee:          big.NewInt(100),
			maxGasBlockFee:   big.NewInt(1000),
			blockFeeDuration: 1,
			timeElapsed:      1,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 100, big.NewInt(100), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 1000},
			},
			extraStateContribution: nil,
			shouldErr:              false,
		},
		"tx only base fee after large time window": {
			baseFee:          big.NewInt(100),
			maxGasBlockFee:   big.NewInt(1000),
			blockFeeDuration: 1,
			timeElapsed:      math.MaxUint64,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 100, big.NewInt(100), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 1000},
			},
			extraStateContribution: nil,
			shouldErr:              false,
		},
		"tx covers exactly block fee after half block fee duration": {
			baseFee:          big.NewInt(100),
			maxGasBlockFee:   big.NewInt(1000),
			blockFeeDuration: 2,
			timeElapsed:      1,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 1000, big.NewInt(150), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 1000},
			},
			extraStateContribution: nil,
			shouldErr:              false,
		},
		"tx covers less than block fee after half block fee duration": {
			baseFee:          big.NewInt(100),
			maxGasBlockFee:   big.NewInt(1000),
			blockFeeDuration: 2,
			timeElapsed:      1,
			txs: []*types.Transaction{
				types.NewTransaction(0, common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8"), big.NewInt(0), 1000, big.NewInt(149), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 1000},
			},
			extraStateContribution: nil,
			shouldErr:              true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			engine := NewDummyEngine(new(ConsensusCallbacks))
			if err := engine.verifyBlockFee(test.baseFee, test.maxGasBlockFee, test.blockFeeDuration, test.timeElapsed, test.txs, test.receipts, test.extraStateContribution); err != nil {
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
