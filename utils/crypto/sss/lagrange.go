// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sss

import (
	"math/big"
)

var zero = big.NewInt(0)

func CalculateSecret(points []Point, mod *big.Int) *big.Int {
	xProd := 1
	for _, point := range points {
		xProd *= -point.X
	}

	/*
	 x_1-x_2    x_1-x_3    x_1-x_4
	 x_2-x_3    x_2-x_4
	 x_3-x_4
	*/
	/*
	 1,2    1,3    1,4
	 2,3    2,4
	 3,4
	*/
	// TODO: alloc only 1 array
	diffs := make([][]int, len(points)-1)
	for i := range diffs {
		thisDiff := make([]int, len(points)-i-1)
		for jIndex, j := 0, i+1; j < len(points); jIndex, j = jIndex+1, j+1 {
			thisDiff[jIndex] = points[i].X - points[j].X
		}
		diffs[i] = thisDiff
	}

	denominators := make([]int, len(points))
	for i, point := range points {
		prod := big.NewInt(int64(-point.X))
		if i < len(diffs) {
			for _, diff := range diffs[i] {
				prod.Mul(prod, big.NewInt(int64(diff)))
			}
		}
		for j := i - 1; j >= 0; j-- {
			thisDiffs := diffs[j]
			diff := thisDiffs[len(thisDiffs)-len(points)+i]
			prod.Mul(prod, big.NewInt(-int64(diff)))
		}
		denominators[i] = int(prod.Int64())
	}

	denominatorProd := 1
	for _, denominator := range denominators {
		denominatorProd *= denominator
	}
	scale := xProd * denominatorProd

	result := big.NewInt(0)
	for i, point := range points {
		bigScale := big.NewInt(int64(scale))
		bigScale.Mul(bigScale, point.Y)
		bigScale.Mod(bigScale, mod)

		denominator := big.NewInt(int64(denominators[i]))
		inverse(denominator, mod)
		bigScale.Mul(bigScale, denominator)

		result.Add(result, bigScale)
	}
	result.Mod(result, mod)
	denominator := big.NewInt(int64(denominatorProd))
	inverse(denominator, mod)
	result.Mul(result, denominator)
	result.Mod(result, mod)

	return result
}

func inverse(den, mod *big.Int) {
	mod = new(big.Int).Set(mod)

	temp := new(big.Int)
	x := big.NewInt(0)
	lastX := big.NewInt(1)
	for mod.Cmp(zero) != 0 {
		temp.Div(den, mod)
		temp.Mul(temp, x)
		temp.Sub(
			lastX,
			temp,
		)
		lastX.Set(x)
		x.Set(temp)

		temp.Mod(den, mod)
		den.Set(mod)
		mod, temp = temp, mod
	}
	den.Set(lastX)
}
