// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

func BenchmarkVerify(b *testing.B) {
	require := require.New(b)

	privateKey, err := NewPrivateKey()
	require.NoError(err)

	message := utils.RandomBytes(512)
	hash := hashing.ComputeHash256(message)

	publicKey := privateKey.PublicKey()
	signature, err := privateKey.SignHash(hash)
	require.NoError(err)

	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		require.True(publicKey.VerifyHash(hash, signature))
	}
}
