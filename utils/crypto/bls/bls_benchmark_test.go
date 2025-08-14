// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/blstest"
)

func BenchmarkVerify(b *testing.B) {
	privateKey := newKey(require.New(b))
	publicKey := publicKey(privateKey)

	for _, messageSize := range blstest.BenchmarkSizes {
		b.Run(strconv.Itoa(messageSize), func(b *testing.B) {
			message := utils.RandomBytes(messageSize)
			signature := sign(privateKey, message)

			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				require.True(b, Verify(publicKey, signature, message))
			}
		})
	}
}

func BenchmarkAggregatePublicKeys(b *testing.B) {
	keys := make([]*PublicKey, blstest.BiggestBenchmarkSize)

	for i := range keys {
		privateKey := newKey(require.New(b))

		keys[i] = publicKey(privateKey)
	}

	for _, size := range blstest.BenchmarkSizes {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				_, err := AggregatePublicKeys(keys[:size])
				require.NoError(b, err)
			}
		})
	}
}

func BenchmarkPublicKeyToCompressedBytes(b *testing.B) {
	sk := newKey(require.New(b))

	pk := publicKey(sk)

	b.ResetTimer()
	for range b.N {
		PublicKeyToCompressedBytes(pk)
	}
}

func BenchmarkPublicKeyFromCompressedBytes(b *testing.B) {
	sk := newKey(require.New(b))

	pk := publicKey(sk)
	pkBytes := PublicKeyToCompressedBytes(pk)

	b.ResetTimer()
	for range b.N {
		_, _ = PublicKeyFromCompressedBytes(pkBytes)
	}
}

func BenchmarkPublicKeyToUncompressedBytes(b *testing.B) {
	sk := newKey(require.New(b))

	pk := publicKey(sk)

	b.ResetTimer()
	for range b.N {
		PublicKeyToUncompressedBytes(pk)
	}
}

func BenchmarkPublicKeyFromValidUncompressedBytes(b *testing.B) {
	sk := newKey(require.New(b))

	pk := publicKey(sk)
	pkBytes := PublicKeyToUncompressedBytes(pk)

	b.ResetTimer()
	for range b.N {
		_ = PublicKeyFromValidUncompressedBytes(pkBytes)
	}
}

func BenchmarkSignatureFromBytes(b *testing.B) {
	sk := newKey(require.New(b))

	message := utils.RandomBytes(32)
	signature := sign(sk, message)
	signatureBytes := SignatureToBytes(signature)

	b.ResetTimer()
	for range b.N {
		_, _ = SignatureFromBytes(signatureBytes)
	}
}
