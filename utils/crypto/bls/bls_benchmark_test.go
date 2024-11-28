// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
)

var (
	sizes = []int{
		1,
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
		8192,
		16384,
		32768,
	}
	biggestSize = sizes[len(sizes)-1]
)

func BenchmarkSign(b *testing.B) {
	privateKey, err := NewSigner()
	require.NoError(b, err)
	for _, messageSize := range sizes {
		b.Run(strconv.Itoa(messageSize), func(b *testing.B) {
			message := utils.RandomBytes(messageSize)

			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				_ = privateKey.Sign(message)
			}
		})
	}
}

func BenchmarkVerify(b *testing.B) {
	privateKey, err := NewSigner()
	require.NoError(b, err)
	publicKey := privateKey.PublicKey()

	for _, messageSize := range sizes {
		b.Run(strconv.Itoa(messageSize), func(b *testing.B) {
			message := utils.RandomBytes(messageSize)
			signature := privateKey.Sign(message)

			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				require.True(b, Verify(publicKey, signature, message))
			}
		})
	}
}

func BenchmarkAggregatePublicKeys(b *testing.B) {
	keys := make([]*PublicKey, biggestSize)
	for i := range keys {
		privateKey, err := NewSigner()
		require.NoError(b, err)

		keys[i] = privateKey.PublicKey()
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

func BenchmarkPublicKeyToCompressedBytes(b *testing.B) {
	sk, err := NewSigner()
	require.NoError(b, err)

	pk := sk.PublicKey()

	b.ResetTimer()
	for range b.N {
		PublicKeyToCompressedBytes(pk)
	}
}

func BenchmarkPublicKeyFromCompressedBytes(b *testing.B) {
	sk, err := NewSigner()
	require.NoError(b, err)

	pk := sk.PublicKey()
	pkBytes := PublicKeyToCompressedBytes(pk)

	b.ResetTimer()
	for range b.N {
		_, _ = PublicKeyFromCompressedBytes(pkBytes)
	}
}

func BenchmarkPublicKeyToUncompressedBytes(b *testing.B) {
	sk, err := NewSigner()
	require.NoError(b, err)

	pk := sk.PublicKey()

	b.ResetTimer()
	for range b.N {
		PublicKeyToUncompressedBytes(pk)
	}
}

func BenchmarkPublicKeyFromValidUncompressedBytes(b *testing.B) {
	sk, err := NewSigner()
	require.NoError(b, err)

	pk := sk.PublicKey()
	pkBytes := PublicKeyToUncompressedBytes(pk)

	b.ResetTimer()
	for range b.N {
		_ = PublicKeyFromValidUncompressedBytes(pkBytes)
	}
}

func BenchmarkSignatureFromBytes(b *testing.B) {
	privateKey, err := NewSigner()
	require.NoError(b, err)

	message := utils.RandomBytes(32)
	signature := privateKey.Sign(message)
	signatureBytes := SignatureToBytes(signature)

	b.ResetTimer()
	for range b.N {
		_, _ = SignatureFromBytes(signatureBytes)
	}
}
