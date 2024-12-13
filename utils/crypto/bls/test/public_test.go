// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bls_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/ava-labs/avalanchego/utils/crypto/bls"
	"github.com/ava-labs/avalanchego/utils/crypto/bls/signers/local"
)

func TestPublicKeyFromCompressedBytesWrongSize(t *testing.T) {
	require := require.New(t)

	pkBytes := utils.RandomBytes(bls.PublicKeyLen + 1)
	_, err := bls.PublicKeyFromCompressedBytes(pkBytes)
	require.ErrorIs(err, bls.ErrFailedPublicKeyDecompress)
}

func TestPublicKeyBytes(t *testing.T) {
	require := require.New(t)

	sk, err := local.NewSigner()
	require.NoError(err)

	pk := sk.PublicKey()
	pkBytes := bls.PublicKeyToCompressedBytes(pk)

	pk2, err := bls.PublicKeyFromCompressedBytes(pkBytes)
	require.NoError(err)
	pk2Bytes := bls.PublicKeyToCompressedBytes(pk2)

	require.Equal(pk, pk2)
	require.Equal(pkBytes, pk2Bytes)
}

func TestAggregatePublicKeysNoop(t *testing.T) {
	require := require.New(t)

	sk, err := local.NewSigner()
	require.NoError(err)

	pk := sk.PublicKey()
	pkBytes := bls.PublicKeyToCompressedBytes(pk)

	aggPK, err := bls.AggregatePublicKeys([]*bls.PublicKey{pk})
	require.NoError(err)

	aggPKBytes := bls.PublicKeyToCompressedBytes(aggPK)
	require.NoError(err)

	require.Equal(pk, aggPK)
	require.Equal(pkBytes, aggPKBytes)
}
