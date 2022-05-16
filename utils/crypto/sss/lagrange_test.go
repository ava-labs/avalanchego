// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package sss

import (
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/stretchr/testify/assert"
)

func ExampleCalculateSecret() {
	CalculateSecret([]Point{
		{
			X: 1,
			Y: big.NewInt(3),
		},
		{
			X: 2,
			Y: big.NewInt(4),
		},
		{
			X: 7,
			Y: big.NewInt(11),
		},
	}, big.NewInt(100))

	// Output: debugging
}

func TestCalculateSecret(t *testing.T) {
	assert := assert.New(t)

	maxCoefficient := big.NewInt(1439)
	for i := 0; i < 1000; i++ {
		secret, _ := rand.Int(rand.Reader, maxCoefficient)
		numCoefficients := uint(2)

		p, err := NewRandomPolynomial(secret, maxCoefficient, numCoefficients)
		assert.NoError(err)

		t.Logf("polynomial: f(x) = %s", p)

		secretShare0 := p.Evaluate(1, maxCoefficient)
		t.Logf("secretShare: = f(%d) = %d", 1, secretShare0)

		secretShare1 := p.Evaluate(2, maxCoefficient)
		t.Logf("secretShare: = f(%d) = %d", 2, secretShare1)

		calculatedSecret := CalculateSecret(
			[]Point{
				{
					X: 1,
					Y: secretShare0,
				},
				{
					X: 2,
					Y: secretShare1,
				},
			},
			maxCoefficient,
		)

		assert.Equal(secret.String(), calculatedSecret.String())
	}
}
