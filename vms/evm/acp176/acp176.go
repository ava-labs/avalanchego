// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// ACP176 implements the fee logic specified here:
// https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/176-dynamic-evm-gas-limit-and-price-discovery-updates/README.md
package acp176

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"

	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/gas"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	MinTargetPerSecond  = 1_000_000                                 // P
	TargetConversion    = MaxTargetChangeRate * MaxTargetExcessDiff // D
	MaxTargetExcessDiff = 1 << 15                                   // Q
	MinGasPrice         = 1                                         // M

	TimeToFillCapacity            = 5    // in seconds
	TargetToMax                   = 2    // multiplier to convert from target per second to max per second
	TargetToPriceUpdateConversion = 87   // 87 ~= 60 / ln(2) which makes the price double at most every ~60 seconds
	MaxTargetChangeRate           = 1024 // Controls the rate that the target can change per block.

	TargetToMaxCapacity = TargetToMax * TimeToFillCapacity
	MinMaxPerSecond     = MinTargetPerSecond * TargetToMax
	MinMaxCapacity      = MinMaxPerSecond * TimeToFillCapacity

	StateSize = 3 * wrappers.LongLen

	maxTargetExcess = 1_024_950_627 // TargetConversion * ln(MaxUint64 / MinTargetPerSecond) + 1
)

var ErrStateInsufficientLength = errors.New("insufficient length for fee state")

// State represents the current state of the gas pricing and constraints.
type State struct {
	Gas          gas.State
	TargetExcess gas.Gas // q
}

// ParseState returns the state from the provided bytes. It is the inverse of
// [State.Bytes]. This function allows for additional bytes to be padded at the
// end of the provided bytes.
func ParseState(bytes []byte) (State, error) {
	if len(bytes) < StateSize {
		return State{}, fmt.Errorf("%w: expected at least %d bytes but got %d bytes",
			ErrStateInsufficientLength,
			StateSize,
			len(bytes),
		)
	}

	return State{
		Gas: gas.State{
			Capacity: gas.Gas(binary.BigEndian.Uint64(bytes)),
			Excess:   gas.Gas(binary.BigEndian.Uint64(bytes[wrappers.LongLen:])),
		},
		TargetExcess: gas.Gas(binary.BigEndian.Uint64(bytes[2*wrappers.LongLen:])),
	}, nil
}

// Target returns the target gas consumed per second, `T`.
//
// Target = MinTargetPerSecond * e^(TargetExcess / TargetConversion)
func (s *State) Target() gas.Gas {
	return gas.Gas(gas.CalculatePrice(
		MinTargetPerSecond,
		s.TargetExcess,
		TargetConversion,
	))
}

// MaxCapacity returns the maximum possible accrued gas capacity, `C`.
func (s *State) MaxCapacity() gas.Gas {
	targetPerSecond := s.Target()
	return mulWithUpperBound(targetPerSecond, TargetToMaxCapacity)
}

// GasPrice returns the current required fee per gas.
//
// GasPrice = MinGasPrice * e^(Excess / (Target() * TargetToPriceUpdateConversion))
func (s *State) GasPrice() gas.Price {
	targetPerSecond := s.Target()
	priceUpdateConversion := mulWithUpperBound(targetPerSecond, TargetToPriceUpdateConversion) // K
	return gas.CalculatePrice(MinGasPrice, s.Gas.Excess, priceUpdateConversion)
}

// AdvanceTime increases the gas capacity and decreases the gas excess based on
// the elapsed seconds.
func (s *State) AdvanceTime(seconds uint64) {
	targetPerSecond := s.Target()
	maxPerSecond := mulWithUpperBound(targetPerSecond, TargetToMax)    // R
	maxCapacity := mulWithUpperBound(maxPerSecond, TimeToFillCapacity) // C
	s.Gas = s.Gas.AdvanceTime(
		maxCapacity,
		maxPerSecond,
		targetPerSecond,
		seconds,
	)
}

// ConsumeGas decreases the gas capacity and increases the gas excess by
// gasUsed + extraGasUsed. If the gas capacity is insufficient, an error is
// returned.
func (s *State) ConsumeGas(
	gasUsed uint64,
	extraGasUsed *big.Int,
) error {
	newGas, err := s.Gas.ConsumeGas(gas.Gas(gasUsed))
	if err != nil {
		return err
	}

	if extraGasUsed == nil {
		s.Gas = newGas
		return nil
	}
	if !extraGasUsed.IsUint64() {
		return fmt.Errorf("%w: extraGasUsed (%d) exceeds MaxUint64",
			gas.ErrInsufficientCapacity,
			extraGasUsed,
		)
	}
	newGas, err = newGas.ConsumeGas(gas.Gas(extraGasUsed.Uint64()))
	if err != nil {
		return err
	}

	s.Gas = newGas
	return nil
}

// UpdateTargetExcess updates the targetExcess to be as close as possible to the
// desiredTargetExcess without exceeding the maximum targetExcess change.
func (s *State) UpdateTargetExcess(desiredTargetExcess gas.Gas) {
	previousTargetPerSecond := s.Target()
	s.TargetExcess = targetExcess(s.TargetExcess, desiredTargetExcess)
	newTargetPerSecond := s.Target()
	s.Gas.Excess = scaleExcess(
		s.Gas.Excess,
		newTargetPerSecond,
		previousTargetPerSecond,
	)

	// Ensure the gas capacity does not exceed the maximum capacity.
	newMaxCapacity := mulWithUpperBound(newTargetPerSecond, TargetToMaxCapacity) // C
	s.Gas.Capacity = min(s.Gas.Capacity, newMaxCapacity)
}

// Bytes returns the binary representation of the state.
func (s *State) Bytes() []byte {
	bytes := make([]byte, StateSize)
	binary.BigEndian.PutUint64(bytes, uint64(s.Gas.Capacity))
	binary.BigEndian.PutUint64(bytes[wrappers.LongLen:], uint64(s.Gas.Excess))
	binary.BigEndian.PutUint64(bytes[2*wrappers.LongLen:], uint64(s.TargetExcess))
	return bytes
}

// DesiredTargetExcess calculates the optimal desiredTargetExcess given the
// desired target.
func DesiredTargetExcess(desiredTarget gas.Gas) gas.Gas {
	// This could be solved directly by calculating D * ln(desiredTarget / P)
	// using floating point math. However, it introduces inaccuracies. So, we
	// use a binary search to find the closest integer solution.
	return gas.Gas(sort.Search(maxTargetExcess, func(targetExcessGuess int) bool {
		state := State{
			TargetExcess: gas.Gas(targetExcessGuess),
		}
		return state.Target() >= desiredTarget
	}))
}

// targetExcess calculates the optimal new targetExcess for a block proposer to
// include given the current and desired excess values.
func targetExcess(excess, desired gas.Gas) gas.Gas {
	change := safemath.AbsDiff(excess, desired)
	change = min(change, MaxTargetExcessDiff)
	if excess < desired {
		return excess + change
	}
	return excess - change
}

// scaleExcess scales the excess during gas target modifications to keep the
// price constant.
func scaleExcess(
	excess,
	newTargetPerSecond,
	previousTargetPerSecond gas.Gas,
) gas.Gas {
	var bigExcess uint256.Int
	bigExcess.SetUint64(uint64(excess))

	var bigTarget uint256.Int
	bigTarget.SetUint64(uint64(newTargetPerSecond))
	bigExcess.Mul(&bigExcess, &bigTarget)

	bigTarget.SetUint64(uint64(previousTargetPerSecond))
	bigExcess.Div(&bigExcess, &bigTarget)
	return gas.Gas(bigExcess.Uint64())
}

// mulWithUpperBound multiplies two numbers and returns the result. If the
// result overflows, it returns [math.MaxUint64].
func mulWithUpperBound(a, b gas.Gas) gas.Gas {
	product, err := safemath.Mul(a, b)
	if err != nil {
		return math.MaxUint64
	}
	return product
}
