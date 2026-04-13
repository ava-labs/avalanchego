// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gastime

import (
	"fmt"
	"math"

	"github.com/ava-labs/avalanchego/vms/components/gas"
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/vms/saevm/hook"
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
	// Although [Time.SetTarget] scales the excess by the same factor as the
	// change in target, it rounds when necessary, which might alter the price
	// by a negligible amount. We therefore take a price snapshot beforehand
	// otherwise we'd call [Time.findExcessForPrice] with a different value,
	// which makes it extremely hard to test.
	p := tm.Price()
	tm.SetTarget(target)

	cfg, err := newConfig(hookCfg)
	if err != nil {
		return fmt.Errorf("%T.newConfig() after block: %w", tm, err)
	}
	if cfg.equals(tm.config) {
		return nil
	}
	tm.config = cfg
	tm.excess = tm.findExcessForPrice(p)

	return nil
}

// findExcessForPrice uses binary search over uint64 to find the smallest excess
// value that produces targetPrice with the current [config]. This maintains
// price continuity under a change in [config], with the following scenarios:
//
//   - K changes (via TargetToExcessScaling): Scale excess to maintain current price
//   - StaticPricing is true: Set excess to 0, enabling fixed price mode
//   - M decreases: Scale excess to maintain current price
//   - M increases AND current price >= new M: Scale excess to maintain current price
//   - M increases AND current price < new M: Price bumps to new M (excess becomes 0)
func (tm *Time) findExcessForPrice(targetPrice gas.Price) gas.Gas {
	// We return 0 in case targetPrice < minPrice because we should at least maintain the minimum price
	// by setting the excess to 0. ( P = M * e^(0 / K) = M )
	// Note: Even though we return 0 for excess it won't avoid accumulating excess in the long run.
	if targetPrice <= tm.config.minPrice || tm.config.staticPricing {
		return 0
	}

	k := tm.excessScalingFactor()

	// The price function is monotonic non-decreasing so binary search is appropriate.
	lo, hi := gas.Gas(0), gas.Gas(math.MaxUint64)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if gas.CalculatePrice(tm.config.minPrice, mid, k) >= targetPrice {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo
}
