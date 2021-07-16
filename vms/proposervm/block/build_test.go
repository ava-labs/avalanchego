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
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}

	assert := assert.New(t)

	tlsCert, err := staking.NewTLSCert()
	assert.NoError(err)

	cert := tlsCert.Leaf
	key := tlsCert.PrivateKey.(crypto.Signer)

	builtBlock, err := Build(parentID, timestamp, pChainHeight, cert, innerBlockBytes, key)
	assert.NoError(err)

	assert.Equal(parentID, builtBlock.ParentID())
	assert.Equal(pChainHeight, builtBlock.PChainHeight())
	assert.Equal(timestamp, builtBlock.Timestamp())
	assert.Equal(innerBlockBytes, builtBlock.Block())

	err = builtBlock.Verify()
	assert.NoError(err)
}

func TestBuildCorrectTimestamp(t *testing.T) {
	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}
	skewedTimestamp := timestamp.Add(time.Millisecond)

	assert := assert.New(t)

	tlsCert, err := staking.NewTLSCert()
	assert.NoError(err)

	cert := tlsCert.Leaf
	key := tlsCert.PrivateKey.(crypto.Signer)

	builtBlock, err := Build(parentID, skewedTimestamp, pChainHeight, cert, innerBlockBytes, key)
	assert.NoError(err)

	assert.Equal(parentID, builtBlock.ParentID())
	assert.Equal(pChainHeight, builtBlock.PChainHeight())
	assert.Equal(timestamp, builtBlock.Timestamp())
	assert.Equal(innerBlockBytes, builtBlock.Block())

	err = builtBlock.Verify()
	assert.NoError(err)
}
