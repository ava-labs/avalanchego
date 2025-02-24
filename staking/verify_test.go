// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package staking

import (
	"crypto"
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/hashing"
)

func BenchmarkSign(b *testing.B) {
	tlsCert, err := NewTLSCert()
	require.NoError(b, err)

	signer := tlsCert.PrivateKey.(crypto.Signer)
	msg := []byte("msg")
	hash := hashing.ComputeHash256(msg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := signer.Sign(rand.Reader, hash, crypto.SHA256)
		require.NoError(b, err)
	}
}

func BenchmarkVerify(b *testing.B) {
	tlsCert, err := NewTLSCert()
	require.NoError(b, err)

	signer := tlsCert.PrivateKey.(crypto.Signer)
	msg := []byte("msg")
	signature, err := signer.Sign(
		rand.Reader,
		hashing.ComputeHash256(msg),
		crypto.SHA256,
	)
	require.NoError(b, err)

	certBytes := tlsCert.Leaf.Raw
	cert, err := ParseCertificate(certBytes)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := CheckSignature(cert, msg, signature)
		require.NoError(b, err)
	}
}
