// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"math/big"
	"testing"

	"github.com/ava-labs/subnet-evm/commontype"
	"github.com/ava-labs/subnet-evm/core/types"
	"github.com/ava-labs/subnet-evm/params"
	"github.com/ava-labs/subnet-evm/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	testFeeConfig = commontype.FeeConfig{
		MinBlockGasCost:          big.NewInt(0),
		MaxBlockGasCost:          big.NewInt(1_000_000),
		TargetBlockRate:          2,
		BlockGasCostStep:         big.NewInt(50_000),
		TargetGas:                big.NewInt(10_000_000),
		BaseFeeChangeDenominator: big.NewInt(12),
		MinBaseFee:               big.NewInt(25 * utils.GWei),
	}

	testFeeConfigDouble = commontype.FeeConfig{
		MinBlockGasCost:          big.NewInt(2),
		MaxBlockGasCost:          big.NewInt(2_000_000),
		TargetBlockRate:          4,
		BlockGasCostStep:         big.NewInt(100_000),
		TargetGas:                big.NewInt(20_000_000),
		BaseFeeChangeDenominator: big.NewInt(24),
		MinBaseFee:               big.NewInt(50 * utils.GWei),
	}
)

func TestBlockGasCost(t *testing.T) {
	t.Run("normal", func(t *testing.T) {
		BlockGasCostTest(t, testFeeConfig)
	})
	t.Run("double", func(t *testing.T) {
		BlockGasCostTest(t, testFeeConfigDouble)
	})
}

func BlockGasCostTest(t *testing.T, feeConfig commontype.FeeConfig) {
	maxBlockGasCostBig := feeConfig.MaxBlockGasCost
	maxBlockGasCost := feeConfig.MaxBlockGasCost.Uint64()
	blockGasCostStep := feeConfig.BlockGasCostStep.Uint64()
	minBlockGasCost := feeConfig.MinBlockGasCost.Uint64()
	targetBlockRate := feeConfig.TargetBlockRate

	tests := []struct {
		name       string
		parentTime uint64
		parentCost *big.Int
		timestamp  uint64
		expected   uint64
	}{
		{
			name:       "normal",
			parentTime: 10,
			parentCost: maxBlockGasCostBig,
			timestamp:  10 + targetBlockRate + 1,
			expected:   maxBlockGasCost - blockGasCostStep,
		},
		{
			name:       "negative_time_elapsed",
			parentTime: 10,
			parentCost: feeConfig.MinBlockGasCost,
			timestamp:  9,
			expected:   minBlockGasCost + blockGasCostStep*targetBlockRate,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parent := &types.Header{
				Time:         test.parentTime,
				BlockGasCost: test.parentCost,
			}

			assert.Equal(t, test.expected, BlockGasCost(
				feeConfig,
				parent,
				test.timestamp,
			))
		})
	}
}

func TestBlockGasCostWithStep(t *testing.T) {
	t.Run("normal", func(t *testing.T) {
		BlockGasCostWithStepTest(t, testFeeConfig)
	})
	t.Run("double", func(t *testing.T) {
		BlockGasCostWithStepTest(t, testFeeConfigDouble)
	})
}

func BlockGasCostWithStepTest(t *testing.T, feeConfig commontype.FeeConfig) {
	minBlockGasCost := feeConfig.MinBlockGasCost.Uint64()
	blockGasCostStep := feeConfig.BlockGasCostStep.Uint64()
	targetBlockRate := feeConfig.TargetBlockRate
	bigMaxBlockGasCost := feeConfig.MaxBlockGasCost
	maxBlockGasCost := bigMaxBlockGasCost.Uint64()
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
			expected:    minBlockGasCost,
		},
		{
			name:        "timeElapsed_0",
			parentCost:  big.NewInt(0),
			timeElapsed: 0,
			expected:    targetBlockRate * blockGasCostStep,
		},
		{
			name:        "timeElapsed_1",
			parentCost:  big.NewInt(0),
			timeElapsed: 1,
			expected:    (targetBlockRate - 1) * blockGasCostStep,
		},
		{
			name:        "timeElapsed_0_with_parentCost",
			parentCost:  big.NewInt(50_000),
			timeElapsed: 0,
			expected:    50_000 + targetBlockRate*blockGasCostStep,
		},
		{
			name:        "timeElapsed_0_with_max_parentCost",
			parentCost:  bigMaxBlockGasCost,
			timeElapsed: 0,
			expected:    maxBlockGasCost,
		},
		{
			name:        "timeElapsed_1_with_max_parentCost",
			parentCost:  bigMaxBlockGasCost,
			timeElapsed: 1,
			expected:    maxBlockGasCost,
		},
		{
			name:        "timeElapsed_at_target",
			parentCost:  big.NewInt(900_000),
			timeElapsed: targetBlockRate,
			expected:    900_000,
		},
		{
			name:        "timeElapsed_over_target_1",
			parentCost:  bigMaxBlockGasCost,
			timeElapsed: targetBlockRate + 1,
			expected:    maxBlockGasCost - blockGasCostStep,
		},
		{
			name:        "timeElapsed_over_target_10",
			parentCost:  bigMaxBlockGasCost,
			timeElapsed: targetBlockRate + 10,
			expected:    maxBlockGasCost - 10*blockGasCostStep,
		},
		{
			name:        "timeElapsed_over_target_15",
			parentCost:  bigMaxBlockGasCost,
			timeElapsed: targetBlockRate + 15,
			expected:    maxBlockGasCost - 15*blockGasCostStep,
		},
		{
			name:        "timeElapsed_large_clamped_to_0",
			parentCost:  bigMaxBlockGasCost,
			timeElapsed: targetBlockRate + 100,
			expected:    minBlockGasCost,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, BlockGasCostWithStep(
				feeConfig,
				test.parentCost,
				blockGasCostStep,
				test.timeElapsed,
			))
		})
	}
}

func TestEstimateRequiredTip(t *testing.T) {
	tests := []struct {
		name               string
		subnetEVMTimestamp *uint64
		header             *types.Header
		want               *big.Int
		wantErr            error
	}{
		{
			name:               "not_subnet_evm",
			subnetEVMTimestamp: utils.NewUint64(1),
			header:             &types.Header{},
		},
		{
			name:               "nil_base_fee",
			subnetEVMTimestamp: utils.NewUint64(0),
			header: &types.Header{
				BlockGasCost: big.NewInt(1),
			},
			wantErr: errBaseFeeNil,
		},
		{
			name:               "nil_block_gas_cost",
			subnetEVMTimestamp: utils.NewUint64(0),
			header: &types.Header{
				BaseFee: big.NewInt(1),
			},
			wantErr: errBlockGasCostNil,
		},
		{
			name:               "success",
			subnetEVMTimestamp: utils.NewUint64(0),
			header: &types.Header{
				GasUsed:      123,
				BaseFee:      big.NewInt(456),
				BlockGasCost: big.NewInt(101112),
			},
			// totalRequiredTips = BlockGasCost * BaseFee
			// estimatedTip = totalRequiredTips / GasUsed
			want: big.NewInt((101112 * 456) / (123)),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require := require.New(t)

			config := &params.ChainConfig{
				NetworkUpgrades: params.NetworkUpgrades{
					SubnetEVMTimestamp: test.subnetEVMTimestamp,
				},
			}
			requiredTip, err := EstimateRequiredTip(config, test.header)
			require.ErrorIs(err, test.wantErr)
			require.Equal(test.want, requiredTip)
		})
	}
}
