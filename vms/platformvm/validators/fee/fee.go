// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"math"

	"github.com/ava-labs/avalanchego/vms/components/gas"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// Config contains all the static parameters of the dynamic fee mechanism.
type Config struct {
	Target                   gas.Gas   `json:"target"`
	MinPrice                 gas.Price `json:"minPrice"`
	ExcessConversionConstant gas.Gas   `json:"excessConversionConstant"`
}

// State represents the current on-chain values used in the dynamic fee
// mechanism.
type State struct {
	Current gas.Gas `json:"current"`
	Excess  gas.Gas `json:"excess"`
}

// AdvanceTime adds (s.Current - target) * seconds to Excess.
//
// If Excess would underflow, it is set to 0.
// If Excess would overflow, it is set to MaxUint64.
func (s State) AdvanceTime(target gas.Gas, seconds uint64) State {
	excess := s.Excess
	if s.Current < target {
		excess = excess.SubPerSecond(target-s.Current, seconds)
	} else if s.Current > target {
		excess = excess.AddPerSecond(s.Current-target, seconds)
	}
	return State{
		Current: s.Current,
		Excess:  excess,
	}
}

// CostOf calculates how much to charge based on the dynamic fee mechanism for
// [seconds].
func (s State) CostOf(c Config, seconds uint64) uint64 {
	// If the current and target are the same, the price is constant.
	if s.Current == c.Target {
		price := gas.CalculatePrice(c.MinPrice, s.Excess, c.ExcessConversionConstant)
		cost, err := safemath.Mul(seconds, uint64(price))
		if err != nil {
			return math.MaxUint64
		}
		return cost
	}

	var (
		cost uint64
		err  error
	)
	for i := uint64(0); i < seconds; i++ {
		s = s.AdvanceTime(c.Target, 1)

		// Advancing the time is going to either hold excess constant,
		// monotonically increase it, or monotonically decrease it. If it is
		// equal to 0 after performing one of these operations, it is guaranteed
		// to always remain 0.
		if s.Excess == 0 {
			secondsWithZeroExcess := seconds - i
			zeroExcessCost, err := safemath.Mul(uint64(c.MinPrice), secondsWithZeroExcess)
			if err != nil {
				return math.MaxUint64
			}

			cost, err = safemath.Add(cost, zeroExcessCost)
			if err != nil {
				return math.MaxUint64
			}
			return cost
		}

		price := gas.CalculatePrice(c.MinPrice, s.Excess, c.ExcessConversionConstant)
		cost, err = safemath.Add(cost, uint64(price))
		if err != nil {
			return math.MaxUint64
		}
	}
	return cost
}

// SecondsUntil calculates the number of seconds that it would take to charge at
// least [targetCost] based on the dynamic fee mechanism. The result is capped
// at [maxSeconds].
func (s State) SecondsUntil(c Config, maxSeconds uint64, targetCost uint64) uint64 {
	// Because this function can divide by prices, we need to sanity check the
	// parameters to avoid division by 0.
	if c.MinPrice == 0 {
		if targetCost == 0 {
			return 0
		}
		return maxSeconds
	}

	// If the current and target are the same, the price is constant.
	if s.Current == c.Target {
		price := gas.CalculatePrice(c.MinPrice, s.Excess, c.ExcessConversionConstant)
		return secondsUntil(
			uint64(price),
			maxSeconds,
			targetCost,
		)
	}

	var (
		cost    uint64
		seconds uint64
		err     error
	)
	for cost < targetCost && seconds < maxSeconds {
		s = s.AdvanceTime(c.Target, 1)

		// Advancing the time is going to either hold excess constant,
		// monotonically increase it, or monotonically decrease it. If it is
		// equal to 0 after performing one of these operations, it is guaranteed
		// to always remain 0.
		if s.Excess == 0 {
			zeroExcessCost := targetCost - cost
			secondsWithZeroExcess := secondsUntil(
				uint64(c.MinPrice),
				maxSeconds,
				zeroExcessCost,
			)

			totalSeconds, err := safemath.Add(seconds, secondsWithZeroExcess)
			if err != nil || totalSeconds >= maxSeconds {
				return maxSeconds
			}
			return totalSeconds
		}

		seconds++
		price := gas.CalculatePrice(c.MinPrice, s.Excess, c.ExcessConversionConstant)
		cost, err = safemath.Add(cost, uint64(price))
		if err != nil {
			return seconds
		}
	}
	return seconds
}

// Calculate the number of seconds that it would take to charge at least [cost]
// at [price] every second. The result is capped at [maxSeconds].
func secondsUntil(price uint64, maxSeconds uint64, cost uint64) uint64 {
	// Directly rounding up could cause an overflow. Instead we round down and
	// then check if we should have rounded up.
	secondsRoundedDown := cost / price
	if secondsRoundedDown >= maxSeconds {
		return maxSeconds
	}
	if cost%price == 0 {
		return secondsRoundedDown
	}
	return secondsRoundedDown + 1
}
