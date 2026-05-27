// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp283

import (
	"sort"

	"github.com/ava-labs/avalanchego/vms/components/gas"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	// MinPriceWei (M) is the minimum gas price in wei.
	MinPriceWei = 1
	// ConversionRate (D) is the conversion factor for exponential calculations.
	ConversionRate = 1 << 20
	// MaxPriceExcessDiff (Q) is the maximum change in excess per update.
	MaxPriceExcessDiff = 200
	// QMaxWei is the hard upper bound on the gas price in wei (100 nAVAX).
	QMaxWei gas.Price = 100_000_000_000

	// InitialPriceExcess represents the initial price excess (around 10^7 wei,
	// around 0.01 nAVAX).
	InitialPriceExcess PriceExcess = 16_901_049

	maxPriceExcess PriceExcess = 46_516_320 // ConversionRate * ln(MaxUint64 / MinPriceWei) + 1
)

// PriceExcess represents the excess for price calculation in the dynamic
// minimum gas price mechanism.
type PriceExcess uint64

// Price returns the minimum gas price in wei, `q`.
//
// Price = min(MinPriceWei * e^(PriceExcess / ConversionRate), QMaxWei)
func (p PriceExcess) Price() gas.Price {
	return min(
		gas.CalculatePrice(MinPriceWei, gas.Gas(p), ConversionRate),
		QMaxWei,
	)
}

// UpdatePriceExcess updates the PriceExcess to be as close as possible to the
// desiredPriceExcess without exceeding the maximum PriceExcess change.
func (p *PriceExcess) UpdatePriceExcess(desiredPriceExcess PriceExcess) {
	*p = calculatePriceExcess(*p, desiredPriceExcess)
}

// DesiredPriceExcess calculates the optimal price excess given the desired
// price in wei.
func DesiredPriceExcess(desiredPrice gas.Price) PriceExcess {
	return PriceExcess(sort.Search(int(maxPriceExcess), func(priceExcessGuess int) bool {
		excess := PriceExcess(priceExcessGuess)
		return excess.Price() >= desiredPrice
	}))
}

// calculatePriceExcess calculates the optimal new PriceExcess for a block
// proposer to include given the current and desired excess values.
func calculatePriceExcess(excess, desired PriceExcess) PriceExcess {
	change := safemath.AbsDiff(excess, desired)
	change = min(change, MaxPriceExcessDiff)
	if excess < desired {
		return excess + change
	}
	return excess - change
}
