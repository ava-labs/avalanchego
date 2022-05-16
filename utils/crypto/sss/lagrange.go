// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sss

import (
	"math/big"
)

// CalculateSecret performs a lagrange interpolation of the y-intercept of the
// polynomial that is uniquely described by [points] in the finite field
// described by [mod].
func CalculateSecret(points []Point, mod *big.Int) *big.Int {
	temp := new(big.Int)
	temp2 := new(big.Int)

	denominators := make([]*big.Int, len(points))
	for i, iPoint := range points {
		prod := new(big.Int)
		denominators[i] = prod
		prod.SetUint64(uint64(iPoint.X))
		prod.Neg(prod)

		for j, jPoint := range points {
			if i == j {
				continue
			}
			temp.SetUint64(uint64(iPoint.X))
			temp2.SetUint64(uint64(jPoint.X))
			temp.Sub(temp, temp2)

			prod.Mul(prod, temp)
		}
	}

	denominatorProd := big.NewInt(1)
	for _, denominator := range denominators {
		denominatorProd.Mul(denominatorProd, denominator)
	}

	numerator := big.NewInt(1)
	for _, point := range points {
		temp.SetInt64(int64(-point.X))
		numerator.Mul(numerator, temp)
	}
	numerator.Mul(numerator, denominatorProd)

	result := big.NewInt(0)
	for i, point := range points {
		temp.Set(numerator)
		temp.Mul(temp, point.Y)
		temp.Mod(temp, mod)

		denominator := denominators[i]
		denominator.ModInverse(denominator, mod)
		temp.Mul(temp, denominator)

		result.Add(result, temp)
	}
	result.Mod(result, mod)
	denominatorProd.ModInverse(denominatorProd, mod)
	result.Mul(result, denominatorProd)
	result.Mod(result, mod)

	return result
}
