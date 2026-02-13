// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package localsigner

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"

	blst "github.com/supranational/blst/bindings/go"
)

const SecretKeyLen = blst.BLST_SCALAR_BYTES

func TestSecretKeyFromBytesZero(t *testing.T) {
	require := require.New(t)

	var skArr [SecretKeyLen]byte
	skBytes := skArr[:]
	_, err := FromBytes(skBytes)
	require.ErrorIs(err, ErrFailedSecretKeyDeserialize)
}

func TestSecretKeyFromBytesWrongSize(t *testing.T) {
	require := require.New(t)

	skBytes := utils.RandomBytes(SecretKeyLen + 1)
	_, err := FromBytes(skBytes)
	require.ErrorIs(err, ErrFailedSecretKeyDeserialize)
}

func TestSecretKeyBytes(t *testing.T) {
	require := require.New(t)

	msg := utils.RandomBytes(1234)

	sk, err := New()
	require.NoError(err)
	sig, err := sk.Sign(msg)
	require.NoError(err)
	skBytes := sk.ToBytes()

	sk2, err := FromBytes(skBytes)
	require.NoError(err)
	sig2, err := sk2.Sign(msg)
	require.NoError(err)
	sk2Bytes := sk2.ToBytes()

	require.Equal(sk, sk2)
	require.Equal(skBytes, sk2Bytes)
	require.Equal(sig, sig2)
}
