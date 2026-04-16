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

// BeforeBlock is intended to be called before processing a block, with the
// timestamp portions provided. `subSec` must be in [0, second).
func (tm *Time) BeforeBlock(bTime time.Time) {
	// g scales `subSec` by the rate provided relative to 1 second.
	//
	// `subSec` is in [0,second). The lower bound guarantees that the conversion to unsigned
	// [gas.Gas] is safe while the upper bound guarantees that the mul-div
	// result can't overflow so we don't have to check the error.
	sec, subSec := bTime.Unix(), bTime.Nanosecond()
	g, _, _ := intmath.MulDivCeil(
		gas.Gas(subSec), //#nosec G115 -- See above
		tm.Rate(),
		gas.Gas(time.Second),
	)
	tm.FastForwardTo(uint64(sec), g) //#nosec G115 -- won't overflow for a long time.
}

// AfterBlock is intended to be called after processing a block, with the
// target and gas configuration provided.
func (tm *Time) AfterBlock(used gas.Gas, target gas.Gas, cfg GasPriceConfig) error {
	tm.Tick(used)
	// Although [Time.SetTarget] scales the excess by the same factor as the
	// change in target, it rounds when necessary, which might alter the price
	// by a negligible amount. We therefore take a price snapshot beforehand
	// otherwise we'd call [Time.findExcessForPrice] with a different value,
	// which makes it extremely hard to test.
	p := tm.Price()
	tm.SetTarget(target)

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("%T.Validate() after block: %w", cfg, err)
	}
	if cfg.equals(tm.config) {
		return nil
	}
	tm.config = cfg
	tm.excess = tm.findExcessForPrice(p)

	return nil
}

// findExcessForPrice uses binary search over uint64 to find the smallest excess
// value that produces targetPrice with the current [GasPriceConfig]. This maintains
// price continuity under a change in [GasPriceConfig], with the following scenarios:
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
	if targetPrice <= tm.config.MinPrice || tm.config.StaticPricing {
		return 0
	}

	k := tm.excessScalingFactor()

	// The price function is monotonic non-decreasing so binary search is appropriate.
	lo, hi := gas.Gas(0), gas.Gas(math.MaxUint64)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if gas.CalculatePrice(tm.config.MinPrice, mid, k) >= targetPrice {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo
}
