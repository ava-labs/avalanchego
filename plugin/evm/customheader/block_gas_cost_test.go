// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customheader

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/common/math"
	"github.com/ava-labs/libevm/core/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/coreth/params/extras"
	"github.com/ava-labs/coreth/plugin/evm/customtypes"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap4"
	"github.com/ava-labs/coreth/plugin/evm/upgrade/ap5"
	"github.com/ava-labs/coreth/utils"
)

func TestBlockGasCost(t *testing.T) {
	tests := []struct {
		name       string
		upgrades   extras.NetworkUpgrades
		parentTime uint64
		parentCost *big.Int
		timestamp  uint64
		expected   *big.Int
	}{
		{
			name:       "before_ap4",
			parentTime: 10,
			upgrades:   extras.TestApricotPhase3Config.NetworkUpgrades,
			parentCost: big.NewInt(ap4.MaxBlockGasCost),
			timestamp:  10 + ap4.TargetBlockRate + 1,
			expected:   nil,
		},
		{
			name:       "normal_ap4",
			parentTime: 10,
			upgrades:   extras.TestApricotPhase4Config.NetworkUpgrades,
			parentCost: big.NewInt(ap4.MaxBlockGasCost),
			timestamp:  10 + ap4.TargetBlockRate + 1,
			expected:   big.NewInt(ap4.MaxBlockGasCost - ap4.BlockGasCostStep),
		},
		{
			name:       "normal_ap5",
			upgrades:   extras.TestApricotPhase5Config.NetworkUpgrades,
			parentTime: 10,
			parentCost: big.NewInt(ap4.MaxBlockGasCost),
			timestamp:  10 + ap4.TargetBlockRate + 1,
			expected:   big.NewInt(ap4.MaxBlockGasCost - ap5.BlockGasCostStep),
		},
		{
			name:       "negative_time_elapsed",
			upgrades:   extras.TestApricotPhase4Config.NetworkUpgrades,
			parentTime: 10,
			parentCost: big.NewInt(ap4.MinBlockGasCost),
			timestamp:  9,
			expected:   big.NewInt(ap4.MinBlockGasCost + ap4.BlockGasCostStep*ap4.TargetBlockRate),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := &extras.ChainConfig{
				NetworkUpgrades: test.upgrades,
			}
			parent := customtypes.WithHeaderExtra(
				&types.Header{
					Time: test.parentTime,
				},
				&customtypes.HeaderExtra{
					BlockGasCost: test.parentCost,
				},
			)

			assert.Equal(t, test.expected, BlockGasCost(
				config,
				parent,
				test.timestamp,
			))
		})
	}
}

func TestBlockGasCostWithStep(t *testing.T) {
	tests := []struct {
		name        string
		parentCost  *big.Int
		timeElapsed uint64
		expected    uint64
	}{
		{
			name:        "nil_parentCost",
			parentCost:  nil,
			timeElapsed: 0,
			expected:    ap4.MinBlockGasCost,
		},
		{
			name:        "timeElapsed_0",
			parentCost:  big.NewInt(0),
			timeElapsed: 0,
			expected:    ap4.TargetBlockRate * ap4.BlockGasCostStep,
		},
		{
			name:        "timeElapsed_1",
			parentCost:  big.NewInt(0),
			timeElapsed: 1,
			expected:    (ap4.TargetBlockRate - 1) * ap4.BlockGasCostStep,
		},
		{
			name:        "timeElapsed_0_with_parentCost",
			parentCost:  big.NewInt(50_000),
			timeElapsed: 0,
			expected:    50_000 + ap4.TargetBlockRate*ap4.BlockGasCostStep,
		},
		{
			name:        "timeElapsed_0_with_max_parentCost",
			parentCost:  big.NewInt(ap4.MaxBlockGasCost),
			timeElapsed: 0,
			expected:    ap4.MaxBlockGasCost,
		},
		{
			name:        "timeElapsed_1_with_max_parentCost",
			parentCost:  big.NewInt(ap4.MaxBlockGasCost),
			timeElapsed: 1,
			expected:    ap4.MaxBlockGasCost,
		},
		{
			name:        "timeElapsed_at_target",
			parentCost:  big.NewInt(900_000),
			timeElapsed: ap4.TargetBlockRate,
			expected:    900_000,
		},
		{
			name:        "timeElapsed_over_target_3",
			parentCost:  big.NewInt(ap4.MaxBlockGasCost),
			timeElapsed: 3,
			expected:    ap4.MaxBlockGasCost - (3-ap4.TargetBlockRate)*ap4.BlockGasCostStep,
		},
		{
			name:        "timeElapsed_over_target_10",
			parentCost:  big.NewInt(ap4.MaxBlockGasCost),
			timeElapsed: 10,
			expected:    ap4.MaxBlockGasCost - (10-ap4.TargetBlockRate)*ap4.BlockGasCostStep,
		},
		{
			name:        "timeElapsed_over_target_20",
			parentCost:  big.NewInt(ap4.MaxBlockGasCost),
			timeElapsed: 20,
			expected:    ap4.MaxBlockGasCost - (20-ap4.TargetBlockRate)*ap4.BlockGasCostStep,
		},
		{
			name:        "timeElapsed_over_target_22",
			parentCost:  big.NewInt(ap4.MaxBlockGasCost),
			timeElapsed: 22,
			expected:    ap4.MaxBlockGasCost - (22-ap4.TargetBlockRate)*ap4.BlockGasCostStep,
		},
		{
			name:        "timeElapsed_large_clamped_to_0",
			parentCost:  big.NewInt(ap4.MaxBlockGasCost),
			timeElapsed: 23,
			expected:    0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, BlockGasCostWithStep(
				test.parentCost,
				ap4.BlockGasCostStep,
				test.timeElapsed,
			))
		})
	}
}

func TestEstimateRequiredTip(t *testing.T) {
	tests := []struct {
		name         string
		ap4Timestamp *uint64
		header       *types.Header
		want         *big.Int
		wantErr      error
	}{
		{
			name:         "not_ap4",
			ap4Timestamp: utils.NewUint64(1),
			header:       &types.Header{},
		},
		{
			name:         "nil_base_fee",
			ap4Timestamp: utils.NewUint64(0),
			header: customtypes.WithHeaderExtra(
				&types.Header{},
				&customtypes.HeaderExtra{
					ExtDataGasUsed: big.NewInt(1),
					BlockGasCost:   big.NewInt(1),
				},
			),
			wantErr: errBaseFeeNil,
		},
		{
			name:         "nil_block_gas_cost",
			ap4Timestamp: utils.NewUint64(0),
			header: customtypes.WithHeaderExtra(
				&types.Header{
					BaseFee: big.NewInt(1),
				},
				&customtypes.HeaderExtra{
					ExtDataGasUsed: big.NewInt(1),
				},
			),
			wantErr: errBlockGasCostNil,
		},
		{
			name:         "nil_extra_data_gas_used",
			ap4Timestamp: utils.NewUint64(0),
			header: customtypes.WithHeaderExtra(
				&types.Header{
					BaseFee: big.NewInt(1),
				},
				&customtypes.HeaderExtra{
					BlockGasCost: big.NewInt(1),
				},
			),
			wantErr: errExtDataGasUsedNil,
		},
		{
			name:         "no_gas_used",
			ap4Timestamp: utils.NewUint64(0),
			header: customtypes.WithHeaderExtra(
				&types.Header{
					GasUsed: 0,
					BaseFee: big.NewInt(1),
				},
				&customtypes.HeaderExtra{
					ExtDataGasUsed: big.NewInt(0),
					BlockGasCost:   big.NewInt(1),
				},
			),
			wantErr: errNoGasUsed,
		},
		{
			name:         "success",
			ap4Timestamp: utils.NewUint64(0),
			header: customtypes.WithHeaderExtra(
				&types.Header{
					GasUsed: 123,
					BaseFee: big.NewInt(456),
				},
				&customtypes.HeaderExtra{
					ExtDataGasUsed: big.NewInt(789),
					BlockGasCost:   big.NewInt(101112),
				},
			),
			// totalGasUsed = GasUsed + ExtDataGasUsed
			// totalRequiredTips = BlockGasCost * BaseFee
			// estimatedTip = totalRequiredTips / totalGasUsed
			want: big.NewInt((101112 * 456) / (123 + 789)),
		},
		{
			name:         "success_rounds_up",
			ap4Timestamp: utils.NewUint64(0),
			header: customtypes.WithHeaderExtra(
				&types.Header{
					GasUsed: 124,
					BaseFee: big.NewInt(456),
				},
				&customtypes.HeaderExtra{
					ExtDataGasUsed: big.NewInt(789),
					BlockGasCost:   big.NewInt(101112),
				},
			),
			// totalGasUsed = GasUsed + ExtDataGasUsed
			// totalRequiredTips = BlockGasCost * BaseFee
			// estimatedTip = totalRequiredTips / totalGasUsed
			want: big.NewInt((101112*456)/(124+789) + 1), // +1 to round up
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			config := &extras.ChainConfig{
				NetworkUpgrades: extras.NetworkUpgrades{
					ApricotPhase4BlockTimestamp: test.ap4Timestamp,
				},
			}
			requiredTip, err := EstimateRequiredTip(config, test.header)
			require.ErrorIs(err, test.wantErr)
			require.Equal(test.want, requiredTip)
		})
	}
}

func TestVerifyBlockFee(t *testing.T) {
	testAddr := common.HexToAddress("7ef5a6135f1fd6a02593eedc869c6d41d934aef8")
	tests := map[string]struct {
		baseFee                *big.Int
		parentBlockGasCost     *big.Int
		timeElapsed            uint64
		txs                    []*types.Transaction
		receipts               []*types.Receipt
		extraStateContribution *big.Int
		expectedErr            error
	}{
		"tx only base fee": {
			baseFee:            big.NewInt(100),
			parentBlockGasCost: big.NewInt(0),
			timeElapsed:        0,
			txs: []*types.Transaction{
				types.NewTransaction(0, testAddr, big.NewInt(0), 100, big.NewInt(100), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 1000},
			},
			extraStateContribution: nil,
			expectedErr:            ErrInsufficientBlockGas,
		},
		"tx covers exactly block fee": {
			baseFee:            big.NewInt(100),
			parentBlockGasCost: big.NewInt(0),
			timeElapsed:        0,
			txs: []*types.Transaction{
				types.NewTransaction(0, testAddr, big.NewInt(0), 100_000, big.NewInt(200), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 100_000},
			},
			extraStateContribution: nil,
		},
		"txs share block fee": {
			baseFee:            big.NewInt(100),
			parentBlockGasCost: big.NewInt(0),
			timeElapsed:        0,
			txs: []*types.Transaction{
				types.NewTransaction(0, testAddr, big.NewInt(0), 100_000, big.NewInt(200), nil),
				types.NewTransaction(1, testAddr, big.NewInt(0), 100_000, big.NewInt(100), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 100_000},
				{GasUsed: 100_000},
			},
			extraStateContribution: nil,
		},
		"txs split block fee": {
			baseFee:            big.NewInt(100),
			parentBlockGasCost: big.NewInt(0),
			timeElapsed:        0,
			txs: []*types.Transaction{
				types.NewTransaction(0, testAddr, big.NewInt(0), 100_000, big.NewInt(150), nil),
				types.NewTransaction(1, testAddr, big.NewInt(0), 100_000, big.NewInt(150), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 100_000},
				{GasUsed: 100_000},
			},
			extraStateContribution: nil,
		},
		"split block fee with extra state contribution": {
			baseFee:            big.NewInt(100),
			parentBlockGasCost: big.NewInt(0),
			timeElapsed:        0,
			txs: []*types.Transaction{
				types.NewTransaction(0, testAddr, big.NewInt(0), 100_000, big.NewInt(150), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 100_000},
			},
			extraStateContribution: big.NewInt(5_000_000),
		},
		"extra state contribution insufficient": {
			baseFee:                big.NewInt(100),
			parentBlockGasCost:     big.NewInt(0),
			timeElapsed:            0,
			txs:                    nil,
			receipts:               nil,
			extraStateContribution: big.NewInt(9_999_999),
			expectedErr:            ErrInsufficientBlockGas,
		},
		"negative extra state contribution": {
			baseFee:                big.NewInt(100),
			parentBlockGasCost:     big.NewInt(0),
			timeElapsed:            0,
			txs:                    nil,
			receipts:               nil,
			extraStateContribution: big.NewInt(-1),
			expectedErr:            errInvalidExtraStateChangeContribution,
		},
		"extra state contribution covers block fee": {
			baseFee:                big.NewInt(100),
			parentBlockGasCost:     big.NewInt(0),
			timeElapsed:            0,
			txs:                    nil,
			receipts:               nil,
			extraStateContribution: big.NewInt(10_000_000),
		},
		"extra state contribution covers more than block fee": {
			baseFee:                big.NewInt(100),
			parentBlockGasCost:     big.NewInt(0),
			timeElapsed:            0,
			txs:                    nil,
			receipts:               nil,
			extraStateContribution: big.NewInt(10_000_001),
		},
		"tx only base fee after full time window": {
			baseFee:            big.NewInt(100),
			parentBlockGasCost: big.NewInt(500_000),
			timeElapsed:        ap4.TargetBlockRate + 10,
			txs: []*types.Transaction{
				types.NewTransaction(0, testAddr, big.NewInt(0), 100, big.NewInt(100), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 1000},
			},
			extraStateContribution: nil,
		},
		"tx only base fee after large time window": {
			baseFee:            big.NewInt(100),
			parentBlockGasCost: big.NewInt(100_000),
			timeElapsed:        math.MaxUint64,
			txs: []*types.Transaction{
				types.NewTransaction(0, testAddr, big.NewInt(0), 100, big.NewInt(100), nil),
			},
			receipts: []*types.Receipt{
				{GasUsed: 1000},
			},
			extraStateContribution: nil,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			blockGasCost := BlockGasCostWithStep(
				test.parentBlockGasCost,
				ap4.BlockGasCostStep,
				test.timeElapsed,
			)
			bigBlockGasCost := new(big.Int).SetUint64(blockGasCost)

			err := VerifyBlockFee(test.baseFee, bigBlockGasCost, test.txs, test.receipts, test.extraStateContribution)
			require.ErrorIs(t, err, test.expectedErr)
		})
	}
}
