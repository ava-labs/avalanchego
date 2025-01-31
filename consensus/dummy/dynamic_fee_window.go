// (c) 2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package dummy

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/wrappers"
	"github.com/ava-labs/coreth/params"
	"github.com/ethereum/go-ethereum/common/math"
)

var ErrDynamicFeeWindowInsufficientLength = errors.New("insufficient length for dynamic fee window")

// DynamicFeeWindow is a window of the last [params.RollupWindow] seconds of gas
// usage.
//
// Index 0 is the oldest entry, and [params.RollupWindow]-1 is the current
// entry.
type DynamicFeeWindow [params.RollupWindow]uint64

func ParseDynamicFeeWindow(bytes []byte) (DynamicFeeWindow, error) {
	if len(bytes) < params.DynamicFeeExtraDataSize {
		return DynamicFeeWindow{}, fmt.Errorf("%w: expected at least %d bytes but got %d bytes",
			ErrDynamicFeeWindowInsufficientLength,
			params.DynamicFeeExtraDataSize,
			len(bytes),
		)
	}

	var window DynamicFeeWindow
	for i := range window {
		offset := i * wrappers.LongLen
		window[i] = binary.BigEndian.Uint64(bytes[offset:])
	}
	return window, nil
}

// Add adds the amounts to the most recent entry in the window.
//
// If the most recent entry overflows, it is set to [math.MaxUint64].
func (w *DynamicFeeWindow) Add(amounts ...uint64) {
	const lastIndex uint = params.RollupWindow - 1
	w[lastIndex] = add(w[lastIndex], amounts...)
}

// Shift removes the oldest n entries from the window and adds n new empty
// entries.
func (w *DynamicFeeWindow) Shift(n uint64) {
	if n >= params.RollupWindow {
		*w = DynamicFeeWindow{}
		return
	}

	var newWindow DynamicFeeWindow
	copy(newWindow[:], w[n:])
	*w = newWindow
}

// Sum returns the sum of all the entries in the window.
//
// If the sum overflows, [math.MaxUint64] is returned.
func (w *DynamicFeeWindow) Sum() uint64 {
	return add(0, w[:]...)
}

func (w *DynamicFeeWindow) Bytes() []byte {
	bytes := make([]byte, params.DynamicFeeExtraDataSize)
	for i, v := range w {
		offset := i * wrappers.LongLen
		binary.BigEndian.PutUint64(bytes[offset:], v)
	}
	return bytes
}

func add(sum uint64, values ...uint64) uint64 {
	var overflow bool
	for _, v := range values {
		sum, overflow = math.SafeAdd(sum, v)
		if overflow {
			return math.MaxUint64
		}
	}
	return sum
}
