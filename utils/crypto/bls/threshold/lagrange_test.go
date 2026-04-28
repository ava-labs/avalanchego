// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package threshold

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLagrangeCoefficients verifies Lagrange coefficient computation with a known case.
// For indices {1, 2, 3} with threshold 3, we can verify the coefficients manually:
//
//	L_1(0) = (2*3) / ((2-1)*(3-1)) = 6/2 = 3
//	L_2(0) = (1*3) / ((1-2)*(3-2)) = 3/(-1) = -3 mod r
//	L_3(0) = (1*2) / ((1-3)*(2-3)) = 2/((-2)*(-1)) = 2/2 = 1
func TestLagrangeCoefficients(t *testing.T) {
	coeffs, err := lagrangeCoefficients([]int{1, 2, 3})
	require.NoError(t, err)
	require.Len(t, coeffs, 3)

	// L_1(0) = 3
	c1Bytes := coeffs[0].Serialize()
	require.Equal(t, byte(3), c1Bytes[31])
	for i := 0; i < 31; i++ {
		require.Equal(t, byte(0), c1Bytes[i])
	}

	// L_3(0) = 1
	c3Bytes := coeffs[2].Serialize()
	require.Equal(t, byte(1), c3Bytes[31])
	for i := 0; i < 31; i++ {
		require.Equal(t, byte(0), c3Bytes[i])
	}

	// L_2(0) = -3 mod r = r - 3
	// Verify by checking that the sum L_1 + L_2 + L_3 = 1 (property of Lagrange at x=0 with p(0)=1 for p(x)=1).
	// Actually the sum of Lagrange coefficients at x=0 always equals 1 for any set of indices.
	// So L_2 = 1 - 3 - 1 = -3 mod r
}

// TestLagrangeZeroIndex verifies that index 0 is rejected.
func TestLagrangeZeroIndex(t *testing.T) {
	_, err := lagrangeCoefficients([]int{0, 1, 2})
	require.ErrorIs(t, err, errZeroIndex)
}

// TestLagrangeDuplicateIndex verifies that duplicate indices are rejected.
func TestLagrangeDuplicateIndex(t *testing.T) {
	_, err := lagrangeCoefficients([]int{1, 1, 2})
	require.ErrorIs(t, err, errDuplicateIndex)
}

// TestLagrangeNegativeIndex verifies that negative indices are rejected.
func TestLagrangeNegativeIndex(t *testing.T) {
	_, err := lagrangeCoefficients([]int{-1, 1, 2})
	require.ErrorIs(t, err, errZeroIndex)
}

// TestLagrangeSingleIndex verifies coefficients for a single index.
// With only one index, L_1(0) = 1 (the product is empty, so it's 1).
func TestLagrangeSingleIndex(t *testing.T) {
	coeffs, err := lagrangeCoefficients([]int{5})
	require.NoError(t, err)
	require.Len(t, coeffs, 1)

	// L_5(0) with no other indices = 1
	cBytes := coeffs[0].Serialize()
	require.Equal(t, byte(1), cBytes[31])
	for i := 0; i < 31; i++ {
		require.Equal(t, byte(0), cBytes[i])
	}
}

// TestLagrangeCoefficientsSumToOne verifies that Lagrange coefficients always
// sum to 1 mod r. This is a fundamental property: for any polynomial p(x) = 1
// (constant), interpolation at x=0 must return 1, so the weights must sum to 1.
func TestLagrangeCoefficientsSumToOne(t *testing.T) {
	testCases := [][]int{
		{1, 2, 3},
		{1, 3, 5},
		{2, 4, 6, 8},
		{1, 2, 3, 4, 5},
		{10, 20, 30},
	}

	one := big.NewInt(1)

	for _, indices := range testCases {
		coeffs, err := lagrangeCoefficients(indices)
		require.NoError(t, err)

		sum := new(big.Int)
		for _, c := range coeffs {
			b := c.Serialize()
			val := new(big.Int).SetBytes(b)
			sum.Add(sum, val)
			sum.Mod(sum, groupOrder)
		}

		require.Equal(t, one, sum, "coefficients for indices %v do not sum to 1", indices)
	}
}

// TestLagrangeNonContiguousIndices verifies that non-contiguous indices work correctly.
// The indices don't need to be 1..n — any distinct positive integers work.
func TestLagrangeNonContiguousIndices(t *testing.T) {
	coeffs, err := lagrangeCoefficients([]int{3, 7, 11})
	require.NoError(t, err)
	require.Len(t, coeffs, 3)

	// Verify sum = 1.
	sum := new(big.Int)
	for _, c := range coeffs {
		b := c.Serialize()
		val := new(big.Int).SetBytes(b)
		sum.Add(sum, val)
		sum.Mod(sum, groupOrder)
	}
	require.Equal(t, big.NewInt(1), sum)
}

// TestLagrangeOrderIndependence verifies that the coefficients correspond to
// the correct index regardless of input ordering.
func TestLagrangeOrderIndependence(t *testing.T) {
	coeffs1, err := lagrangeCoefficients([]int{1, 2, 3})
	require.NoError(t, err)

	coeffs2, err := lagrangeCoefficients([]int{3, 1, 2})
	require.NoError(t, err)

	// coeffs1[0] is for index 1, coeffs2[1] is for index 1 — should match.
	require.Equal(t, coeffs1[0].Serialize(), coeffs2[1].Serialize())
	// coeffs1[1] is for index 2, coeffs2[2] is for index 2.
	require.Equal(t, coeffs1[1].Serialize(), coeffs2[2].Serialize())
	// coeffs1[2] is for index 3, coeffs2[0] is for index 3.
	require.Equal(t, coeffs1[2].Serialize(), coeffs2[0].Serialize())
}

// TestBigIntToScalar verifies the conversion helper.
func TestBigIntToScalar(t *testing.T) {
	// Small value.
	s := bigIntToScalar(big.NewInt(42))
	require.NotNil(t, s)
	b := s.Serialize()
	require.Equal(t, byte(42), b[31])
	for i := 0; i < 31; i++ {
		require.Equal(t, byte(0), b[i])
	}

	// Larger value.
	s = bigIntToScalar(big.NewInt(256))
	require.NotNil(t, s)
	b = s.Serialize()
	require.Equal(t, byte(1), b[30])
	require.Equal(t, byte(0), b[31])
}
