// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ap4

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBlockGasCost(t *testing.T) {
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
			want:        500 + 100*TargetBlockRate,
		},
		{
			name:        "timeElapsed_at_target",
			parentCost:  3,
			step:        100,
			timeElapsed: TargetBlockRate,
			want:        3,
		},
		{
			name:        "timeElapsed_over_target",
			parentCost:  500,
			step:        100,
			timeElapsed: 2 * TargetBlockRate,
			want:        500 - 100*TargetBlockRate,
		},
		{
			name:        "change_overflow",
			parentCost:  500,
			step:        math.MaxUint64,
			timeElapsed: 0,
			want:        MaxBlockGasCost,
		},
		{
			name:        "cost_overflow",
			parentCost:  math.MaxUint64,
			step:        1,
			timeElapsed: 0,
			want:        MaxBlockGasCost,
		},
		{
			name:        "clamp_to_max",
			parentCost:  MaxBlockGasCost,
			step:        100,
			timeElapsed: TargetBlockRate - 1,
			want:        MaxBlockGasCost,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(
				t,
				test.want,
				BlockGasCost(
					test.parentCost,
					test.step,
					test.timeElapsed,
				),
			)
		})
	}
}
