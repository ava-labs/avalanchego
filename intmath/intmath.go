// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package intmath provides special-case integer arithmetic.
package intmath

import (
	"errors"
	"math/bits"

	"golang.org/x/exp/constraints"
)

// BoundedSubtract returns `max(a-b,floor)` without underflow.
func BoundedSubtract[T constraints.Unsigned](a, b, floor T) T {
	// If `floor + b` overflows then it's impossible for `a` to ever be large
	// enough for the subtraction to not be bounded.
	minA := floor + b
	if overflow := minA < b; overflow || a <= minA {
		return floor
	}
	return a - b
}

// BoundedMultiply returns `min(a*b,ceil)` without overflow.
func BoundedMultiply[T constraints.Unsigned](a, b, ceil T) T {
	if b != 0 && a > ceil/b {
		return ceil
	}
	return a * b
}

// ErrOverflow is returned if a return value would have overflowed its type.
var ErrOverflow = errors.New("overflow")

// MulDiv returns the quotient and remainder of `(a*b)/den` without overflow in
// the event that `a*b>=2^64`. However, if the quotient were to overflow then
// [ErrOverflow] is returned.
func MulDiv[T ~uint64](a, b, den T) (quo, rem T, err error) {
	return mulDiv(a, b, den, false)
}

// MulDivCeil is equivalent to [MulDiv] except that it returns the rounded-up
// quotient and the complement of the remainder, i.e. the amount that would have
// had to be added to `a*b` to result in the same quotient exactly.
func MulDivCeil[T ~uint64](a, b, den T) (quo T, extra T, err error) {
	q, r, err := mulDiv(a, b, den, true)
	return q, den - r - 1, err
}

func mulDiv[T ~uint64](a, b, den T, ceil bool) (quo, rem T, err error) {
	hi, lo := bits.Mul64(uint64(a), uint64(b))

	if ceil {
		var carry uint64
		lo, carry = bits.Add64(lo, uint64(den)-1, 0)
		// If both `a` and `b` are MaxUint64 then `hi==MaxUint64-1` so this
		// can't overflow because `carry ∈ {0,1}`.
		hi += carry
	}

	if uint64(den) <= hi {
		return 0, 0, ErrOverflow
	}
	q, r := bits.Div64(hi, lo, uint64(den))
	return T(q), T(r), nil
}

// CeilDiv returns `ceil(num/den)`, i.e. the rounded-up quotient.
func CeilDiv[T ~uint64](num, den T) T {
	lo, hi := bits.Add64(uint64(num), uint64(den)-1, 0)
	// [bits.Div64] panics if the denominator is zero (expected behaviour) or if
	// `den <= hi`. The latter is impossible because `hi` is a carry bit (i.e.
	// can only be 0 or 1) and even if `num==MaxUint64` then `den` would have to
	// be `>=2` for `hi` to be non-zero.
	quo, _ := bits.Div64(hi, lo, uint64(den))
	return T(quo)
}
