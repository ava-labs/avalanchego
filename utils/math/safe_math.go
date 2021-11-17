// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import (
	"errors"
	"math"
)

var errOverflow = errors.New("overflow occurred")

// Max64 returns the maximum of the values provided
func Max64(max uint64, nums ...uint64) uint64 {
	for _, num := range nums {
		if num > max {
			max = num
		}
	}
	return max
}

// Min64 returns the minimum of the values provided
func Min64(min uint64, nums ...uint64) uint64 {
	for _, num := range nums {
		if num < min {
			min = num
		}
	}
	return min
}

// Add64 returns:
// 1) a + b
// 2) If there is overflow, an error
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

// Mul64 returns:
// 1) a * b
// 2) If there is overflow, an error
func Mul64(a, b uint64) (uint64, error) {
	if b != 0 && a > math.MaxUint64/b {
		return 0, errOverflow
	}
	return a * b, nil
}

func Diff64(a, b uint64) uint64 {
	return Max64(a, b) - Min64(a, b)
}
