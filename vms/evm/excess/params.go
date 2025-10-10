// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package excess

import (
	"sort"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// Params contains the parameters needed for excess calculations.
type Params struct {
	MinValue       uint64 // Minimum value
	ConversionRate uint64 // Conversion factor for exponential calculations
	MaxExcessDiff  uint64 // Maximum change in excess per update
	MaxExcess      uint64 // Maximum possible excess value
}

// AdjustExcess calculates the optimal new excess given the current and desired excess values,
// ensuring the change does not exceed the maximum excess difference.
func (p Params) AdjustExcess(excess, desired uint64) uint64 {
	change := safemath.AbsDiff(excess, desired)
	change = min(change, p.MaxExcessDiff)
	if excess < desired {
		return excess + change
	}
	return excess - change
}

// DesiredExcess calculates the optimal desiredExcess given the
// desired value using binary search.
func (p Params) DesiredExcess(desiredValue uint64) uint64 {
	// This could be solved directly by calculating ConversionRate * ln(desiredValue / MinValue)
	// using floating point math. However, it introduces inaccuracies. So, we
	// use a binary search to find the closest integer solution.
	return uint64(sort.Search(int(p.MaxExcess), func(targetExcessGuess int) bool {
		calculatedValue := p.CalculateValue(uint64(targetExcessGuess))
		return calculatedValue >= desiredValue
	}))
}

// CalculateValue calculates the value using exponential formula:
// Value = MinValue * e^(Excess / ConversionRate)
func (p Params) CalculateValue(excess uint64) uint64 {
	return safemath.ApproximateExponential(
		p.MinValue,
		excess,
		p.ConversionRate,
	)
}
