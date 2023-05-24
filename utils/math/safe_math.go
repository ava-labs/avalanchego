// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import (
	"errors"
	"math"

	"golang.org/x/exp/constraints"

	"github.com/ava-labs/avalanchego/utils"
)

var (
	ErrOverflow  = errors.New("overflow")
	ErrUnderflow = errors.New("underflow")
)

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

// Add64 returns:
// 1) a + b
// 2) If there is overflow, an error
//
// Note that we don't have a generic Add function because checking for
// an overflow requires knowing the max size of a given type, which we
// don't know if we're adding generic types.
func Add64(a, b uint64) (uint64, error) {
	if a > math.MaxUint64-b {
		return 0, ErrOverflow
	}
	return a + b, nil
}

// Sub returns:
// 1) a - b
// 2) If there is underflow, an error
func Sub[T constraints.Unsigned](a, b T) (T, error) {
	if a < b {
		return utils.Zero[T](), ErrUnderflow
	}
	return a - b, nil
}

// Mul64 returns:
// 1) a * b
// 2) If there is overflow, an error
//
// Note that we don't have a generic Mul function because checking for
// an overflow requires knowing the max size of a given type, which we
// don't know if we're adding generic types.
func Mul64(a, b uint64) (uint64, error) {
	if b != 0 && a > math.MaxUint64/b {
		return 0, ErrOverflow
	}
	return a * b, nil
}

func AbsDiff[T constraints.Unsigned](a, b T) T {
	return Max(a, b) - Min(a, b)
}
