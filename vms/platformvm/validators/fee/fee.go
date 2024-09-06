// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"math"

	"github.com/ava-labs/avalanchego/vms/components/gas"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// CalculateExcess updates the excess value based on the target and current
// usage over the provided duration.
func CalculateExcess(
	target gas.Gas,
	current gas.Gas,
	excess gas.Gas,
	duration uint64,
) gas.Gas {
	if current < target {
		return excess.SubPerSecond(target-current, duration)
	}
	return excess.AddPerSecond(current-target, duration)
}

// CalculateCost calculates the how much to charge based on the dynamic fee
// mechanism for [duration].
func CalculateCost(
	target gas.Gas,
	current gas.Gas,
	minPrice gas.Price,
	excessConversionConstant gas.Gas,
	excess gas.Gas,
	duration uint64,
) uint64 {
	// If the current and target are the same, the price is constant.
	if current == target {
		price := gas.CalculatePrice(minPrice, excess, excessConversionConstant)
		cost, err := safemath.Mul(duration, uint64(price))
		if err != nil {
			return math.MaxUint64
		}
		return cost
	}

	var (
		cost uint64
		err  error
	)
	for i := uint64(0); i < duration; i++ {
		excess = CalculateExcess(target, current, excess, 1)
		// If the excess is 0, the price will remain constant for all future
		// iterations.
		if excess == 0 {
			durationWithZeroExcess := duration - i
			zeroExcessCost, err := safemath.Mul(uint64(minPrice), durationWithZeroExcess)
			if err != nil {
				return math.MaxUint64
			}

			cost, err = safemath.Add(cost, zeroExcessCost)
			if err != nil {
				return math.MaxUint64
			}
			return cost
		}

		price := gas.CalculatePrice(minPrice, excess, excessConversionConstant)
		cost, err = safemath.Add(cost, uint64(price))
		if err != nil {
			return math.MaxUint64
		}
	}
	return cost
}

// CalculateDuration calculates the duration that it would take to charge at
// least [targetCost] based on the dynamic fee mechanism. The result is capped
// at [maxDuration].
func CalculateDuration(
	target gas.Gas,
	current gas.Gas,
	minPrice gas.Price,
	excessConversionConstant gas.Gas,
	excess gas.Gas,
	maxDuration uint64,
	targetCost uint64,
) uint64 {
	// Because this function can divide by prices, we need to sanity check the
	// parameters to avoid division by 0.
	if minPrice == 0 {
		if targetCost == 0 {
			return 0
		}
		return maxDuration
	}

	// If the current and target are the same, the price is constant.
	if current == target {
		price := gas.CalculatePrice(minPrice, excess, excessConversionConstant)
		return calculateDuration(
			uint64(price),
			targetCost,
			maxDuration,
		)
	}

	var (
		cost     uint64
		duration uint64
		err      error
	)
	for cost < targetCost && duration < maxDuration {
		excess = CalculateExcess(target, current, excess, 1)
		// If the excess is 0, the price will remain constant for all future
		// iterations.
		if excess == 0 {
			zeroExcessCost := targetCost - cost
			durationWithZeroExcess := calculateDuration(
				uint64(minPrice),
				zeroExcessCost,
				maxDuration,
			)

			duration, err = safemath.Add(duration, durationWithZeroExcess)
			if err != nil || duration >= maxDuration {
				return maxDuration
			}
			return duration
		}

		duration++
		price := gas.CalculatePrice(minPrice, excess, excessConversionConstant)
		cost, err = safemath.Add(cost, uint64(price))
		if err != nil {
			return duration
		}
	}
	return duration
}

// Calculate the duration that it would take to charge at least [cost] at
// [price] every second. The result is capped at [maxDuration].
func calculateDuration(
	price uint64,
	cost uint64,
	maxDuration uint64,
) uint64 {
	// We can't directly round up because of rounding errors. Instead we
	// round down and then check if we should have rounded up.
	durationRoundedDown := cost / price
	if durationRoundedDown >= maxDuration {
		return maxDuration
	}
	if cost%price == 0 {
		return durationRoundedDown
	}
	return durationRoundedDown + 1
}
