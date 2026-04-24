// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// ACP176 implements the fee logic specified here:
// https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/176-dynamic-evm-gas-limit-and-price-discovery-updates/README.md
package acp176

import (
	"sort"

	"github.com/ava-labs/avalanchego/vms/components/gas"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	MinTargetPerSecond  = 1_000_000 // P
	ConversionRate      = 1 << 25   // D
	MaxTargetExcessDiff = 1 << 15   // Q

	maxTargetExcess = 1_024_950_627 // ConversionRate * ln(MaxUint64 / MinTargetPerSecond) + 1

	TargetToExcessScaling = 87 // 87 ~= 60 / ln(2)
	MinPrice              = 1  // M
)

type TargetExcess uint64

// Target returns the target gas per second.
//
// Target = MinTargetPerSecond * e^(TargetExcess / ConversionRate)
func (t TargetExcess) Target() gas.Gas {
	return gas.Gas(gas.CalculatePrice(
		MinTargetPerSecond,
		gas.Gas(t),
		ConversionRate,
	))
}

// UpdateTargetExcess updates the TargetExcess to be as close as possible to the
// desiredTargetExcess without changing by more than [MaxTargetExcessDiff].
func (t *TargetExcess) UpdateTargetExcess(desiredTargetExcess TargetExcess) {
	*t = calculateTargetExcess(*t, desiredTargetExcess)
}

// calculateTargetExcess calculates the optimal new TargetExcess for a block
// proposer to include given the current and desired excess values.
func calculateTargetExcess(excess, desired TargetExcess) TargetExcess {
	change := safemath.AbsDiff(excess, desired)
	change = min(change, MaxTargetExcessDiff)
	if excess < desired {
		return excess + change
	}
	return excess - change
}

// DesiredTargetExcess calculates the optimal target excess given the desired
// target in gas.
func DesiredTargetExcess(desired gas.Gas) TargetExcess {
	// This could be solved directly by calculating D * ln(desired / P)
	// using floating point math. However, it introduces inaccuracies. So, we
	// use a binary search to find the closest integer solution.
	return TargetExcess(sort.Search(int(maxTargetExcess), func(targetExcessGuess int) bool {
		return TargetExcess(targetExcessGuess).Target() >= desired
	}))
}
