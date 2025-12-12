// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gas

import (
	"math"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

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
func CalculatePrice(
	minPrice Price,
	excess Gas,
	excessConversionConstant Gas,
) Price {
	return Price(safemath.ApproximateExponential(uint64(minPrice), uint64(excess), uint64(excessConversionConstant)))
}
