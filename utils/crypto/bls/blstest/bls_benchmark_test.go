// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package blstest

import (
	"crypto/rand"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"

	blst "github.com/supranational/blst/bindings/go"
)

func newKey(require *require.Assertions) *blst.SecretKey {
	var ikm [32]byte
	_, err := rand.Read(ikm[:])
	require.NoError(err)
	sk := blst.KeyGen(ikm[:])
	ikm = [32]byte{} // zero out the ikm

	return sk
}

func sign(sk *blst.SecretKey, msg []byte) *bls.Signature {
	return new(bls.Signature).Sign(sk, msg, bls.CiphersuiteSignature)
}

func publicKey(sk *blst.SecretKey) *bls.PublicKey {
	return new(bls.PublicKey).From(sk)
}

func BenchmarkVerify(b *testing.B) {
	privateKey := newKey(require.New(b))
	publicKey := publicKey(privateKey)

	for _, messageSize := range BenchmarkSizes {
		b.Run(strconv.Itoa(messageSize), func(b *testing.B) {

			message := utils.RandomBytes(messageSize)
			signature := sign(privateKey, message)

			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				require.True(b, bls.Verify(publicKey, signature, message))
			}
		})
	}
}

func BenchmarkAggregatePublicKeys(b *testing.B) {
	keys := make([]*bls.PublicKey, BiggestBenchmarkSize)

	for i := range keys {
		privateKey := newKey(require.New(b))

		keys[i] = publicKey(privateKey)
	}

	for _, size := range BenchmarkSizes {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				_, err := bls.AggregatePublicKeys(keys[:size])
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
		bls.PublicKeyToCompressedBytes(pk)
	}
}

func BenchmarkPublicKeyFromCompressedBytes(b *testing.B) {
	sk := newKey(require.New(b))

	pk := publicKey(sk)
	pkBytes := bls.PublicKeyToCompressedBytes(pk)

	b.ResetTimer()
	for range b.N {
		_, _ = bls.PublicKeyFromCompressedBytes(pkBytes)
	}
}

func BenchmarkPublicKeyToUncompressedBytes(b *testing.B) {
	sk := newKey(require.New(b))

	pk := publicKey(sk)

	b.ResetTimer()
	for range b.N {
		bls.PublicKeyToUncompressedBytes(pk)
	}
}

func BenchmarkPublicKeyFromValidUncompressedBytes(b *testing.B) {
	sk := newKey(require.New(b))

	pk := publicKey(sk)
	pkBytes := bls.PublicKeyToUncompressedBytes(pk)

	b.ResetTimer()
	for range b.N {
		_ = bls.PublicKeyFromValidUncompressedBytes(pkBytes)
	}
}

func BenchmarkSignatureFromBytes(b *testing.B) {
	sk := newKey(require.New(b))

	message := utils.RandomBytes(32)
	signature := sign(sk, message)
	signatureBytes := bls.SignatureToBytes(signature)

	b.ResetTimer()
	for range b.N {
		_, _ = bls.SignatureFromBytes(signatureBytes)
	}
}
