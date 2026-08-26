// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamic

import (
	"time"

	"github.com/ava-labs/avalanchego/vms/components/gas"
)

// DelayExponent encodes the minimum block delay.
//
// Implements ACP-226, specified here:
// https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/226-dynamic-minimum-block-times/README.md
type DelayExponent uint64

// InitialDelayExponent is the ACP-226 initial delay exponent. It is the
// smallest exponent whose [DelayExponent.Delay] decodes to the 2000ms initial
// minimum block delay, obtained by inverting the
// Delay = minimum·e^(exponent/conversionRate) formula (minimum = 1ms,
// conversionRate = 2²⁰) for a 2000ms target and rounding up so the floored
// decode reaches 2000ms exactly:
//
//	InitialDelayExponent = ⌊conversionRate·ln(2000/minimum)⌋ + 1
//	                     = ⌊2²⁰·ln(2000)⌋ + 1
const InitialDelayExponent DelayExponent = 7_970_124

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

// DelayDuration returns the minimum block delay ([DelayExponent.Delay], which
// is denominated in milliseconds) as a [time.Duration].
func (d DelayExponent) DelayDuration() time.Duration {
	return time.Duration(d.Delay()) * time.Millisecond //#nosec G115 -- Delay() returns ms values that fit in int64 for centuries
}

// Toward returns a new value where d is moved to be as close as possible to
// desired without changing by more than allowed.
//
// If desired is nil, d is returned unmodified.
func (d DelayExponent) Toward(desired *DelayExponent) DelayExponent {
	// Per ACP-226, the per-block exponent change is capped at 200.
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
