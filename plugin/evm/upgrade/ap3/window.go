// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// AP3 defines the dynamic fee window used after the Apricot Phase 3 upgrade.
package ap3

import (
	"math"

	"github.com/ava-labs/coreth/utils"
	safemath "github.com/ethereum/go-ethereum/common/math"
)

const (
	// WindowLen is the number of seconds of gas consumption to track.
	WindowLen = 10

	// MinBaseFee is the minimum base fee that is allowed after Apricot Phase 3
	// upgrade.
	//
	// This value was modified in Apricot Phase 4.
	MinBaseFee = 75 * utils.GWei

	// MaxBaseFee is the maximum base fee that is allowed after Apricot Phase 3
	// upgrade.
	//
	// This value was modified in Apricot Phase 4.
	MaxBaseFee = 225 * utils.GWei

	// InitialBaseFee is the base fee that is used for the first Apricot Phase 3
	// block.
	InitialBaseFee = MaxBaseFee

	// TargetGas is the target amount of gas to be included in the window. The
	// target amount of gas per second equals [TargetGas] / [WindowLen].
	//
	// This value was modified in Apricot Phase 5.
	TargetGas = 10_000_000

	// IntrinsicBlockGas is the amount of gas that should always be included in
	// the window.
	//
	// This value became dynamic in Apricot Phase 4.
	IntrinsicBlockGas = 1_000_000

	// BaseFeeChangeDenominator is the denominator used to smoothen base fee
	// changes.
	//
	// This value was modified in Apricot Phase 5.
	BaseFeeChangeDenominator = 12
)

// Window is a window of the last [WindowLen] seconds of gas usage.
//
// Index 0 is the oldest entry, and [WindowLen]-1 is the current entry.
type Window [WindowLen]uint64

// Add adds the amounts to the most recent entry in the window.
//
// If the most recent entry overflows, it is set to [math.MaxUint64].
func (w *Window) Add(amounts ...uint64) {
	const lastIndex uint = WindowLen - 1
	w[lastIndex] = add(w[lastIndex], amounts...)
}

// Shift removes the oldest n entries from the window and adds n new empty
// entries.
func (w *Window) Shift(n uint64) {
	if n >= WindowLen {
		*w = Window{}
		return
	}

	var newWindow Window
	copy(newWindow[:], w[n:])
	*w = newWindow
}

// Sum returns the sum of all the entries in the window.
//
// If the sum overflows, [math.MaxUint64] is returned.
func (w *Window) Sum() uint64 {
	return add(0, w[:]...)
}

func add(sum uint64, values ...uint64) uint64 {
	var overflow bool
	for _, v := range values {
		sum, overflow = safemath.SafeAdd(sum, v)
		if overflow {
			return math.MaxUint64
		}
	}
	return sum
}
