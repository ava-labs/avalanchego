// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import (
	"errors"
	"math"

	"golang.org/x/exp/constraints"
)

var errOverflow = errors.New("overflow occurred")

func Max[T constraints.Ordered](max T, nums ...T) T {
	for _, num := range nums {
		if num > max {
			max = num
		}
	}
	return max
}

func Min[T constraints.Ordered](min T, nums ...T) T {
	for _, num := range nums {
		if num < min {
			min = num
		}
	}
	return min
}

// Add returns:
// 1) a + b
// 2) If there is overflow, an error
func Add[T constraints.Unsigned](a, b T) (T, error) {
	if uint64(a) > math.MaxUint64-uint64(b) {
		return 0, errOverflow
	}
	return a + b, nil
}

// Sub returns:
// 1) a - b
// 2) If there is underflow, an error
func Sub[T constraints.Unsigned](a, b T) (T, error) {
	if a < b {
		return *new(T), errOverflow //nolint:gocritic
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
	return Max(a, b) - Min(a, b)
}
