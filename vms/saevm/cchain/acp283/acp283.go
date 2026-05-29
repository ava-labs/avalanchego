// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package acp283

import (
	"math"

	"github.com/ava-labs/avalanchego/vms/components/gas"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	// minPriceWei (M) is the minimum gas price in wei.
	minPriceWei = 1

	// conversionRate (D) is the conversion factor for the exponential price
	// curve. It is fixed at MaxUint64 / ln(MaxUint64) so the full uint64 excess
	// range maps onto the full uint64 price range, removing the need for a
	// price cap.
	conversionRate = 415_828_534_307_635_077

	// maxPriceExcessDiff (Q) is the maximum change in excess per block. It is
	// conversionRate * ln(2) / MinBlocksToDouble, where MinBlocksToDouble = 3634
	// is the fewest blocks over which the price can double or halve (the single
	// tunable parameter, ~1h to double at 1s blocks).
	maxPriceExcessDiff PriceExcess = 79_314_908_132_007

	// InitialPriceExcess is the C-chain's initial price excess. The minimum
	// price is 1 wei, so the excess starts at 0.
	InitialPriceExcess PriceExcess = 0

	// maxExponent is the largest excess DesiredPriceExcess can return: the price
	// saturates at MaxUint64 once the excess reaches MaxUint64 - 37.
	maxExponent = math.MaxUint64 - 37
)

// PriceExcess represents the excess for price calculation in the dynamic
// minimum gas price mechanism.
type PriceExcess uint64

// Price returns the minimum gas price in wei, `q`.
//
// Price = minPriceWei * e^(p / conversionRate)
func (p PriceExcess) Price() gas.Price {
	return gas.CalculatePrice(minPriceWei, gas.Gas(p), conversionRate)
}

// Toward moves the PriceExcess as close as possible to desired without
// exceeding the maximum PriceExcess change.
func (p *PriceExcess) Toward(desired PriceExcess) {
	*p = calculatePriceExcess(*p, desired)
}

// DesiredPriceExcess returns the smallest excess whose Price is at least
// desiredPrice.
func DesiredPriceExcess(desiredPrice gas.Price) PriceExcess {
	// Binary search the excess range. sort.Search is unusable because both its
	// bound and result are int: the answer can exceed MaxInt64 (e.g. a 100 nAVAX
	// floor needs excess > 1e19).
	lo, hi := PriceExcess(0), PriceExcess(maxExponent)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if mid.Price() >= desiredPrice {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo
}

// calculatePriceExcess calculates the optimal new PriceExcess for a block
// proposer to include given the current and desired excess values.
//
// TODO(https://github.com/ava-labs/avalanchego/issues/5438): consolidate this
// with the ACP-176 and ACP-226 integrators into a shared package.
func calculatePriceExcess(excess, desired PriceExcess) PriceExcess {
	change := min(safemath.AbsDiff(excess, desired), maxPriceExcessDiff)
	if excess < desired {
		return excess + change
	}
	return excess - change
}
