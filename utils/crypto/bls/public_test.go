// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
)

func TestPublicKeyFromBytesWrongSize(t *testing.T) {
	require := require.New(t)

	pkBytes := utils.RandomBytes(PublicKeyLen + 1)
	_, err := PublicKeyFromBytes(pkBytes)
	require.ErrorIs(err, errFailedPublicKeyDecompress)
}

func TestPublicKeyBytes(t *testing.T) {
	require := require.New(t)

	sk, err := NewSecretKey()
	require.NoError(err)

	pk := PublicFromSecretKey(sk)
	pkBytes := PublicKeyToBytes(pk)

	pk2, err := PublicKeyFromBytes(pkBytes)
	require.NoError(err)
	pk2Bytes := PublicKeyToBytes(pk2)

	require.Equal(pk, pk2)
	require.Equal(pkBytes, pk2Bytes)
}

func TestAggregatePublicKeysNoop(t *testing.T) {
	require := require.New(t)

	sk, err := NewSecretKey()
	require.NoError(err)

	pk := PublicFromSecretKey(sk)
	pkBytes := PublicKeyToBytes(pk)

	aggPK, err := AggregatePublicKeys([]*PublicKey{pk})
	require.NoError(err)

	aggPKBytes := PublicKeyToBytes(aggPK)
	require.NoError(err)

	require.Equal(pk, aggPK)
	require.Equal(pkBytes, aggPKBytes)
}
