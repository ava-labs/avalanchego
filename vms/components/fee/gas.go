// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	"math"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

type (
	Gas      uint64
	GasPrice uint64
)

func (g Gas) AddPerSecond(gasPerSecond Gas, seconds uint64) Gas {
	newGas, err := safemath.Mul64(uint64(gasPerSecond), seconds)
	if err != nil {
		return math.MaxUint64
	}
	totalGas, err := safemath.Add64(uint64(g), newGas)
	if err != nil {
		return math.MaxUint64
	}
	return Gas(totalGas)
}

func (g Gas) SubPerSecond(gasPerSecond Gas, seconds uint64) Gas {
	gasToRemove, err := safemath.Mul64(uint64(gasPerSecond), seconds)
	if err != nil {
		return 0
	}
	totalGas, err := safemath.Sub(uint64(g), gasToRemove)
	if err != nil {
		return 0
	}
	return Gas(totalGas)
}

// MulExp returns g*e^(excess / gasConversionConstant)
func (g GasPrice) MulExp(
	excess Gas,
	gasConversionConstant Gas,
) GasPrice {
	// TODO: Implement this
	return 0
}
