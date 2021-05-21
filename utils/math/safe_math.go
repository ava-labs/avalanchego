// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import (
	"errors"
	"math"
)

var errOverflow = errors.New("overflow occurred")

// Max64 ...
func Max64(a, b uint64) uint64 {
	if a < b {
		return b
	}
	return a
}

// Min64 ...
func Min64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

// Add64 ...
func Add64(a, b uint64) (uint64, error) {
	if a > math.MaxUint64-b {
		return 0, errOverflow
	}
	return a + b, nil
}

// Sub64 returns:
// 1) a - b
// 2) If there is underflow, an error
func Sub64(a, b uint64) (uint64, error) {
	if a < b {
		return 0, errOverflow
	}
	return a - b, nil
}

// Mul64 ...
func Mul64(a, b uint64) (uint64, error) {
	if b != 0 && a > math.MaxUint64/b {
		return 0, errOverflow
	}
	return a * b, nil
}

// Diff64 ...
func Diff64(a, b uint64) uint64 {
	return Max64(a, b) - Min64(a, b)
}
