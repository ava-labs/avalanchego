// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package threshold

import (
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"

	blst "github.com/supranational/blst/bindings/go"
)

var (
	errInsufficientPartials = errors.New("not enough partial signatures to reconstruct")
	errInvalidPartialSig    = errors.New("invalid partial signature")
)

// PartialSig is a threshold partial signature from one node.
type PartialSig struct {
	// Index is the 1-based Shamir evaluation point of the signer.
	// Derived from the signer's position in the canonical validator list + 1.
	Index int
	// Sig is the compressed BLS signature in G2 (96 bytes).
	Sig []byte
}

// Scheme holds the threshold parameters and group public key.
// It provides reconstruction and verification of threshold signatures.
type Scheme struct {
	Threshold int
	N         int
	GroupPK   *bls.PublicKey
}

// NewScheme creates a Scheme from config parameters.
// Used at runtime when keys already exist and you just need reconstruction/verification.
func NewScheme(threshold, n int, groupPK *bls.PublicKey) *Scheme {
	return &Scheme{
		Threshold: threshold,
		N:         n,
		GroupPK:   groupPK,
	}
}

// HasEnough returns true if count >= threshold.
func (s *Scheme) HasEnough(count int) bool {
	return count >= s.Threshold
}

// Remaining returns the number of additional partial sigs needed.
// Returns 0 if count already meets or exceeds the threshold.
func (s *Scheme) Remaining(count int) int {
	if count >= s.Threshold {
		return 0
	}
	return s.Threshold - count
}

// Reconstruct combines t partial signatures via Lagrange interpolation in G2.
// Returns the reconstructed threshold signature as a compressed G2 point (96 bytes).
func (s *Scheme) Reconstruct(partials []PartialSig) ([]byte, error) {
	if len(partials) < s.Threshold {
		return nil, fmt.Errorf(
			"%w: have %d, need %d",
			errInsufficientPartials, len(partials), s.Threshold,
		)
	}

	// Extract indices for Lagrange computation.
	indices := make([]int, len(partials))
	for i, p := range partials {
		indices[i] = p.Index
	}

	// Compute Lagrange coefficients.
	coeffs, err := lagrangeCoefficients(indices)
	if err != nil {
		return nil, fmt.Errorf("failed to compute Lagrange coefficients: %w", err)
	}

	// Decompress each partial signature and perform multi-scalar multiplication.
	// result = sum( coeffs[i] * partials[i] ) in G2
	var result blst.P2
	for i, p := range partials {
		sig, err := bls.SignatureFromBytes(p.Sig)
		if err != nil {
			return nil, fmt.Errorf("%w at index %d: %w", errInvalidPartialSig, p.Index, err)
		}

		// Convert affine to projective, multiply by Lagrange coefficient, accumulate.
		var proj blst.P2
		proj.FromAffine(sig)
		scaled := proj.Mult(&coeffs[i])

		if i == 0 {
			result = *scaled
		} else {
			result.AddAssign(scaled)
		}
	}

	return result.ToAffine().Compress(), nil
}

// Verify checks a reconstructed threshold signature against the group public key.
func (s *Scheme) Verify(sig []byte, msg []byte) error {
	parsedSig, err := bls.SignatureFromBytes(sig)
	if err != nil {
		return fmt.Errorf("invalid threshold signature: %w", err)
	}

	if !bls.Verify(s.GroupPK, parsedSig, msg) {
		return errors.New("threshold signature verification failed")
	}

	return nil
}
