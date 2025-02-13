// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// AP4 implements the block gas cost logic activated by the Apricot Phase 4
// upgrade.
package ap4

import (
	"math"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	MinBlockGasCost = 0
	MaxBlockGasCost = 1_000_000
	TargetBlockRate = 2 // in seconds

	// BlockGasCostStep is the rate at which the block gas cost changes per
	// second.
	//
	// This value was modified by the Apricot Phase 5 upgrade.
	BlockGasCostStep = 50_000
)

// BlockGasCost calculates the required block gas cost.
//
// cost = parentCost + step * (TargetBlockRate - timeElapsed)
//
// The returned cost is clamped to [MinBlockGasCost, MaxBlockGasCost].
func BlockGasCost(
	parentCost uint64,
	step uint64,
	timeElapsed uint64,
) uint64 {
	deviation := safemath.AbsDiff(TargetBlockRate, timeElapsed)
	change, err := safemath.Mul(step, deviation)
	if err != nil {
		change = math.MaxUint64
	}

	var (
		op                 = safemath.Add[uint64]
		defaultCost uint64 = MaxBlockGasCost
	)
	if timeElapsed > TargetBlockRate {
		op = safemath.Sub
		defaultCost = MinBlockGasCost
	}

	cost, err := op(parentCost, change)
	if err != nil {
		cost = defaultCost
	}

	switch {
	case cost < MinBlockGasCost:
		// This is technically dead code because [MinBlockGasCost] is 0, but it
		// makes the code more clear.
		return MinBlockGasCost
	case cost > MaxBlockGasCost:
		return MaxBlockGasCost
	default:
		return cost
	}
}
