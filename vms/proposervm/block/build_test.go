// (c) 2021, Ava Labs, Inc. All rights reserved.
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
	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	forkTime := timestamp.Add(-1 * time.Second)
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}

	assert := assert.New(t)

	tlsCert, err := staking.NewTLSCert()
	assert.NoError(err)

	cert := tlsCert.Leaf
	key := tlsCert.PrivateKey.(crypto.Signer)

	builtBlock, err := Build(parentID, timestamp, forkTime, pChainHeight, cert, innerBlockBytes, key)
	assert.NoError(err)

	assert.Equal(parentID, builtBlock.ParentID())
	assert.Equal(pChainHeight, builtBlock.PChainHeight())
	assert.Equal(timestamp, builtBlock.Timestamp())
	assert.Equal(innerBlockBytes, builtBlock.Block())

	err = builtBlock.Verify()
	assert.NoError(err)
}

func TestPreForkBuild(t *testing.T) {
	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	forkTime := timestamp.Add(10 * time.Second)
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}
	innerBlockID := ids.ID{10}
	assert := assert.New(t)

	builtBlock, err := BuildPreFork(parentID, timestamp, forkTime, innerBlockBytes, innerBlockID)
	assert.NoError(err)

	assert.Equal(parentID, builtBlock.ParentID())
	assert.Equal(pChainHeight, builtBlock.PChainHeight())
	assert.Equal(timestamp, builtBlock.Timestamp())
	assert.Equal(innerBlockBytes, builtBlock.Block())

	err = builtBlock.Verify()
	assert.NoError(err)
}
