// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp283

import (
	"math"

	"github.com/ava-labs/avalanchego/vms/components/gas"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	// MinPriceWei (M) is the minimum gas price in wei.
	MinPriceWei = 1

	// ConversionRate (D) is the conversion factor for the exponential price
	// curve. It is fixed at MaxUint64 / ln(MaxUint64) so the full uint64 excess
	// range maps onto the full uint64 price range, removing the need for a
	// price cap.
	ConversionRate = 415_828_534_307_635_077

	// MinBlocksToDouble is the fewest blocks over which the price can double or
	// halve. It is the only tunable parameter of the mechanism.
	MinBlocksToDouble = 3634

	// MaxPriceExcessDiff (Q) is the maximum change in excess per block. It is
	// ConversionRate * ln(2) / MinBlocksToDouble.
	MaxPriceExcessDiff PriceExcess = 79_314_908_132_007

	// InitialPriceExcess is the C-chain's initial price excess. The minimum
	// price is 1 wei, so the excess starts at 0.
	InitialPriceExcess PriceExcess = 0
)

// PriceExcess represents the excess for price calculation in the dynamic
// minimum gas price mechanism.
type PriceExcess uint64

// Price returns the minimum gas price in wei, `q`.
//
// Price = MinPriceWei * e^(PriceExcess / ConversionRate)
func (p PriceExcess) Price() gas.Price {
	return gas.CalculatePrice(MinPriceWei, gas.Gas(p), ConversionRate)
}

// UpdatePriceExcess updates the PriceExcess to be as close as possible to the
// desiredPriceExcess without exceeding the maximum PriceExcess change.
func (p *PriceExcess) UpdatePriceExcess(desiredPriceExcess PriceExcess) {
	*p = calculatePriceExcess(*p, desiredPriceExcess)
}

// DesiredPriceExcess returns the smallest excess whose Price is at least
// desiredPrice.
func DesiredPriceExcess(desiredPrice gas.Price) PriceExcess {
	// Binary search the full uint64 range. sort.Search is unusable because both
	// its bound and result are int: the answer can exceed MaxInt64 (e.g. a
	// 100 nAVAX floor needs excess > 1e19).
	lo, hi := uint64(0), uint64(math.MaxUint64)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if PriceExcess(mid).Price() >= desiredPrice {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return PriceExcess(lo)
}

// calculatePriceExcess calculates the optimal new PriceExcess for a block
// proposer to include given the current and desired excess values.
func calculatePriceExcess(excess, desired PriceExcess) PriceExcess {
	change := min(safemath.AbsDiff(excess, desired), MaxPriceExcessDiff)
	if excess < desired {
		return excess + change
	}
	return excess - change
}
