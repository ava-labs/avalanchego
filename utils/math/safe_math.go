// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import (
	"errors"
	"math"
	"math/bits"

	"golang.org/x/exp/constraints"
)

var (
	ErrOverflow     = errors.New("overflow")
	ErrUnderflow    = errors.New("underflow")
	errDivideByZero = errors.New("divide by zero")

	// Deprecated: Add64 is deprecated. Use Add[uint64] instead.
	Add64 = Add[uint64]

	// Deprecated: Mul64 is deprecated. Use Mul[uint64] instead.
	Mul64 = Mul[uint64]
)

// MaxUint returns the maximum value of an unsigned integer of type T.
func MaxUint[T constraints.Unsigned]() T {
	return ^T(0)
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
		return 0, ErrUnderflow
	}
	return a - b, nil
}

// Mul returns:
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

// MulDiv computes (a * b) / c with full precision.
// The result is rounded to the nearest integer.
// Returns errDivideByZero if c is zero, or ErrOverflow if the result exceeds uint64.
func MulDiv(a, b, c uint64) (uint64, error) {
	if c == 0 {
		return 0, errDivideByZero
	}

	// bits.Mul64 returns the full 128-bit product as hi<<64 + lo, so the
	// intermediate multiplication doesn't overflow even when a*b > MaxUint64.
	hi, lo := bits.Mul64(a, b)

	// bits.Div64 requires c > hi. If c <= hi, the quotient of the 128-bit
	// product divided by c is at least 2^64 and can't fit in a uint64.
	if c <= hi {
		return 0, ErrOverflow
	}

	quo, rem := bits.Div64(hi, lo, c)

	// Round to nearest by checking whether the fractional part rem/c is less
	// than 1/2. The rem < 1<<63 guard prevents overflow in 2*rem.
	if rem < (1<<63) && 2*rem < c {
		return quo, nil
	}

	// The fractional part is at least 1/2, so round up. If quo is already
	// MaxUint64, rounding up would overflow the result.
	if quo == math.MaxUint64 {
		return 0, ErrOverflow
	}
	return quo + 1, nil
}
