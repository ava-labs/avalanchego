// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamic

import "github.com/ava-labs/avalanchego/vms/components/gas"

// DelayExponent encodes the minimum block delay.
type DelayExponent uint64

// Delay returns the minimum block delay in milliseconds.
//
// Delay = minimum * e^(d / conversionRate)
func (d DelayExponent) Delay() uint64 {
	const (
		minimum        = 1 // milliseconds
		conversionRate = 1 << 20
	)
	return uint64(gas.CalculatePrice(
		minimum,
		gas.Gas(d),
		conversionRate,
	))
}

// Toward returns a new value where d is moved to be as close as possible to
// desired without changing by more than allowed.
//
// If desired is nil, d is returned unmodified.
func (d DelayExponent) Toward(desired *DelayExponent) DelayExponent {
	const maxDiff = 200
	return toward(d, desired, maxDiff)
}

// DesiredDelayExponent calculates the optimal delay exponent given the desired
// delay.
func DesiredDelayExponent(desired uint64) DelayExponent {
	// Binary search avoids the rounding error of a floating-point solution.
	const maxExponent = 46_516_320 // conversionRate * ln(MaxUint64 / minimum) + 1
	return search(maxExponent, func(guess DelayExponent) bool {
		return guess.Delay() >= desired
	})
}
