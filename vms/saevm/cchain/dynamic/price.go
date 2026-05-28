// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dynamic

import (
	"math"

	"github.com/ava-labs/avalanchego/vms/components/gas"
)

type PriceExponent uint64

// Price returns the gas price in aAVAX.
//
// Price = minimum * e^(p / conversionRate)
func (p PriceExponent) Price() gas.Price {
	const (
		minimum gas.Price = 1
		// conversionRate is set so that Price ranges from [1, MaxUint64] with
		// the highest possible granularity.
		conversionRate gas.Gas = 415_828_534_307_635_077 // MaxUint64 / ln(MaxUint64 / minimum)
	)
	return gas.CalculatePrice(
		minimum,
		gas.Gas(p),
		conversionRate,
	)
}

// Toward returns a new value where p is moved to be as close as possible to
// desired without changing by more than allowed.
//
// If desired is nil, p is returned unmodified.
func (p PriceExponent) Toward(desired *PriceExponent) PriceExponent {
	const (
		diffToDouble   = 288_230_376_151_711_744 // conversionRate * ln(2)
		blocksToDouble = 3_600
		maxDiff        = diffToDouble / blocksToDouble
	)
	return toward(p, desired, maxDiff)
}

// DesiredPriceExponent calculates the optimal price exponent given the desired
// gas price.
func DesiredPriceExponent(desired gas.Price) PriceExponent {
	// This could be solved with floating point math. However, it introduces
	// inaccuracies. So, we use a binary search to find the closest integer
	// solution.
	const maxExponent = math.MaxUint64 - 37 // conversionRate * ln(MaxUint64 / minimum) + 1
	return search(maxExponent, func(guess PriceExponent) bool {
		return guess.Price() >= desired
	})
}
