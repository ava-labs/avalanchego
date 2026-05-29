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
	// curve. It is fixed at MaxUint64 / ln(MaxUint64) so the full uint64
	// exponent range maps onto the full uint64 price range, removing the need
	// for a price cap.
	conversionRate = 415_828_534_307_635_077

	// maxPriceExponentDiff (Q) is the maximum change in the exponent per block.
	// It is conversionRate * ln(2) / MinBlocksToDouble, where MinBlocksToDouble
	// = 3634 is the fewest blocks over which the price can double or halve (the
	// single tunable parameter, ~1h to double at 1s blocks).
	maxPriceExponentDiff PriceExponent = 79_314_908_132_007

	// InitialPriceExponent is the C-chain's initial price exponent. The minimum
	// price is 1 wei, so the exponent starts at 0.
	InitialPriceExponent PriceExponent = 0

	// maxExponent is the largest exponent DesiredPriceExponent can return: the
	// price saturates at MaxUint64 once the exponent reaches MaxUint64 - 37.
	maxExponent = math.MaxUint64 - 37
)

// PriceExponent represents the exponent in the dynamic minimum gas price curve.
type PriceExponent uint64

// Price returns the minimum gas price in wei, `q`.
//
// Price = minPriceWei * e^(p / conversionRate)
func (p PriceExponent) Price() gas.Price {
	return gas.CalculatePrice(minPriceWei, gas.Gas(p), conversionRate)
}

// Toward moves the PriceExponent as close as possible to desired without
// exceeding the maximum PriceExponent change.
func (p *PriceExponent) Toward(desired PriceExponent) {
	*p = calculatePriceExponent(*p, desired)
}

// DesiredPriceExponent returns the smallest exponent whose Price is at least
// desiredPrice.
func DesiredPriceExponent(desiredPrice gas.Price) PriceExponent {
	// Binary search the exponent range. sort.Search is unusable because both its
	// bound and result are int: the answer can exceed MaxInt64 (e.g. a 100 nAVAX
	// floor needs an exponent > 1e19).
	lo, hi := PriceExponent(0), PriceExponent(maxExponent)
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

// calculatePriceExponent calculates the optimal new PriceExponent for a block
// proposer to include given the current and desired exponent values.
//
// TODO(https://github.com/ava-labs/avalanchego/issues/5438): consolidate this
// with the ACP-176 and ACP-226 integrators into a shared package.
func calculatePriceExponent(exponent, desired PriceExponent) PriceExponent {
	change := min(safemath.AbsDiff(exponent, desired), maxPriceExponentDiff)
	if exponent < desired {
		return exponent + change
	}
	return exponent - change
}
