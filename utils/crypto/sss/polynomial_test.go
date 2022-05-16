// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sss

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewRandomEmptyPolynomial(t *testing.T) {
	assert := assert.New(t)

	secret := big.NewInt(10)
	maxCoefficient := big.NewInt(100)
	numCoefficients := uint(0)

	p, err := NewRandomPolynomial(secret, maxCoefficient, numCoefficients)
	assert.NoError(err)

	expected := big.NewInt(0)

	x := 0
	val := p.Evaluate(x, maxCoefficient)

	assert.Equal(expected.String(), val.String())

	x = 1
	val = p.Evaluate(x, maxCoefficient)

	assert.Equal(expected.String(), val.String())
}

func TestNewRandomPolynomial(t *testing.T) {
	assert := assert.New(t)

	secret := big.NewInt(10)
	maxCoefficient := big.NewInt(100)
	numCoefficients := uint(10)

	p, err := NewRandomPolynomial(secret, maxCoefficient, numCoefficients)
	assert.NoError(err)

	x := 0
	val := p.Evaluate(x, maxCoefficient)

	assert.Equal(secret.String(), val.String())
}

func TestPolynomialEvaluate(t *testing.T) {
	tests := []struct {
		name     string
		p        Polynomial
		x        int
		mod      *big.Int
		expected *big.Int
	}{
		{
			name:     "f(0) = 0 % 100",
			p:        []*big.Int{},
			x:        0,
			mod:      big.NewInt(100),
			expected: big.NewInt(0),
		},
		{
			name:     "f(1) = 0 % 100",
			p:        []*big.Int{},
			x:        1,
			mod:      big.NewInt(100),
			expected: big.NewInt(0),
		},
		{
			name: "f(1) = 10 % 100",
			p: []*big.Int{
				big.NewInt(10),
			},
			x:        1,
			mod:      big.NewInt(100),
			expected: big.NewInt(10),
		},
		{
			name: "f(1) = 100 % 10",
			p: []*big.Int{
				big.NewInt(100),
			},
			x:        1,
			mod:      big.NewInt(10),
			expected: big.NewInt(0),
		},
		{
			name: "f(5) = 10 + 5*x % 100",
			p: []*big.Int{
				big.NewInt(10),
				big.NewInt(5),
			},
			x:        5,
			mod:      big.NewInt(100),
			expected: big.NewInt(35),
		},
		{
			name: "f(5) = 10 + 5*x % 10",
			p: []*big.Int{
				big.NewInt(10),
				big.NewInt(5),
			},
			x:        5,
			mod:      big.NewInt(10),
			expected: big.NewInt(5),
		},
		{
			name: "f(3) = 10 + 5*x + 2*x^2 % 1000",
			p: []*big.Int{
				big.NewInt(10),
				big.NewInt(5),
				big.NewInt(2),
			},
			x:        3,
			mod:      big.NewInt(1000),
			expected: big.NewInt(43),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert := assert.New(t)

			val := test.p.Evaluate(test.x, test.mod)
			assert.Equal(test.expected.String(), val.String())
		})
	}
}
