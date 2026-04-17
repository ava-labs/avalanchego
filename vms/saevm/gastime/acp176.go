// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gastime

import (
	"fmt"
	"math"
	"time"

	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/intmath"
)

// BeforeBlock is intended to be called before processing a block with the
// provided time. The gastime is advanced to be no earlier than the block time.
func (tm *Time) BeforeBlock(t time.Time) {
	tm.FastForwardToTime(t)
}

// FastForwardToTime is equivalent to [Time.FastForwardTo] except that it
// accepts a [time.Time].
func (tm *Time) FastForwardToTime(t time.Time) {
	g, _, err := intmath.MulDivCeil(
		gas.Gas(t.Nanosecond()), //#nosec G115 -- ns is in [0, time.Second)
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
	tm.FastForwardTo(uint64(t.Unix()), g) //#nosec G115 -- known non-negative.
}

// AfterBlock is intended to be called after processing a block, with the
// target and gas configuration provided.
func (tm *Time) AfterBlock(used gas.Gas, target gas.Gas, c GasPriceConfig) error {
	if err := c.Validate(); err != nil {
		return fmt.Errorf("%T.Validate() after block: %w", c, err)
	}
	target = clampTarget(target)

	tm.Tick(used)

	tm.excess = scaleExcess(
		tm.excess,
		target, c.TargetToExcessScaling,
		tm.target, tm.config.TargetToExcessScaling,
	)
	if c.StaticPricing {
		tm.excess = 0
	}

	tm.target = target
	tm.Time.SetRate(rateOf(tm.target))
	tm.config = c

	minExcess := priceExcess(c.MinPrice, tm.excessScalingFactor())
	tm.excess = max(tm.excess, minExcess)
	return nil
}

// scaleExcess returns x * T' * K' / (T * K) rounded up and capped to
// [math.MaxUint64].
func scaleExcess(x, newT, newScale, oldT, oldScale gas.Gas) gas.Gas {
	var (
		newK uint256.Int // T' * K'
		v    uint256.Int
	)
	newK.SetUint64(uint64(newT))
	v.SetUint64(uint64(newScale))
	newK.Mul(&newK, &v)

	var oldK uint256.Int // T * K
	oldK.SetUint64(uint64(oldT))
	v.SetUint64(uint64(oldScale))
	oldK.Mul(&oldK, &v)

	v.SetUint64(uint64(x))
	v.Mul(&v, &newK)
	v.Add(&v, &oldK) // round up by adding oldK - 1
	v.SubUint64(&v, 1)
	v.Div(&v, &oldK)

	if !v.IsUint64() {
		return math.MaxUint64
	}
	return gas.Gas(v.Uint64())
}

// priceExcess returns an integer approximation of ln(p) * k.
//
// If [calculatePrice] can produce p, priceExcess returns the minimum excess to
// produce p. Otherwise, it returns the maximum excess to produce a number < p,
// which may happen due to overflow or integer approximation.
func priceExcess(p gas.Price, k gas.Gas) gas.Gas {
	if p <= 1 {
		return 0
	}
	// Binary search for the minimum x where calculatePrice(x, k) >= p.
	//
	// calculatePrice(0, k) == 1 and p > 1, so lo > 0.
	lo, hi := gas.Gas(1), gas.Gas(math.MaxUint64)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if calculatePrice(mid, k) >= p {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	// If [calculatePrice] can't generate p due to integer approximation, honor
	// the lower price expectation.
	if calculatePrice(lo, k) > p {
		return lo - 1
	}
	return lo
}

// calculatePrice returns an integer approximation of e^(x/k).
func calculatePrice(x, k gas.Gas) gas.Price {
	return gas.CalculatePrice(1, x, k)
}
