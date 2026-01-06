// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// AP4 implements the block gas cost logic activated by the Apricot Phase 4
// upgrade.
package ap4

import (
	"math"

	"github.com/ava-labs/avalanchego/graft/coreth/utils"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	// MinBlockGasCost is the minimum block gas cost.
	MinBlockGasCost = 0
	// MaxBlockGasCost is the maximum block gas cost. If the block gas cost
	// would exceed this value, the block gas cost is set to this value.
	MaxBlockGasCost = 1_000_000
	// TargetBlockRate is the target amount of time in seconds between blocks.
	// If blocks are produced faster than this rate, the block gas cost is
	// increased. If blocks are produced slower than this rate, the block gas
	// cost is decreased.
	TargetBlockRate = 2

	// BlockGasCostStep is the rate at which the block gas cost changes per
	// second.
	//
	// This value was modified by the Apricot Phase 5 upgrade.
	BlockGasCostStep = 50_000

	// MinBaseFee is the minimum base fee that is allowed after Apricot Phase 3
	// upgrade.
	//
	// This value modifies the previously used `ap3.MinBaseFee`.
	//
	// This value was modified in Etna.
	MinBaseFee = 25 * utils.GWei

	// MaxBaseFee is the maximum base fee that is allowed after Apricot Phase 3
	// upgrade.
	//
	// This value modifies the previously used `ap3.MaxBaseFee`.
	MaxBaseFee = 1_000 * utils.GWei
)

// BlockGasCost calculates the required block gas cost.
//
// cost = parentCost + step * ([TargetBlockRate] - timeElapsed)
//
// The returned cost is clamped to [[MinBlockGasCost], [MaxBlockGasCost]].
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
