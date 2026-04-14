// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gastime

import (
	"fmt"
	"math"

	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/avalanchego/vms/saevm/hook"
	"github.com/ava-labs/avalanchego/vms/saevm/intmath"
)

// BeforeBlock is intended to be called before processing a block, with the
// timestamp sourced from [hook.Points] and [types.Header].
func (tm *Time) BeforeBlock(hooks hook.Points, h *types.Header) {
	tm.FastForwardTo(
		h.Time,
		SubSecond(hooks, h, tm.Rate()),
	)
}

// AfterBlock is intended to be called after processing a block, with the
// target and gas configuration sourced from [hook.Points] and [types.Header].
func (tm *Time) AfterBlock(used gas.Gas, hooks hook.Points, h *types.Header) error {
	tm.Tick(used)
	target, hookCfg := hooks.GasConfigAfter(h)
	c, err := newConfig(hookCfg)
	if err != nil {
		return fmt.Errorf("%T.newConfig() after block: %w", tm, err)
	}

	tm.setGasPriceConfig(target, c)
	return nil
}

func (tm *Time) setGasPriceConfig(target gas.Gas, c config) {
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

	if c.staticPricing {
		tm.excess = 0
	}

	// x := max(x, ln(minPrice) * K' * T')
	minExcess := minPriceExcess(c.minPrice, newK)
	tm.excess = max(tm.excess, minExcess)
}

// minPriceExcess returns the lowest excess that produces minPrice exactly, if
// one exists. If the integer approximation in [calculatePrice] skips over
// minPrice, it returns the maximum excess where the price is strictly less
// than minPrice.
//
// Mathematically, it is calculating: ln(minPrice) * k.
func minPriceExcess(minPrice gas.Price, k gas.Gas) gas.Gas {
	if minPrice <= 1 {
		return 0
	}
	// Binary search for the minimum x where calculatePrice(x, k) >= minPrice.
	//
	// calculatePrice(0, k) == 1, and minPrice > 1, so lo > 0.
	lo, hi := gas.Gas(1), gas.Gas(math.MaxUint64)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if calculatePrice(mid, k) >= minPrice {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	// Ensure [Time.Price] can return minPrice even if the approximation can't
	// represent it.
	if calculatePrice(lo, k) == minPrice {
		return lo
	}
	return lo - 1
}

func calculatePrice(x, k gas.Gas) gas.Price {
	return gas.CalculatePrice(1, x, k)
}
