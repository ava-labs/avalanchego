// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package math

import (
	"math"

	"github.com/holiman/uint256"
)

var max256Uint64 = new(uint256.Int).SetUint64(math.MaxUint64)

// CalculateExponential returns the approximate exponential result given the factor, the
// numerator, and the denominator.
//
// It is defined as an approximation of:
//
//	factor * e^(numerator / denominator)
//
// This implements the EIP-4844 fake exponential formula:
//
//	def fake_exponential(factor: int, numerator: int, denominator: int) -> int:
//		i = 1
//		output = 0
//		numerator_accum = factor * denominator
//		while numerator_accum > 0:
//			output += numerator_accum
//			numerator_accum = (numerator_accum * numerator) // (denominator * i)
//			i += 1
//		return output // denominator
//
// This implementation is optimized with the knowledge that any value greater
// than MaxUint64 gets returned as MaxUint64. This means that every intermediate
// value is guaranteed to be at most MaxUint193. So, we can safely use
// uint256.Int.
//
// This function does not perform any memory allocations.
//
//nolint:dupword // The python is copied from the EIP-4844 specification
func CalculateExponential(
	factor uint64,
	numerator uint64,
	denominator uint64,
) uint64 {
	var (
		num   uint256.Int
		denom uint256.Int

		i              uint256.Int
		output         uint256.Int
		numeratorAccum uint256.Int

		maxOutput uint256.Int
	)
	num.SetUint64(numerator)     // range is [0, MaxUint64]
	denom.SetUint64(denominator) // range is [0, MaxUint64]

	i.SetOne()
	numeratorAccum.SetUint64(factor)            // range is [0, MaxUint64]
	numeratorAccum.Mul(&numeratorAccum, &denom) // range is [0, MaxUint128]

	maxOutput.Mul(&denom, max256Uint64) // range is [0, MaxUint128]
	for numeratorAccum.Sign() > 0 {
		output.Add(&output, &numeratorAccum) // range is [0, MaxUint192+MaxUint128]
		if output.Cmp(&maxOutput) >= 0 {
			return math.MaxUint64
		}
		// maxOutput < MaxUint128 so numeratorAccum < MaxUint128.
		numeratorAccum.Mul(&numeratorAccum, &num) // range is [0, MaxUint192]
		numeratorAccum.Div(&numeratorAccum, &denom)
		numeratorAccum.Div(&numeratorAccum, &i)

		i.AddUint64(&i, 1)
	}
	return output.Div(&output, &denom).Uint64()
}
