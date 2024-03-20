// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
)

func TestPublicKeyFromCompressedBytesWrongSize(t *testing.T) {
	require := require.New(t)

	pkBytes := utils.RandomBytes(PublicKeyLen + 1)
	_, err := PublicKeyFromCompressedBytes(pkBytes)
	require.ErrorIs(err, ErrFailedPublicKeyDecompress)
}

func TestPublicKeyBytes(t *testing.T) {
	require := require.New(t)

	sk, err := NewSecretKey()
	require.NoError(err)

	pk := PublicFromSecretKey(sk)
	pkBytes := PublicKeyToCompressedBytes(pk)

	pk2, err := PublicKeyFromCompressedBytes(pkBytes)
	require.NoError(err)
	pk2Bytes := PublicKeyToCompressedBytes(pk2)

	require.Equal(pk, pk2)
	require.Equal(pkBytes, pk2Bytes)
}

func TestAggregatePublicKeysNoop(t *testing.T) {
	require := require.New(t)

	sk, err := NewSecretKey()
	require.NoError(err)

	pk := PublicFromSecretKey(sk)
	pkBytes := PublicKeyToCompressedBytes(pk)

	aggPK, err := AggregatePublicKeys([]*PublicKey{pk})
	require.NoError(err)

	aggPKBytes := PublicKeyToCompressedBytes(aggPK)
	require.NoError(err)

	require.Equal(pk, aggPK)
	require.Equal(pkBytes, aggPKBytes)
}
