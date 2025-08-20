// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"crypto"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
)

func TestBuild(t *testing.T) {
	require := require.New(t)

	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	pChainEpoch := PChainEpoch{
		Height:    2,
		Epoch:     0,
		StartTime: time.Unix(123, 0),
	}
	innerBlockBytes := []byte{3}
	chainID := ids.ID{4}

	tlsCert, err := staking.NewTLSCert()
	require.NoError(err)

	cert, err := staking.ParseCertificate(tlsCert.Leaf.Raw)
	require.NoError(err)
	key := tlsCert.PrivateKey.(crypto.Signer)
	nodeID := ids.NodeIDFromCert(cert)

	builtBlock, err := Build(
		parentID,
		timestamp,
		pChainHeight,
		pChainEpoch,
		cert,
		innerBlockBytes,
		chainID,
		key,
	)
	require.NoError(err)

	require.Equal(parentID, builtBlock.ParentID())
	require.Equal(pChainHeight, builtBlock.PChainHeight())
	require.Equal(timestamp, builtBlock.Timestamp())
	require.Equal(innerBlockBytes, builtBlock.Block())
	require.Equal(nodeID, builtBlock.Proposer())
}

func TestBuildUnsigned(t *testing.T) {
	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	pChainEpoch := PChainEpoch{
		Height:    2,
		Epoch:     0,
		StartTime: time.Unix(123, 0),
	}
	innerBlockBytes := []byte{3}

	require := require.New(t)

	builtBlock, err := BuildUnsigned(parentID, timestamp, pChainHeight, pChainEpoch, innerBlockBytes)
	require.NoError(err)

	require.Equal(parentID, builtBlock.ParentID())
	require.Equal(pChainHeight, builtBlock.PChainHeight())
	require.Equal(timestamp, builtBlock.Timestamp())
	require.Equal(innerBlockBytes, builtBlock.Block())
	require.Equal(ids.EmptyNodeID, builtBlock.Proposer())
}

func TestBuildHeader(t *testing.T) {
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

	require.Equal(chainID, builtHeader.ChainID())
	require.Equal(parentID, builtHeader.ParentID())
	require.Equal(bodyID, builtHeader.BodyID())
}

func TestBuildOption(t *testing.T) {
	require := require.New(t)

	parentID := ids.ID{1}
	innerBlockBytes := []byte{3}

	builtOption, err := BuildOption(parentID, innerBlockBytes)
	require.NoError(err)

	require.Equal(parentID, builtOption.ParentID())
	require.Equal(innerBlockBytes, builtOption.Block())
}
