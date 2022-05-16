// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sss

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

// Polynomial ends up representing a polynomial as:
// f(x) = sum from i=0 to i=n of polynomial[i] * x^i
type Polynomial []*big.Int

func NewRandomPolynomial(secret, maxCoefficient *big.Int, numCoefficients uint) (Polynomial, error) {
	if numCoefficients == 0 {
		return nil, nil
	}
	p := make([]*big.Int, numCoefficients)
	p[0] = secret

	var err error
	for i := uint(1); i < numCoefficients; i++ {
		p[i], err = rand.Int(rand.Reader, maxCoefficient)
		if err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (p Polynomial) Evaluate(x uint, mod *big.Int) *big.Int {
	numCoefficients := len(p)
	if numCoefficients == 0 {
		return big.NewInt(0)
	}

	result := new(big.Int).Set(p[numCoefficients-1])
	result.Mod(result, mod)

	bigX := new(big.Int).SetUint64(uint64(x))
	for index := numCoefficients - 2; index >= 0; index-- {
		result.Mul(result, bigX)
		result.Add(result, p[index])
		result.Mod(result, mod)
	}
	return result
}

func (p Polynomial) String() string {
	sb := strings.Builder{}
	for i, c := range p {
		if i != 0 {
			sb.WriteString(" + ")
		}
		sb.WriteString(fmt.Sprintf("%d * x^%d", c, i))
	}
	return sb.String()
}
