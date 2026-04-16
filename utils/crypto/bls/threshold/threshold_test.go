// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package threshold

import (
	"testing"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"

	"github.com/stretchr/testify/require"
)

// TestGenerateKeysDeterministic verifies that the same seed produces the
// same keys and group PK.
func TestGenerateKeysDeterministic(t *testing.T) {
	scheme1, signers1, master1, err := GenerateKeys(4, 3, testSeed)
	require.NoError(t, err)

	scheme2, signers2, master2, err := GenerateKeys(4, 3, testSeed)
	require.NoError(t, err)

	// Same group PK.
	require.Equal(t, bls.PublicKeyToCompressedBytes(scheme1.GroupPK), bls.PublicKeyToCompressedBytes(scheme2.GroupPK))

	// Same master SK.
	require.Equal(t, master1.Serialize(), master2.Serialize())

	// Same signer public keys.
	for i := range signers1 {
		pk1 := bls.PublicKeyToCompressedBytes(signers1[i].PublicKey())
		pk2 := bls.PublicKeyToCompressedBytes(signers2[i].PublicKey())
		require.Equal(t, pk1, pk2)
	}
}

// TestGenerateKeysInvalidParams verifies that invalid parameters are rejected.
func TestGenerateKeysInvalidParams(t *testing.T) {
	_, _, _, err := GenerateKeys(4, 0, testSeed)
	require.Error(t, err)

	_, _, _, err = GenerateKeys(2, 3, testSeed)
	require.Error(t, err)
}
