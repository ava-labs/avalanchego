// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gas

import (
	"math"

	"github.com/holiman/uint256"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var maxUint64 = new(uint256.Int).SetUint64(math.MaxUint64)

type (
	Gas   uint64
	Price uint64
)

// Cost converts the gas to nAVAX based on the price.
//
// If overflow would occur, an error is returned.
func (g Gas) Cost(price Price) (uint64, error) {
	return safemath.Mul(uint64(g), uint64(price))
}

// AddDuration returns g + gasRate * duration.
// The units chosen for time must be consistent with the units chosen for gasRate.
//
// If overflow would occur, MaxUint64 is returned.
func (g Gas) AddDuration(gasRate Gas, duration uint64) Gas {
	newGas, err := safemath.Mul(uint64(gasRate), duration)
	if err != nil {
		return math.MaxUint64
	}
	totalGas, err := safemath.Add(uint64(g), newGas)
	if err != nil {
		return math.MaxUint64
	}
	return Gas(totalGas)
}

// SubDuration returns g - gasRate * duration.
// The units chosen for time must be consistent with the units chosen for gasRate.
//
// If underflow would occur, 0 is returned.
func (g Gas) SubDuration(gasRate Gas, duration uint64) Gas {
	gasToRemove, err := safemath.Mul(uint64(gasRate), duration)
	if err != nil {
		return 0
	}
	totalGas, err := safemath.Sub(uint64(g), gasToRemove)
	if err != nil {
		return 0
	}
	return Gas(totalGas)
}

// CalculatePrice returns the gas price given the minimum gas price, the
// excess gas, and the excess conversion constant.
//
// It is defined as an approximation of:
//
//	minPrice * e^(excess / excessConversionConstant)
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
func CalculatePrice(
	minPrice Price,
	excess Gas,
	excessConversionConstant Gas,
) Price {
	var (
		numerator   uint256.Int
		denominator uint256.Int

		i              uint256.Int
		output         uint256.Int
		numeratorAccum uint256.Int

		maxOutput uint256.Int
	)
	numerator.SetUint64(uint64(excess))                     // range is [0, MaxUint64]
	denominator.SetUint64(uint64(excessConversionConstant)) // range is [0, MaxUint64]

	i.SetOne()
	numeratorAccum.SetUint64(uint64(minPrice))        // range is [0, MaxUint64]
	numeratorAccum.Mul(&numeratorAccum, &denominator) // range is [0, MaxUint128]

	maxOutput.Mul(&denominator, maxUint64) // range is [0, MaxUint128]
	for numeratorAccum.Sign() > 0 {
		output.Add(&output, &numeratorAccum) // range is [0, MaxUint192+MaxUint128]
		if output.Cmp(&maxOutput) >= 0 {
			return math.MaxUint64
		}
		// maxOutput < MaxUint128 so numeratorAccum < MaxUint128.
		numeratorAccum.Mul(&numeratorAccum, &numerator) // range is [0, MaxUint192]
		numeratorAccum.Div(&numeratorAccum, &denominator)
		numeratorAccum.Div(&numeratorAccum, &i)

		i.AddUint64(&i, 1)
	}
	return Price(output.Div(&output, &denominator).Uint64())
}
