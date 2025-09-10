// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gas

import (
	"errors"
	"fmt"
	"math"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var ErrInsufficientCapacity = errors.New("insufficient capacity")

type State struct {
	Capacity Gas `serialize:"true" json:"capacity"`
	Excess   Gas `serialize:"true" json:"excess"`
}

// AdvanceTime adds capacityRate to capacity and subtracts targetRate
// from excess over the provided duration.
//
// The units chosen for time must be consistent with the units chosen for
// capacityRate and targetRate.
//
// Capacity is capped at maxCapacity.
// Excess to be removed is capped at excess.
func (s State) AdvanceTime(
	maxCapacity Gas,
	capacityRate Gas,
	targetRate Gas,
	duration uint64,
) State {
	return State{
		Capacity: min(
			s.Capacity.AddOverTime(capacityRate, duration),
			maxCapacity,
		),
		Excess: s.Excess.SubOverTime(targetRate, duration),
	}
}

// ConsumeGas removes gas from capacity and adds gas to excess.
//
// If the capacity is insufficient, an error is returned.
// If the excess would overflow, it is capped at MaxUint64.
func (s State) ConsumeGas(gas Gas) (State, error) {
	newCapacity, err := safemath.Sub(uint64(s.Capacity), uint64(gas))
	if err != nil {
		return State{}, fmt.Errorf("%w: capacity (%d) < gas (%d)", ErrInsufficientCapacity, s.Capacity, gas)
	}

	newExcess, err := safemath.Add(uint64(s.Excess), uint64(gas))
	if err != nil {
		//nolint:nilerr // excess is capped at MaxUint64
		return State{
			Capacity: Gas(newCapacity),
			Excess:   math.MaxUint64,
		}, nil
	}

	return State{
		Capacity: Gas(newCapacity),
		Excess:   Gas(newExcess),
	}, nil
}
