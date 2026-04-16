// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package threshold

import (
	"testing"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"

	"github.com/stretchr/testify/require"
)

var testSeed = [32]byte{
	1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
	17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32,
}

var testMsg = []byte("test message for threshold BLS")

func generateAndSign(t *testing.T, n, threshold int, msg []byte) (*Scheme, []PartialSig) {
	t.Helper()

	scheme, signers, _, err := GenerateKeys(n, threshold, testSeed)
	require.NoError(t, err)

	partials := make([]PartialSig, n)
	for i, signer := range signers {
		sig, err := signer.Sign(msg)
		require.NoError(t, err)
		partials[i] = PartialSig{
			Index: i + 1, // 1-based
			Sig:   bls.SignatureToBytes(sig),
		}
	}

	return scheme, partials
}

// TestReconstructMatchesMasterKey verifies that the reconstructed threshold
// signature is byte-identical to a signature produced directly by the master key.
func TestReconstructMatchesMasterKey(t *testing.T) {
	scheme, signers, masterSK, err := GenerateKeys(4, 3, testSeed)
	require.NoError(t, err)

	// Sign directly with the master key.
	directSig := new(bls.Signature).Sign(masterSK, testMsg, bls.CiphersuiteSignature.Bytes())
	directSigBytes := bls.SignatureToBytes(directSig)

	// Sign with 3 shares and reconstruct.
	partials := make([]PartialSig, 3)
	for i := 0; i < 3; i++ {
		sig, err := signers[i].Sign(testMsg)
		require.NoError(t, err)
		partials[i] = PartialSig{
			Index: i + 1,
			Sig:   bls.SignatureToBytes(sig),
		}
	}

	reconstructed, err := scheme.Reconstruct(partials)
	require.NoError(t, err)

	// Must be byte-identical.
	require.Equal(t, directSigBytes, reconstructed)
}

// TestReconstructHappyPath verifies basic reconstruction with exactly t shares.
func TestReconstructHappyPath(t *testing.T) {
	scheme, partials := generateAndSign(t, 4, 3, testMsg)

	// Use first 3 of 4 partials.
	reconstructed, err := scheme.Reconstruct(partials[:3])
	require.NoError(t, err)
	require.Len(t, reconstructed, 96) // compressed G2 point

	// Verify against group PK.
	err = scheme.Verify(reconstructed, testMsg)
	require.NoError(t, err)
}

// TestReconstructDeterministic verifies that different subsets of t shares
// produce the same threshold signature.
func TestReconstructDeterministic(t *testing.T) {
	scheme, partials := generateAndSign(t, 4, 3, testMsg)

	// Subset 1: indices {1, 2, 3}
	sig1, err := scheme.Reconstruct(partials[:3])
	require.NoError(t, err)

	// Subset 2: indices {1, 2, 4}
	sig2, err := scheme.Reconstruct([]PartialSig{partials[0], partials[1], partials[3]})
	require.NoError(t, err)

	// Subset 3: indices {1, 3, 4}
	sig3, err := scheme.Reconstruct([]PartialSig{partials[0], partials[2], partials[3]})
	require.NoError(t, err)

	// Subset 4: indices {2, 3, 4}
	sig4, err := scheme.Reconstruct([]PartialSig{partials[1], partials[2], partials[3]})
	require.NoError(t, err)

	// All subsets must produce the same signature.
	require.Equal(t, sig1, sig2)
	require.Equal(t, sig1, sig3)
	require.Equal(t, sig1, sig4)
}

// TestReconstructInsufficientShares verifies that fewer than t shares fail.
func TestReconstructInsufficientShares(t *testing.T) {
	scheme, partials := generateAndSign(t, 4, 3, testMsg)

	// Only 2 of 3 required.
	_, err := scheme.Reconstruct(partials[:2])
	require.ErrorIs(t, err, errInsufficientPartials)
}

// TestReconstructInvalidPartial verifies that a corrupted partial signature
// returns an error.
func TestReconstructInvalidPartial(t *testing.T) {
	scheme, partials := generateAndSign(t, 4, 3, testMsg)

	// Corrupt one partial.
	corrupted := make([]PartialSig, 3)
	copy(corrupted, partials[:3])
	corrupted[1].Sig = make([]byte, 96)
	corrupted[1].Sig[0] = 0xFF // invalid G2 point

	_, err := scheme.Reconstruct(corrupted)
	require.Error(t, err)
	require.ErrorIs(t, err, errInvalidPartialSig)
}

// TestReconstructDuplicateIndex verifies that duplicate signer indices fail.
func TestReconstructDuplicateIndex(t *testing.T) {
	scheme, partials := generateAndSign(t, 4, 3, testMsg)

	duplicated := []PartialSig{
		partials[0],
		partials[0], // same index twice
		partials[2],
	}

	_, err := scheme.Reconstruct(duplicated)
	require.Error(t, err)
	require.ErrorIs(t, err, errDuplicateIndex)
}

// TestVerifyAgainstGroupPK verifies that a reconstructed signature passes
// verification against the group public key.
func TestVerifyAgainstGroupPK(t *testing.T) {
	scheme, partials := generateAndSign(t, 4, 3, testMsg)

	sig, err := scheme.Reconstruct(partials[:3])
	require.NoError(t, err)

	err = scheme.Verify(sig, testMsg)
	require.NoError(t, err)
}

// TestVerifyRejectsWrongMessage verifies that a reconstructed signature fails
// verification against a different message.
func TestVerifyRejectsWrongMessage(t *testing.T) {
	scheme, partials := generateAndSign(t, 4, 3, testMsg)

	sig, err := scheme.Reconstruct(partials[:3])
	require.NoError(t, err)

	err = scheme.Verify(sig, []byte("wrong message"))
	require.Error(t, err)
}

// TestHasEnough verifies the quorum check.
func TestHasEnough(t *testing.T) {
	scheme := NewScheme(3, 4, nil)

	require.False(t, scheme.HasEnough(0))
	require.False(t, scheme.HasEnough(1))
	require.False(t, scheme.HasEnough(2))
	require.True(t, scheme.HasEnough(3))
	require.True(t, scheme.HasEnough(4))
}

// TestRemaining verifies the remaining count.
func TestRemaining(t *testing.T) {
	scheme := NewScheme(3, 4, nil)

	require.Equal(t, 3, scheme.Remaining(0))
	require.Equal(t, 2, scheme.Remaining(1))
	require.Equal(t, 1, scheme.Remaining(2))
	require.Equal(t, 0, scheme.Remaining(3))
	require.Equal(t, 0, scheme.Remaining(4))
}

// TestReconstructMoreThanThreshold verifies that providing more than t shares
// still works correctly.
func TestReconstructMoreThanThreshold(t *testing.T) {
	scheme, partials := generateAndSign(t, 4, 3, testMsg)

	// Use all 4 partials (more than threshold of 3).
	sig, err := scheme.Reconstruct(partials)
	require.NoError(t, err)

	err = scheme.Verify(sig, testMsg)
	require.NoError(t, err)

	// Should produce the same result as using exactly 3.
	sig3, err := scheme.Reconstruct(partials[:3])
	require.NoError(t, err)
	require.Equal(t, sig3, sig)
}
