// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fee

import (
	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	Bandwidth Dimension = 0
	DBRead    Dimension = 1
	DBWrite   Dimension = 2 // includes deletes
	Compute   Dimension = 3

	FeeDimensions = 4
)

var (
	ZeroGas      = Gas(0)
	ZeroGasPrice = GasPrice(0)
	Empty        = Dimensions{}
)

type (
	GasPrice uint64
	Gas      uint64

	Dimension  int
	Dimensions [FeeDimensions]uint64
)

func Add(lhs, rhs Dimensions) (Dimensions, error) {
	var res Dimensions
	for i := 0; i < FeeDimensions; i++ {
		v, err := safemath.Add64(lhs[i], rhs[i])
		if err != nil {
			return res, err
		}
		res[i] = v
	}
	return res, nil
}

func Remove(lhs, rhs Dimensions) (Dimensions, error) {
	var res Dimensions
	for i := 0; i < FeeDimensions; i++ {
		v, err := safemath.Sub(lhs[i], rhs[i])
		if err != nil {
			return res, err
		}
		res[i] = v
	}
	return res, nil
}

func ToGas(weights, dimensions Dimensions) (Gas, error) {
	gas, _, err := toGasWithReminder(weights, dimensions, ZeroGas)
	return gas, err
}

func toGasWithReminder(weights, dimensions Dimensions, gasReminder Gas) (Gas, Gas, error) {
	res := uint64(gasReminder)
	for i := 0; i < FeeDimensions; i++ {
		v, err := safemath.Mul64(weights[i], dimensions[i])
		if err != nil {
			return ZeroGas, ZeroGas, err
		}
		res, err = safemath.Add64(res, v)
		if err != nil {
			return ZeroGas, ZeroGas, err
		}
	}
	return Gas(res) / 10, Gas(res) % 10, nil
}
