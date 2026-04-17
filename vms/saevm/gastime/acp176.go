// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gastime

import (
	"fmt"
	"math"
	"time"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/intmath"
)

// BeforeBlock is intended to be called before processing a block with the
// provided time. The gastime is advanced to be no earlier than the block time.
func (tm *Time) BeforeBlock(bTime time.Time) {
	s, ns := bTime.Unix(), bTime.Nanosecond()
	// g = ceil(ns * rate / time.Second)
	g, _, err := intmath.MulDivCeil(
		gas.Gas(ns), //#nosec G115 -- ns is in [0, time.Second)
		tm.Rate(),
		gas.Gas(time.Second),
	)
	if err != nil {
		// [time.Time.Nanosecond] is documented as only returning values in the
		// range [0, time.Second). So either Nanosecond returned an incorrect
		// value, or [intmath.MulDivCeil] incorrectly returned an error.
		// Regardless, this failure MUST be detected in tests, hence not just
		// dropping the error.
		panic(fmt.Sprintf("broken invariant: %v", err))
	}
	tm.FastForwardTo(uint64(s), g) //#nosec G115 -- known non-negative.
}

// AfterBlock is intended to be called after processing a block, with the
// target and gas configuration provided.
func (tm *Time) AfterBlock(used gas.Gas, target gas.Gas, c GasPriceConfig) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("%T.Validate() after block: %w", c, err)
	}

	tm.Tick(used)

	// x := x * (K' * T') / (K * T)
	oldK := tm.excessScalingFactor() // K * T

	tm.target = clampTarget(target)
	tm.Time.SetRate(rateOf(tm.target))
	tm.config = c

	newK := tm.excessScalingFactor() // K' * T'
	scaled, _, err := intmath.MulDivCeil(tm.excess, newK, oldK)
	if err != nil {
		scaled = math.MaxUint64
	}
	tm.excess = scaled

	if c.StaticPricing {
		tm.excess = 0
	}

	// x := max(x, ln(minPrice) * K' * T')
	minExcess := priceExcess(c.MinPrice, newK)
	tm.excess = max(tm.excess, minExcess)
	return nil
}

// priceExcess returns the lowest excess that produces p, if one exists. If the
// integer approximation in [calculatePrice] skips over p, it returns the
// maximum excess where the price is less than p.
//
// Mathematically, it returns ln(p) * k.
func priceExcess(p gas.Price, k gas.Gas) gas.Gas {
	if p <= 1 {
		return 0
	}
	// Binary search for the minimum x where calculatePrice(x, k) >= minPrice.
	//
	// calculatePrice(0, k) == 1, and minPrice > 1, so lo > 0.
	lo, hi := gas.Gas(1), gas.Gas(math.MaxUint64)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if calculatePrice(mid, k) >= p {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	// If [calculatePrice] can't generate p, make sure to honor the lower price
	// expectation.
	if calculatePrice(lo, k) > p {
		return lo - 1
	}
	return lo
}

// calculatePrice returns an integer approximation of e^(x/k).
func calculatePrice(x, k gas.Gas) gas.Price {
	return gas.CalculatePrice(1, x, k)
}
