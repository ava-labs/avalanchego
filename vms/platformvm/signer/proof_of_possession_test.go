// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

func TestProofOfPossession(t *testing.T) {
	require := require.New(t)

	blsSigner, err := newProofOfPossession()
	require.NoError(err)
	require.NoError(blsSigner.Verify())
	require.NotNil(blsSigner.Key())

	blsSigner, err = newProofOfPossession()
	require.NoError(err)
	blsSigner.ProofOfPossession = [bls.SignatureLen]byte{}
	require.Error(blsSigner.Verify())

	blsSigner, err = newProofOfPossession()
	require.NoError(err)
	blsSigner.PublicKey = [bls.PublicKeyLen]byte{}
	require.Error(blsSigner.Verify())

	newBLSSigner, err := newProofOfPossession()
	require.NoError(err)
	newBLSSigner.ProofOfPossession = blsSigner.ProofOfPossession
	require.ErrorIs(newBLSSigner.Verify(), errInvalidProofOfPossession)
}

func TestNewProofOfPossessionDeterministic(t *testing.T) {
	require := require.New(t)

	sk, err := bls.NewSecretKey()
	require.NoError(err)

	blsSigner0 := NewProofOfPossession(sk)
	blsSigner1 := NewProofOfPossession(sk)
	require.Equal(blsSigner0, blsSigner1)
}

func newProofOfPossession() (*ProofOfPossession, error) {
	sk, err := bls.NewSecretKey()
	if err != nil {
		return nil, err
	}
	return NewProofOfPossession(sk), nil
}
