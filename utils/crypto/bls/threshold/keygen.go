// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package threshold

import (
	"fmt"
	"math/big"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"

	blst "github.com/supranational/blst/bindings/go"
)

// GenerateKeys creates a threshold key set from a deterministic seed.
//
// Returns:
//   - scheme: Scheme with group PK, threshold, N (safe to distribute to all nodes)
//   - signers: n LocalSigners whose secret keys are polynomial evaluations p(i+1)
//   - masterSK: the master secret key a_0 = p(0) (for test assertions only)
//
// The i-th signer (0-indexed) holds the key for Shamir index i+1 (1-based).
//
// POC only: all nodes sharing the seed can derive all keys.
// Production: replace with DKG (masterSK is never known to anyone).
func GenerateKeys(n, t int, seed [32]byte) (*Scheme, []*localsigner.LocalSigner, *blst.SecretKey, error) {
	if t < 1 {
		return nil, nil, nil, fmt.Errorf("threshold must be >= 1, got %d", t)
	}
	if n < t {
		return nil, nil, nil, fmt.Errorf("n (%d) must be >= threshold (%d)", n, t)
	}

	// Generate t polynomial coefficients deterministically from seed.
	// Each coefficient a_k is a secret key derived from KeyGen(seed, info_k).
	// The coefficients do not define the secret shares directly, they define the polynomial..
	coefficients := make([]*blst.SecretKey, t)
	for k := 0; k < t; k++ {
		info := []byte(fmt.Sprintf("threshold-coeff-%d", k))
		sk := blst.KeyGen(seed[:], info)
		if sk == nil {
			return nil, nil, nil, fmt.Errorf("failed to generate coefficient %d", k)
		}
		coefficients[k] = sk
	}

	// Convert coefficients to big.Int for polynomial evaluation.
	coeffBig := make([]*big.Int, t)
	for k, sk := range coefficients {
		coeffBig[k] = new(big.Int).SetBytes(sk.Serialize())
	}

	// Evaluate p(i) for i = 1..n to produce each node's secret key share.
	signers := make([]*localsigner.LocalSigner, n)
	for i := 0; i < n; i++ {
		x := int64(i + 1) // 1-based evaluation point
		shareScalar := evaluatePolynomial(coeffBig, x)

		// Convert big.Int to 32-byte big-endian for localsigner.FromBytes.
		shareBytes := make([]byte, 32)
		b := shareScalar.Bytes()
		copy(shareBytes[32-len(b):], b)

		signer, err := localsigner.FromBytes(shareBytes)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to create signer for index %d: %w", i+1, err)
		}
		signers[i] = signer
	}

	// Group public key = g1^{a_0}.
	groupPK := new(bls.PublicKey).From(coefficients[0])

	scheme := NewScheme(t, n, groupPK)
	return scheme, signers, coefficients[0], nil
}

// evaluatePolynomial evaluates p(x) = a_0 + a_1*x + a_2*x^2 + ... + a_{t-1}*x^{t-1} mod r.
// Uses Horner's method for efficiency.
func evaluatePolynomial(coefficients []*big.Int, x int64) *big.Int {
	xBig := big.NewInt(x)
	result := new(big.Int).Set(coefficients[len(coefficients)-1])

	for k := len(coefficients) - 2; k >= 0; k-- {
		result.Mul(result, xBig)
		result.Add(result, coefficients[k])
		result.Mod(result, groupOrder)
	}

	return result
}
