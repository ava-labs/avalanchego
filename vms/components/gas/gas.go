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

// Cost converts the gas to nAVAX based on the price.
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
// The result is capped at [math.MaxUint64]. excessConversionConstant MUST be
// non-zero.
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
	var denominator uint256.Int
	denominator.SetUint64(uint64(excessConversionConstant)) // range is [0, MaxUint64]
	return calculatePrice(minPrice, excess, &denominator)
}

// CalculatePriceWithExcessConversion is equivalent to [CalculatePrice], with
// excessConversionConstant provided as a [uint256.Int] to allow values above
// MaxUint64. excessConversionConstant MUST be in the range [1, MaxUint128].
func CalculatePriceWithExcessConversion(
	minPrice Price,
	excess Gas,
	excessConversionConstant *uint256.Int,
) Price {
	return calculatePrice(minPrice, excess, excessConversionConstant)
}

// calculatePrice is the shared implementation of [CalculatePrice] and
// [CalculatePriceWithExcessConversion].
//
// This implementation is optimized with the knowledge that any value greater
// than MaxUint64 gets returned as MaxUint64. With denominator in [1, MaxUint128],
// every intermediate value fits in a uint256.Int:
//   - maxOutput (= denominator * MaxUint64) is below 2^192;
//   - each term selected for multiplication is below maxOutput, so multiplying it
//     by numerator (< 2^64) stays below 2^256;
//   - after division by denominator, the next term is below MaxUint64^2; and
//   - output is kept below maxOutput, so adding the next term stays below
//     2^192 + 2^128.
//
// This function does not perform any memory allocations.
func calculatePrice(
	minPrice Price,
	excess Gas,
	denominator *uint256.Int,
) Price {
	var (
		numerator uint256.Int

		i              uint256.Int
		output         uint256.Int
		numeratorAccum uint256.Int

		maxOutput uint256.Int
	)
	numerator.SetUint64(uint64(excess)) // range is [0, MaxUint64]

	i.SetOne()
	numeratorAccum.SetUint64(uint64(minPrice))       // range is [0, MaxUint64]
	numeratorAccum.Mul(&numeratorAccum, denominator) // range is [0, MaxUint192]

	maxOutput.Mul(denominator, maxUint64) // range is [0, MaxUint192]
	for numeratorAccum.Sign() > 0 {
		output.Add(&output, &numeratorAccum) // range is [0, 2*MaxUint192)
		if output.Cmp(&maxOutput) >= 0 {
			return math.MaxUint64
		}
		// numeratorAccum < maxOutput <= MaxUint192.
		numeratorAccum.Mul(&numeratorAccum, &numerator) // range is [0, MaxUint256)
		numeratorAccum.Div(&numeratorAccum, denominator)
		numeratorAccum.Div(&numeratorAccum, &i)

		i.AddUint64(&i, 1)
	}
	return Price(output.Div(&output, denominator).Uint64())
}
