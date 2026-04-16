// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package threshold

import (
	"errors"
	"fmt"
	"math/big"

	blst "github.com/supranational/blst/bindings/go"
)

// BLS12-381 scalar field order r.
var groupOrder = new(big.Int).SetBytes([]byte{
	0x73, 0xed, 0xa7, 0x53, 0x29, 0x9d, 0x7d, 0x48,
	0x33, 0x39, 0xd8, 0x08, 0x09, 0xa1, 0xd8, 0x05,
	0x53, 0xbd, 0xa4, 0x02, 0xff, 0xfe, 0x5b, 0xfe,
	0xff, 0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x01,
})

var (
	errDuplicateIndex = errors.New("duplicate signer index")
	errZeroIndex      = errors.New("signer index must be >= 1")
)

// lagrangeCoefficients computes the Lagrange basis polynomial values at x=0
// for the given set of 1-based evaluation indices.
//
// For each index x_i in the set, the coefficient is:
//
//	L_i(0) = prod_{j != i}( x_j / (x_j - x_i) ) mod r
//
// All indices must be >= 1 and distinct.
// Returns one blst.Scalar per index, in the same order as the input.
func lagrangeCoefficients(indices []int) ([]blst.Scalar, error) {
	n := len(indices)

	// Validate indices.
	seen := make(map[int]struct{}, n)
	for _, idx := range indices {
		if idx < 1 {
			return nil, fmt.Errorf("%w: got %d", errZeroIndex, idx)
		}
		if _, exists := seen[idx]; exists {
			return nil, fmt.Errorf("%w: index %d", errDuplicateIndex, idx)
		}
		seen[idx] = struct{}{}
	}

	coeffs := make([]blst.Scalar, n)
	for i := 0; i < n; i++ {
		num := big.NewInt(1)
		den := big.NewInt(1)

		xi := big.NewInt(int64(indices[i]))
		for j := 0; j < n; j++ {
			if i == j {
				continue
			}
			xj := big.NewInt(int64(indices[j]))

			// num *= x_j
			num.Mul(num, xj)
			num.Mod(num, groupOrder)

			// den *= (x_j - x_i)
			diff := new(big.Int).Sub(xj, xi)
			diff.Mod(diff, groupOrder)
			den.Mul(den, diff)
			den.Mod(den, groupOrder)
		}

		// coeff = num * den^{-1} mod r
		denInv := new(big.Int).ModInverse(den, groupOrder)
		if denInv == nil {
			return nil, fmt.Errorf("modular inverse failed for index %d (degenerate indices)", indices[i])
		}
		coeff := new(big.Int).Mul(num, denInv)
		coeff.Mod(coeff, groupOrder)

		scalar := bigIntToScalar(coeff)
		if scalar == nil {
			return nil, fmt.Errorf("failed to convert Lagrange coefficient to scalar for index %d", indices[i])
		}
		coeffs[i] = *scalar
	}

	return coeffs, nil
}

// bigIntToScalar converts a non-negative big.Int (assumed mod r) to a blst.Scalar.
// The big.Int is serialized as a 32-byte big-endian value.
func bigIntToScalar(n *big.Int) *blst.Scalar {
	b := n.Bytes()

	// Zero-pad to 32 bytes on the left.
	padded := make([]byte, 32)
	copy(padded[32-len(b):], b)

	var s blst.Scalar
	return s.FromBEndian(padded)
}
