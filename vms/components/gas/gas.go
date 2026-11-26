// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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

// Cost returns g*price.
//
// If overflow would occur, an error is returned.
func (g Gas) Cost(price Price) (uint64, error) {
	return safemath.Mul(uint64(g), uint64(price))
}

// AddOverTime returns g + gasRate * duration.
//
// If overflow would occur, MaxUint64 is returned.
func (g Gas) AddOverTime(gasRate Gas, duration uint64) Gas {
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

// SubOverTime returns g - gasRate * duration.
//
// If underflow would occur, 0 is returned.
func (g Gas) SubOverTime(gasRate Gas, duration uint64) Gas {
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
//nolint:dupword // The python is copied from the EIP-4844 specification
func CalculatePrice(
	minPrice Price,
	excess Gas,
	excessConversionConstant Gas,
) Price {
	return CalculatePriceWithExcessConversion(minPrice, excess, excessConversionConstant, 1)
}

// CalculatePriceWithExcessConversion is equivalent to [CalculatePrice], except
// that the excess conversion constant is supplied as the two factors whose
// product forms it. This allows the constant to exceed [math.MaxUint64].
//
// This implementation is optimized with the knowledge that any value greater
// than MaxUint64 gets returned as MaxUint64. This means that every intermediate
// value is guaranteed to be at most MaxUint256. So, we can safely use
// uint256.Int.
//
// This function does not perform any memory allocations.
func CalculatePriceWithExcessConversion(
	minPrice Price,
	excess Gas,
	excessConversionFactor1 Gas,
	excessConversionFactor2 Gas,
) Price {
	var (
		numerator   uint256.Int
		denominator uint256.Int
		factor      uint256.Int

		i              uint256.Int
		output         uint256.Int
		numeratorAccum uint256.Int

		maxOutput uint256.Int
	)
	numerator.SetUint64(uint64(excess))                    // range is [0, MaxUint64]
	denominator.SetUint64(uint64(excessConversionFactor1)) // range is [0, MaxUint64]
	factor.SetUint64(uint64(excessConversionFactor2))      // range is [0, MaxUint64]
	denominator.Mul(&denominator, &factor)                 // range is [0, MaxUint128]

	i.SetOne()
	numeratorAccum.SetUint64(uint64(minPrice))        // range is [0, MaxUint64]
	numeratorAccum.Mul(&numeratorAccum, &denominator) // range is [0, MaxUint192]

	maxOutput.Mul(&denominator, maxUint64) // range is [0, MaxUint192]
	for numeratorAccum.Sign() > 0 {
		output.Add(&output, &numeratorAccum) // range is [0, 2*MaxUint192]
		if output.Cmp(&maxOutput) >= 0 {
			return math.MaxUint64
		}
		// maxOutput < MaxUint192 so numeratorAccum < MaxUint192.
		numeratorAccum.Mul(&numeratorAccum, &numerator) // range is [0, MaxUint256]
		numeratorAccum.Div(&numeratorAccum, &denominator)
		numeratorAccum.Div(&numeratorAccum, &i)

		i.AddUint64(&i, 1)
	}
	return Price(output.Div(&output, &denominator).Uint64())
}
