// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
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
	require.ErrorIs(err, errInvalidProofOfPossession)
}

func TestNewProofOfPossessionDeterministic(t *testing.T) {
	require := require.New(t)

	sk, err := bls.NewSecretKey()
	require.NoError(err)

	blsPOP0 := NewProofOfPossession(sk)
	blsPOP1 := NewProofOfPossession(sk)
	require.Equal(blsPOP0, blsPOP1)
}

func newProofOfPossession() (*ProofOfPossession, error) {
	sk, err := bls.NewSecretKey()
	if err != nil {
		return nil, err
	}
	return NewProofOfPossession(sk), nil
}
