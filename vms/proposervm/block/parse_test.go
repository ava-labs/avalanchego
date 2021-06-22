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

func TestParse(t *testing.T) {
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

	builtBlockBytes := builtBlock.Bytes()

	parsedBlock, err := Parse(builtBlockBytes)
	assert.NoError(err)

	assert.Equal(builtBlock.ID(), parsedBlock.ID())
	assert.Equal(builtBlock.ParentID(), parsedBlock.ParentID())
	assert.Equal(builtBlock.PChainHeight(), parsedBlock.PChainHeight())
	assert.Equal(builtBlock.Timestamp(), parsedBlock.Timestamp())
	assert.Equal(builtBlock.Block(), parsedBlock.Block())
	assert.Equal(builtBlock.Proposer(), parsedBlock.Proposer())
	assert.Equal(builtBlockBytes, parsedBlock.Bytes())

	err = parsedBlock.Verify()
	assert.NoError(err)
}
