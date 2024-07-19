// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import (
	"errors"

	"golang.org/x/exp/constraints"

	"github.com/ava-labs/avalanchego/utils"
)

var (
	ErrOverflow  = errors.New("overflow")
	ErrUnderflow = errors.New("underflow")

	// Deprecated: Add64 is deprecated. Use Add[uint64] instead.
	Add64 = Add[uint64]

	// Deprecated: Mul64 is deprecated. Use Mul[uint64] instead.
	Mul64 = Mul[uint64]
)

// MaxUint returns the maximum value of an unsigned integer of type T.
func MaxUint[T constraints.Unsigned]() T {
	// Unsigned integers will underflow to their max value.
	// Ref: https://go.dev/ref/spec#Arithmetic_operators
	var maxValue T
	maxValue--
	return maxValue
}

// Add returns:
// 1) a + b
// 2) If there is overflow, an error
func Add[T constraints.Unsigned](a, b T) (T, error) {
	if a > MaxUint[T]()-b {
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
func Mul[T constraints.Unsigned](a, b T) (T, error) {
	if b != 0 && a > MaxUint[T]()/b {
		return 0, ErrOverflow
	}
	return a * b, nil
}

func AbsDiff[T constraints.Unsigned](a, b T) T {
	return max(a, b) - min(a, b)
}
