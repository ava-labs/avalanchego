// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package fees

import (
	safemath "github.com/ava-labs/avalanchego/utils/math"
)

const (
	Bandwidth Dimension = 0
	UTXORead  Dimension = 1
	UTXOWrite Dimension = 2 // includes delete
	Compute   Dimension = 3 // signatures checks, tx-specific

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

func ScalarProd(lhs, rhs Dimensions) (Gas, error) {
	var res uint64
	for i := 0; i < FeeDimensions; i++ {
		v, err := safemath.Mul64(lhs[i], rhs[i])
		if err != nil {
			return ZeroGas, err
		}
		res, err = safemath.Add64(res, v)
		if err != nil {
			return ZeroGas, err
		}
	}
	return Gas(res), nil
}
