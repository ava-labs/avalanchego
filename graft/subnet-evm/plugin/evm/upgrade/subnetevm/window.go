// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// subnetevm defines the dynamic fee window used after subnetevm upgrade.
package subnetevm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"github.com/ava-labs/avalanchego/utils/wrappers"

	safemath "github.com/ava-labs/libevm/common/math"
)

const (
	// WindowLen is the number of seconds of gas consumption to track.
	WindowLen = 10

	// WindowSize is the number of bytes that are used to encode the window.
	WindowSize = wrappers.LongLen * WindowLen
)

var ErrWindowInsufficientLength = errors.New("insufficient length for window")

// Window is a window of the last [WindowLen] seconds of gas usage.
//
// Index 0 is the oldest entry, and [WindowLen]-1 is the current entry.
type Window [WindowLen]uint64

func ParseWindow(bytes []byte) (Window, error) {
	if len(bytes) < WindowSize {
		return Window{}, fmt.Errorf("%w: expected at least %d bytes but got %d bytes",
			ErrWindowInsufficientLength,
			WindowSize,
			len(bytes),
		)
	}

	var window Window
	for i := range window {
		offset := i * wrappers.LongLen
		window[i] = binary.BigEndian.Uint64(bytes[offset:])
	}
	return window, nil
}

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

func (w *Window) Bytes() []byte {
	bytes := make([]byte, WindowSize)
	for i, v := range w {
		offset := i * wrappers.LongLen
		binary.BigEndian.PutUint64(bytes[offset:], v)
	}
	return bytes
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
