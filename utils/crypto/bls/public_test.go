// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls

import (
	"encoding/base64"
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

	pkBytes, err := base64.StdEncoding.DecodeString("h5qt9SPxaCo+vOx6sn+QkkpP7Y40Yja7SEAs2MGb/mZT7oKTWgLogjy5c4/wWIGC")
	require.NoError(err)

	pk, err := PublicKeyFromCompressedBytes(pkBytes)
	require.NoError(err)

	pk2Bytes := PublicKeyToCompressedBytes(pk)

	require.Equal(pkBytes, pk2Bytes)
}

func TestAggregatePublicKeysNoop(t *testing.T) {
	require := require.New(t)

	pkBytes, err := base64.StdEncoding.DecodeString("h5qt9SPxaCo+vOx6sn+QkkpP7Y40Yja7SEAs2MGb/mZT7oKTWgLogjy5c4/wWIGC")
	require.NoError(err)

	pk, err := PublicKeyFromCompressedBytes(pkBytes)
	require.NoError(err)

	aggPK, err := AggregatePublicKeys([]*PublicKey{pk})
	require.NoError(err)

	aggPKBytes := PublicKeyToCompressedBytes(aggPK)
	require.NoError(err)

	require.Equal(pk, aggPK)
	require.Equal(pkBytes, aggPKBytes)
}
