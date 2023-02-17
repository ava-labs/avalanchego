// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package secp256k1

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/hashing"
)

// NumVerifies is the number of verifications to run per operation
const NumVerifies = 1

var (
	hashes [][]byte

	keys []*PublicKey
	sigs [][]byte
)

func init() {
	// Setup hashes:
	bytes := ids.ID{}
	for i := uint64(0); i < NumVerifies; i++ {
		bytes[i%32]++
		hash := hashing.ComputeHash256(bytes[:])
		hashes = append(hashes, hash)
	}

	// Setup signatures:
	f := &Factory{}
	for i := uint64(0); i < NumVerifies; i++ {
		privateKey, err := f.NewPrivateKey()
		if err != nil {
			panic(err)
		}

		publicKey := privateKey.PublicKey()
		sig, err := privateKey.SignHash(hashes[i])
		if err != nil {
			panic(err)
		}

		keys = append(keys, publicKey)
		sigs = append(sigs, sig)
	}
}

// BenchmarkSECP256k1Verify runs the benchmark with SECP256K1 keys
func BenchmarkSECP256k1Verify(b *testing.B) {
	require := require.New(b)

	for n := 0; n < b.N; n++ {
		for i := 0; i < NumVerifies; i++ {
			require.True(keys[i].VerifyHash(hashes[i], sigs[i]))
		}
	}
}
