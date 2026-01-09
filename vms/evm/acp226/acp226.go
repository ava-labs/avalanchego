// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// ACP-226 implements the dynamic minimum block delay mechanism specified here:
// https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/226-dynamic-minimum-block-times/README.md
package acp226

import (
	"sort"

	"github.com/ava-labs/avalanchego/vms/components/gas"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	// MinDelayMilliseconds (M) is the minimum block delay in milliseconds
	MinDelayMilliseconds = 1 // ms
	// ConversionRate (D) is the conversion factor for exponential calculations
	ConversionRate = 1 << 20
	// MaxDelayExcessDiff (Q) is the maximum change in excess per update
	MaxDelayExcessDiff = 200

	// InitialDelayExcess represents the initial (≈2000ms) delay excess.
	// Formula: ConversionRate (2^20) * ln(2000) + 1
	InitialDelayExcess = 7_970_124

	maxDelayExcess = 46_516_320 // ConversionRate * ln(MaxUint64 / MinDelayMilliseconds) + 1
)

// DelayExcess represents the excess for delay calculation in the dynamic
// minimum block delay mechanism.
type DelayExcess uint64

// Delay returns the minimum block delay in milliseconds, `m`.
//
// Delay = MinDelayMilliseconds * e^(DelayExcess / ConversionRate)
func (t DelayExcess) Delay() uint64 {
	return uint64(gas.CalculatePrice(
		MinDelayMilliseconds,
		gas.Gas(t),
		ConversionRate,
	))
}

// UpdateDelayExcess updates the DelayExcess to be as close as possible to the
// desiredDelayExcess without exceeding the maximum DelayExcess change.
func (t *DelayExcess) UpdateDelayExcess(desiredDelayExcess DelayExcess) {
	*t = calculateDelayExcess(*t, desiredDelayExcess)
}

// DesiredDelayExcess calculates the optimal delay excess given the desired
// delay in milliseconds.
func DesiredDelayExcess(desiredDelay uint64) DelayExcess {
	// This could be solved directly by calculating D * ln(desired / M)
	// using floating point math. However, it introduces inaccuracies. So, we
	// use a binary search to find the closest integer solution.
	return DelayExcess(sort.Search(maxDelayExcess, func(delayExcessGuess int) bool {
		excess := DelayExcess(delayExcessGuess)
		return excess.Delay() >= desiredDelay
	}))
}

// calculateDelayExcess calculates the optimal new DelayExcess for a block
// proposer to include given the current and desired excess values.
func calculateDelayExcess(excess, desired DelayExcess) DelayExcess {
	change := safemath.AbsDiff(excess, desired)
	change = min(change, MaxDelayExcessDiff)
	if excess < desired {
		return excess + change
	}
	return excess - change
}
