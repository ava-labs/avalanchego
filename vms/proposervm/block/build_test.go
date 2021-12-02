// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"crypto"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
)

func TestBuild(t *testing.T) {
	assert := assert.New(t)

	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}
	chainID := ids.ID{4}

	tlsCert, err := staking.NewTLSCert()
	assert.NoError(err)

	cert := tlsCert.Leaf
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
	assert.NoError(err)

	assert.Equal(parentID, builtBlock.ParentID())
	assert.Equal(pChainHeight, builtBlock.PChainHeight())
	assert.Equal(timestamp, builtBlock.Timestamp())
	assert.Equal(innerBlockBytes, builtBlock.Block())

	err = builtBlock.Verify(true, chainID)
	assert.NoError(err)

	err = builtBlock.Verify(false, chainID)
	assert.Error(err)
}

func TestBuildUnsigned(t *testing.T) {
	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}

	assert := assert.New(t)

	builtBlock, err := BuildUnsigned(parentID, timestamp, pChainHeight, innerBlockBytes)
	assert.NoError(err)

	assert.Equal(parentID, builtBlock.ParentID())
	assert.Equal(pChainHeight, builtBlock.PChainHeight())
	assert.Equal(timestamp, builtBlock.Timestamp())
	assert.Equal(innerBlockBytes, builtBlock.Block())
	assert.Equal(ids.ShortEmpty, builtBlock.Proposer())

	err = builtBlock.Verify(false, ids.Empty)
	assert.NoError(err)

	err = builtBlock.Verify(true, ids.Empty)
	assert.Error(err)
}

func TestBuildHeader(t *testing.T) {
	assert := assert.New(t)

	chainID := ids.ID{1}
	parentID := ids.ID{2}
	bodyID := ids.ID{3}

	builtHeader, err := BuildHeader(
		chainID,
		parentID,
		bodyID,
	)
	assert.NoError(err)

	assert.Equal(chainID, builtHeader.ChainID())
	assert.Equal(parentID, builtHeader.ParentID())
	assert.Equal(bodyID, builtHeader.BodyID())
}

func TestBuildOption(t *testing.T) {
	assert := assert.New(t)

	parentID := ids.ID{1}
	innerBlockBytes := []byte{3}

	builtOption, err := BuildOption(parentID, innerBlockBytes)
	assert.NoError(err)

	assert.Equal(parentID, builtOption.ParentID())
	assert.Equal(innerBlockBytes, builtOption.Block())
}
