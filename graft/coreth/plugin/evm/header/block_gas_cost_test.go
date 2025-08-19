// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"math/big"
	"testing"

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
