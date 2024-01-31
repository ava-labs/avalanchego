// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	"math"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const WindowSize = 10

type Window [WindowSize]uint64

// Roll rolls the uint64s consumed units within [consumptionWindow] over by [roll] places.
// For example, if there are 4 uint64 encoded in a 32 byte slice, rollWindow would
// have the following effect:
// Original:
// [1, 2, 3, 4]
// Roll = 0
// [1, 2, 3, 4]
// Roll = 1
// [2, 3, 4, 0]
// Roll = 2
// [3, 4, 0, 0]
// Roll = 3
// [4, 0, 0, 0]
// Roll >= 4
// [0, 0, 0, 0]
// Assumes that [roll] is greater than or equal to 0
func Roll(w Window, roll int) Window {
	// Note: make allocates a zeroed array, so we are guaranteed
	// that what we do not copy into, will be set to 0
	var res [WindowSize]uint64
	if roll > WindowSize {
		return res
	}
	copy(res[:], w[roll:])
	return res
}

// Sum sums the consumed units recorded in [window]. If an overflow occurs,
// while summing the contents, the maximum uint64 value is returned.
func Sum(w Window) uint64 {
	var (
		sum      uint64
		overflow error
	)
	for i := 0; i < WindowSize; i++ {
		// If an overflow occurs while summing the elements of the window, return the maximum
		// uint64 value immediately.
		sum, overflow = safemath.Add64(sum, w[i])
		if overflow != nil {
			return math.MaxUint64
		}
	}
	return sum
}

// Update adds [unitsConsumed] in at index within [window].
// Assumes that [index] has already been validated.
// If an overflow occurs, the maximum uint64 value is used.
func Update(w *Window, idx int, unitsConsumed uint64) {
	prevUnitsConsumed := w[idx]

	totalUnitsConsumed, overflow := safemath.Add64(prevUnitsConsumed, unitsConsumed)
	if overflow != nil {
		totalUnitsConsumed = math.MaxUint64
	}
	w[idx] = totalUnitsConsumed
}

func Last(w *Window) uint64 {
	return w[len(w)-1]
}

// TODO ABENEGIA: add Marshal/Unmarshal to bytes, or let the codec do that?
