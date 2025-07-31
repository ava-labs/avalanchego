// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"math"

	"github.com/ava-labs/avalanchego/vms/components/gas"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

// Config contains all the static parameters of the dynamic fee mechanism.
type Config struct {
	Capacity                 gas.Gas   `json:"capacity"`
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
// seconds.
//
// This implements the ACP-77 cost over time formula:
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

// SecondsRemaining calculates the maximum number of seconds that a validator
// can pay fees before their fundsRemaining would be exhausted based on the
// dynamic fee mechanism. The result is capped at maxSeconds.
func (s State) SecondsRemaining(c Config, maxSeconds uint64, fundsRemaining uint64) uint64 {
	// Because this function can divide by prices, we need to sanity check the
	// parameters to avoid division by 0.
	if c.MinPrice == 0 {
		return maxSeconds
	}

	// If the current and target are the same, the price is constant.
	if s.Current == c.Target {
		price := uint64(gas.CalculatePrice(c.MinPrice, s.Excess, c.ExcessConversionConstant))
		seconds := fundsRemaining / price
		return min(seconds, maxSeconds)
	}

	for seconds := uint64(0); seconds < maxSeconds; seconds++ {
		s = s.AdvanceTime(c.Target, 1)

		// Advancing the time is going to either hold excess constant,
		// monotonically increase it, or monotonically decrease it. If it is
		// equal to 0 after performing one of these operations, it is guaranteed
		// to always remain 0.
		if s.Excess == 0 {
			secondsWithZeroExcess := fundsRemaining / uint64(c.MinPrice)
			totalSeconds, err := safemath.Add(seconds, secondsWithZeroExcess)
			if err != nil {
				// This is technically unreachable, but makes the code more
				// clearly correct.
				return maxSeconds
			}
			return min(totalSeconds, maxSeconds)
		}

		price := uint64(gas.CalculatePrice(c.MinPrice, s.Excess, c.ExcessConversionConstant))
		if price > fundsRemaining {
			return seconds
		}
		fundsRemaining -= price
	}
	return maxSeconds
}
