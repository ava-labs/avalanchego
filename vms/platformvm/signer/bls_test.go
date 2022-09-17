// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package signer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/crypto/bls"
)

func TestBLS(t *testing.T) {
	require := require.New(t)

	blsSigner, err := newBLS()
	require.NoError(err)
	require.NoError(blsSigner.Verify())
	require.NotNil(blsSigner.Key())

	blsSigner, err = newBLS()
	require.NoError(err)
	blsSigner.ProofOfPossession = [bls.SignatureLen]byte{}
	require.Error(blsSigner.Verify())

	blsSigner, err = newBLS()
	require.NoError(err)
	blsSigner.PublicKey = [bls.PublicKeyLen]byte{}
	require.Error(blsSigner.Verify())

	newBLSSigner, err := newBLS()
	require.NoError(err)
	newBLSSigner.ProofOfPossession = blsSigner.ProofOfPossession
	require.ErrorIs(newBLSSigner.Verify(), errInvalidProofOfPossession)
}

func newBLS() (*BLS, error) {
	sk, err := bls.NewSecretKey()
	if err != nil {
		return nil, err
	}
	return NewBLS(sk), nil
}
