// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blockgascost

import (
	"math"
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/graft/subnet-evm/commontype"
)

func TestBlockGasCost(t *testing.T) {
	testFeeConfig := commontype.FeeConfig{
		MinBlockGasCost:  big.NewInt(0),
		MaxBlockGasCost:  big.NewInt(1_000_000),
		TargetBlockRate:  2,
		BlockGasCostStep: big.NewInt(50_000),
	}
	BlockGasCostTest(t, testFeeConfig)

	testFeeConfigDouble := commontype.FeeConfig{
		MinBlockGasCost:  big.NewInt(2),
		MaxBlockGasCost:  big.NewInt(2_000_000),
		TargetBlockRate:  4,
		BlockGasCostStep: big.NewInt(100_000),
	}
	BlockGasCostTest(t, testFeeConfigDouble)
}

func BlockGasCostTest(t *testing.T, testFeeConfig commontype.FeeConfig) {
	targetBlockRate := testFeeConfig.TargetBlockRate
	maxBlockGasCost := testFeeConfig.MaxBlockGasCost.Uint64()
	tests := []struct {
		name        string
		parentCost  uint64
		step        uint64
		timeElapsed uint64
		want        uint64
	}{
		{
			name:        "timeElapsed_under_target",
			parentCost:  500,
			step:        100,
			timeElapsed: 0,
			want:        500 + 100*targetBlockRate,
		},
		{
			name:        "timeElapsed_at_target",
			parentCost:  3,
			step:        100,
			timeElapsed: targetBlockRate,
			want:        3,
		},
		{
			name:        "timeElapsed_over_target",
			parentCost:  500,
			step:        100,
			timeElapsed: 2 * targetBlockRate,
			want:        500 - 100*targetBlockRate,
		},
		{
			name:        "change_overflow",
			parentCost:  500,
			step:        math.MaxUint64,
			timeElapsed: 0,
			want:        maxBlockGasCost,
		},
		{
			name:        "cost_overflow",
			parentCost:  math.MaxUint64,
			step:        1,
			timeElapsed: 0,
			want:        maxBlockGasCost,
		},
		{
			name:        "clamp_to_max",
			parentCost:  maxBlockGasCost,
			step:        100,
			timeElapsed: targetBlockRate - 1,
			want:        maxBlockGasCost,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(
				t,
				test.want,
				BlockGasCost(
					testFeeConfig,
					test.parentCost,
					test.step,
					test.timeElapsed,
				),
			)
		})
	}
}
