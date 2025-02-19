// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package header

import (
	"math/big"
	"testing"

	"github.com/ava-labs/coreth/core/types"
	"github.com/ava-labs/coreth/params"
	"github.com/ava-labs/coreth/plugin/evm/ap4"
	"github.com/ava-labs/coreth/plugin/evm/ap5"
	"github.com/ava-labs/coreth/utils"
	"github.com/stretchr/testify/assert"
)

func TestBlockGasCost(t *testing.T) {
	tests := []struct {
		name                        string
		apricotPhase5BlockTimestamp *uint64
		parentTime                  uint64
		parentCost                  *big.Int
		timestamp                   uint64
		expected                    uint64
	}{
		{
			name:       "normal_ap4",
			parentTime: 10,
			parentCost: big.NewInt(ap4.MaxBlockGasCost),
			timestamp:  10 + ap4.TargetBlockRate + 1,
			expected:   ap4.MaxBlockGasCost - ap4.BlockGasCostStep,
		},
		{
			name:                        "normal_ap5",
			apricotPhase5BlockTimestamp: utils.NewUint64(0),
			parentTime:                  10,
			parentCost:                  big.NewInt(ap4.MaxBlockGasCost),
			timestamp:                   10 + ap4.TargetBlockRate + 1,
			expected:                    ap4.MaxBlockGasCost - ap5.BlockGasCostStep,
		},
		{
			name:       "negative_time_elapsed",
			parentTime: 10,
			parentCost: big.NewInt(ap4.MinBlockGasCost),
			timestamp:  9,
			expected:   ap4.MinBlockGasCost + ap4.BlockGasCostStep*ap4.TargetBlockRate,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := &params.ChainConfig{
				NetworkUpgrades: params.NetworkUpgrades{
					ApricotPhase5BlockTimestamp: test.apricotPhase5BlockTimestamp,
				},
			}
			parent := &types.Header{
				Time:         test.parentTime,
				BlockGasCost: test.parentCost,
			}

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
