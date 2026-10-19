// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package acp176 defines the fee state and constants of ACP-176, specified here:
// https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/176-dynamic-evm-gas-limit-and-price-discovery-updates/README.md
package acp176

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/gas"
)

const (
	MinTargetPerSecond = 1_000_000 // P
	MinGasPrice        = 1         // M

	TimeToFillCapacity = 5 // in seconds
	TargetToMax        = 2 // multiplier to convert from target per second to max per second

	MinMaxPerSecond = MinTargetPerSecond * TargetToMax
	MinMaxCapacity  = MinMaxPerSecond * TimeToFillCapacity

	StateSize = 3 * wrappers.LongLen
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

// Bytes returns the binary representation of the state.
func (s *State) Bytes() []byte {
	bytes := make([]byte, StateSize)
	binary.BigEndian.PutUint64(bytes, uint64(s.Gas.Capacity))
	binary.BigEndian.PutUint64(bytes[wrappers.LongLen:], uint64(s.Gas.Excess))
	binary.BigEndian.PutUint64(bytes[2*wrappers.LongLen:], uint64(s.TargetExcess))
	return bytes
}
