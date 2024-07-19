// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"errors"
	"math"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var ErrInsufficientCapacity = errors.New("insufficient capacity")

type State struct {
	Capacity Gas
	Excess   Gas
}

func (s State) AdvanceTime(
	maxGasCapacity Gas,
	maxGasPerSecond Gas,
	targetGasPerSecond Gas,
	duration uint64,
) State {
	return State{
		Capacity: min(
			s.Capacity.AddPerSecond(maxGasPerSecond, duration),
			maxGasCapacity,
		),
		Excess: s.Excess.SubPerSecond(targetGasPerSecond, duration),
	}
}

func (s State) ConsumeGas(gas Gas) (State, error) {
	newCapacity, err := safemath.Sub(uint64(s.Capacity), uint64(gas))
	if err != nil {
		return State{}, ErrInsufficientCapacity
	}

	newExcess, err := safemath.Add(uint64(s.Excess), uint64(gas))
	if err != nil {
		//nolint:nilerr // excess is capped at math.MaxUint64
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
