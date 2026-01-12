// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signer/localsigner"
)

func TestProofOfPossession(t *testing.T) {
	require := require.New(t)

	blsPOP, err := newProofOfPossession()
	require.NoError(err)
	require.NoError(blsPOP.Verify())
	require.NotNil(blsPOP.Key())

	blsPOP, err = newProofOfPossession()
	require.NoError(err)
	blsPOP.ProofOfPossession = [bls.SignatureLen]byte{}
	err = blsPOP.Verify()
	require.ErrorIs(err, bls.ErrFailedSignatureDecompress)

	blsPOP, err = newProofOfPossession()
	require.NoError(err)
	blsPOP.PublicKey = [bls.PublicKeyLen]byte{}
	err = blsPOP.Verify()
	require.ErrorIs(err, bls.ErrFailedPublicKeyDecompress)

	newBLSPOP, err := newProofOfPossession()
	require.NoError(err)
	newBLSPOP.ProofOfPossession = blsPOP.ProofOfPossession
	err = newBLSPOP.Verify()
	require.ErrorIs(err, ErrInvalidProofOfPossession)
}

func TestNewProofOfPossessionDeterministic(t *testing.T) {
	require := require.New(t)

	sk, err := localsigner.New()
	require.NoError(err)

	blsPOP0, err := NewProofOfPossession(sk)
	require.NoError(err)
	blsPOP1, err := NewProofOfPossession(sk)
	require.NoError(err)
	require.Equal(blsPOP0, blsPOP1)
}

func BenchmarkProofOfPossessionVerify(b *testing.B) {
	pop, err := newProofOfPossession()
	require.NoError(b, err)

	b.ResetTimer()
	for range b.N {
		_ = pop.Verify()
	}
}

func newProofOfPossession() (*ProofOfPossession, error) {
	sk, err := localsigner.New()
	if err != nil {
		return nil, err
	}
	return NewProofOfPossession(sk)
}
