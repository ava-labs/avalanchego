// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"crypto"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
)

func TestParse(t *testing.T) {
	require := require.New(t)

	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}
	chainID := ids.ID{4}

	tlsCert, err := staking.NewTLSCert()
	require.NoError(err)

	cert := staking.CertificateFromX509(tlsCert.Leaf)
	key := tlsCert.PrivateKey.(crypto.Signer)

	builtBlock, err := Build(
		parentID,
		timestamp,
		pChainHeight,
		cert,
		innerBlockBytes,
		chainID,
		key,
	)
	require.NoError(err)

	builtBlockBytes := builtBlock.Bytes()

	parsedBlockIntf, err := Parse(builtBlockBytes)
	require.NoError(err)

	parsedBlock, ok := parsedBlockIntf.(SignedBlock)
	require.True(ok)

	equal(require, chainID, builtBlock, parsedBlock)
}

func TestParseHeader(t *testing.T) {
	require := require.New(t)

	chainID := ids.ID{1}
	parentID := ids.ID{2}
	bodyID := ids.ID{3}

	builtHeader, err := BuildHeader(
		chainID,
		parentID,
		bodyID,
	)
	require.NoError(err)

	builtHeaderBytes := builtHeader.Bytes()

	parsedHeader, err := ParseHeader(builtHeaderBytes)
	require.NoError(err)

	equalHeader(require, builtHeader, parsedHeader)
}

func TestParseOption(t *testing.T) {
	require := require.New(t)

	parentID := ids.ID{1}
	innerBlockBytes := []byte{3}

	builtOption, err := BuildOption(parentID, innerBlockBytes)
	require.NoError(err)

	builtOptionBytes := builtOption.Bytes()

	parsedOption, err := Parse(builtOptionBytes)
	require.NoError(err)

	equalOption(require, builtOption, parsedOption)
}

func TestParseUnsigned(t *testing.T) {
	require := require.New(t)

	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}

	builtBlock, err := BuildUnsigned(parentID, timestamp, pChainHeight, innerBlockBytes)
	require.NoError(err)

	builtBlockBytes := builtBlock.Bytes()

	parsedBlockIntf, err := Parse(builtBlockBytes)
	require.NoError(err)

	parsedBlock, ok := parsedBlockIntf.(SignedBlock)
	require.True(ok)

	equal(require, ids.Empty, builtBlock, parsedBlock)
}

func TestParseGibberish(t *testing.T) {
	require := require.New(t)

	bytes := []byte{0, 1, 2, 3, 4, 5}

	_, err := Parse(bytes)
	require.ErrorIs(err, codec.ErrUnknownVersion)
}
