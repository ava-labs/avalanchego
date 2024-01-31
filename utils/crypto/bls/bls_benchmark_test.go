// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
)

var sizes = []int{
	2,
	4,
	8,
	16,
	32,
	64,
	128,
	256,
	512,
	1024,
	2048,
	4096,
}

func BenchmarkSign(b *testing.B) {
	privateKey, err := NewSecretKey()
	require.NoError(b, err)
	for _, messageSize := range sizes {
		b.Run(strconv.Itoa(messageSize), func(b *testing.B) {
			message := utils.RandomBytes(messageSize)

			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				_ = Sign(privateKey, message)
			}
		})
	}
}

func BenchmarkVerify(b *testing.B) {
	privateKey, err := NewSecretKey()
	require.NoError(b, err)
	publicKey := PublicFromSecretKey(privateKey)

	for _, messageSize := range sizes {
		b.Run(strconv.Itoa(messageSize), func(b *testing.B) {
			message := utils.RandomBytes(messageSize)
			signature := Sign(privateKey, message)

			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				require.True(b, Verify(publicKey, signature, message))
			}
		})
	}
}

func BenchmarkAggregatePublicKeys(b *testing.B) {
	keys := make([]*PublicKey, 4096)
	for i := range keys {
		privateKey, err := NewSecretKey()
		require.NoError(b, err)

		keys[i] = PublicFromSecretKey(privateKey)
	}

	for _, size := range sizes {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				_, err := AggregatePublicKeys(keys[:size])
				require.NoError(b, err)
			}
		})
	}
}
