// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package common provides shared utility functions for ACP implementations.
package common

import (
	"math"
	"sort"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// ExcessParams contains the parameters needed for excess calculations.
type ExcessParams struct {
	MinValue       uint64 // Minimum value (P or M)
	ConversionRate uint64 // Conversion factor for exponential calculations (D)
	MaxExcessDiff  uint64 // Maximum change in excess per update (Q)
	MaxExcess      uint64 // Maximum possible excess value
}

// AdjustExcess calculates the optimal new excess given the current and desired excess values,
// ensuring the change does not exceed the maximum excess difference.
func (p ExcessParams) AdjustExcess(excess, desired uint64) uint64 {
	change := safemath.AbsDiff(excess, desired)
	change = min(change, p.MaxExcessDiff)
	if excess < desired {
		return excess + change
	}
	return excess - change
}

// DesiredExcess calculates the optimal desiredExcess given the
// desired value using binary search.
func (p ExcessParams) DesiredExcess(desiredValue uint64) uint64 {
	// This could be solved directly by calculating D * ln(desiredValue / P)
	// using floating point math. However, it introduces inaccuracies. So, we
	// use a binary search to find the closest integer solution.
	return uint64(sort.Search(int(p.MaxExcess), func(targetExcessGuess int) bool {
		calculatedValue := p.CalculateValue(uint64(targetExcessGuess))
		return calculatedValue >= desiredValue
	}))
}

// CalculateValue calculates the value using exponential formula:
// Value = MinValue * e^(Excess / ConversionRate)
func (p ExcessParams) CalculateValue(excess uint64) uint64 {
	return safemath.CalculateExponential(
		p.MinValue,
		excess,
		p.ConversionRate,
	)
}

// MulWithUpperBound multiplies two numbers and returns the result. If the
// result overflows, it returns [math.MaxUint64].
func MulWithUpperBound(a, b uint64) uint64 {
	product, err := safemath.Mul(a, b)
	if err != nil {
		return math.MaxUint64
	}
	return product
}
