// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// ACP-226 implements the dynamic minimum block delay mechanism specified here:
// https://github.com/avalanche-foundation/ACPs/blob/main/ACPs/226-dynamic-minimum-block-times/README.md
package acp226

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/avalanchego/vms/components/gas"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	// MinTargetDelayMilliseconds (M) is the minimum target block delay in milliseconds
	MinTargetDelayMilliseconds = 1 // ms
	// TargetConversion (D) is the conversion factor for exponential calculations
	TargetConversion = 1 << 20
	// MaxTargetDelayExcessDiff (Q) is the maximum change in target excess per update
	MaxTargetDelayExcessDiff = 200

	TargetDelayExcessBytesSize = wrappers.LongLen

	maxTargetDelayExcess = 46_516_320 // TargetConversion * ln(MaxUint64 / MinTargetDelayMilliseconds) + 1
)

var ErrTargetDelayExcessInsufficientLength = errors.New("insufficient length for block delay state")

// TargetDelayExcess represents the target excess for delay calculation in the dynamic minimum block delay mechanism.
type TargetDelayExcess uint64

// ParseTargetDelayExcess returns the target delay excess from the provided bytes. It is the inverse of
// [TargetDelayExcess.Bytes]. This function allows for additional bytes to be padded at the
// end of the provided bytes.
func ParseTargetDelayExcess(bytes []byte) (TargetDelayExcess, error) {
	if len(bytes) < TargetDelayExcessBytesSize {
		return 0, fmt.Errorf("%w: expected at least %d bytes but got %d bytes",
			ErrTargetDelayExcessInsufficientLength,
			TargetDelayExcessBytesSize,
			len(bytes),
		)
	}

	return TargetDelayExcess(binary.BigEndian.Uint64(bytes)), nil
}

// Bytes returns the binary representation of the target delay excess.
func (t TargetDelayExcess) Bytes() []byte {
	bytes := make([]byte, TargetDelayExcessBytesSize)
	binary.BigEndian.PutUint64(bytes, uint64(t))
	return bytes
}

// TargetDelay returns the target minimum block delay in milliseconds, `T`.
//
// TargetDelay = MinTargetDelayMilliseconds * e^(TargetDelayExcess / TargetConversion)
func (t TargetDelayExcess) TargetDelay() uint64 {
	return uint64(gas.CalculatePrice(
		MinTargetDelayMilliseconds,
		gas.Gas(t),
		TargetConversion,
	))
}

// UpdateTargetDelayExcess updates the targetDelayExcess to be as close as possible to the
// desiredTargetDelayExcess without exceeding the maximum targetDelayExcess change.
func (t *TargetDelayExcess) UpdateTargetDelayExcess(desiredTargetDelayExcess uint64) {
	*t = TargetDelayExcess(targetDelayExcess(uint64(*t), desiredTargetDelayExcess))
}

// DesiredTargetDelayExcess calculates the optimal desiredTargetDelayExcess given the
// desired target delay.
func DesiredTargetDelayExcess(desiredTargetDelayExcess uint64) uint64 {
	// This could be solved directly by calculating D * ln(desiredTarget / M)
	// using floating point math. However, it introduces inaccuracies. So, we
	// use a binary search to find the closest integer solution.
	return uint64(sort.Search(maxTargetDelayExcess, func(targetDelayExcessGuess int) bool {
		excess := TargetDelayExcess(targetDelayExcessGuess)
		return excess.TargetDelay() >= desiredTargetDelayExcess
	}))
}

// targetDelayExcess calculates the optimal new targetDelayExcess for a block proposer to
// include given the current and desired excess values.
func targetDelayExcess(excess, desired uint64) uint64 {
	change := safemath.AbsDiff(excess, desired)
	change = min(change, MaxTargetDelayExcessDiff)
	if excess < desired {
		return excess + change
	}
	return excess - change
}
