// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package common provides shared utility functions for ACP implementations.
package common

import (
	"math"
	"sort"

	safemath "github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/vms/components/gas"
)

// TargetExcessParams contains the parameters needed for target excess calculations.
type TargetExcessParams struct {
	MinTarget        uint64 // Minimum target value (P or M)
	TargetConversion uint64 // Conversion factor for exponential calculations (D)
	MaxExcessDiff    uint64 // Maximum change in excess per update (Q)
	MaxExcess        uint64 // Maximum possible excess value
}

// TargetExcess calculates the optimal new targetExcess for a block proposer to
// include given the current and desired excess values.
func (p TargetExcessParams) TargetExcess(excess, desired uint64) uint64 {
	change := safemath.AbsDiff(excess, desired)
	change = min(change, p.MaxExcessDiff)
	if excess < desired {
		return excess + change
	}
	return excess - change
}

// DesiredTargetExcess calculates the optimal desiredTargetExcess given the
// desired target using binary search.
func (p TargetExcessParams) DesiredTargetExcess(desiredTarget uint64) uint64 {
	// This could be solved directly by calculating D * ln(desiredTarget / P)
	// using floating point math. However, it introduces inaccuracies. So, we
	// use a binary search to find the closest integer solution.
	return uint64(sort.Search(int(p.MaxExcess), func(targetExcessGuess int) bool {
		calculatedTarget := p.CalculateTarget(uint64(targetExcessGuess))
		return calculatedTarget >= desiredTarget
	}))
}

// CalculateTarget calculates the target value using exponential formula:
// Target = MinTarget * e^(Excess / TargetConversion)
func (p TargetExcessParams) CalculateTarget(excess uint64) uint64 {
	return uint64(gas.CalculatePrice(
		gas.Price(p.MinTarget),
		gas.Gas(excess),
		gas.Gas(p.TargetConversion),
	))
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

// min returns the minimum of two uint64 values.
func min(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
