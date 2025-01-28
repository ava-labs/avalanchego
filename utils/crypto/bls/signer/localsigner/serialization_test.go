// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package localsigner

import (
	"testing"

	"github.com/stretchr/testify/require"
	blst "github.com/supranational/blst/bindings/go"

	"github.com/ava-labs/avalanchego/utils"
)

const SecretKeyLen = blst.BLST_SCALAR_BYTES

func TestSecretKeyFromBytesZero(t *testing.T) {
	require := require.New(t)

	var skArr [SecretKeyLen]byte
	skBytes := skArr[:]
	_, err := SecretKeyFromBytes(skBytes)
	require.ErrorIs(err, ErrFailedSecretKeyDeserialize)
}

func TestSecretKeyFromBytesWrongSize(t *testing.T) {
	require := require.New(t)

	skBytes := utils.RandomBytes(SecretKeyLen + 1)
	_, err := SecretKeyFromBytes(skBytes)
	require.ErrorIs(err, ErrFailedSecretKeyDeserialize)
}

func TestSecretKeyBytes(t *testing.T) {
	require := require.New(t)

	msg := utils.RandomBytes(1234)

	sk, err := NewSigner()
	require.NoError(err)
	sig := sk.Sign(msg)
	skBytes := sk.ToBytes()

	sk2, err := SecretKeyFromBytes(skBytes)
	require.NoError(err)
	sig2 := sk2.Sign(msg)
	sk2Bytes := sk2.ToBytes()

	require.Equal(sk, sk2)
	require.Equal(skBytes, sk2Bytes)
	require.Equal(sig, sig2)
}
