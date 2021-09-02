// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/stretchr/testify/assert"
)

func equal(assert *assert.Assertions, want, have Block) {
	assert.Equal(want.ID(), have.ID())
	assert.Equal(want.ParentID(), have.ParentID())
	assert.Equal(want.PChainHeight(), have.PChainHeight())
	assert.Equal(want.Timestamp(), have.Timestamp())
	assert.Equal(want.Block(), have.Block())
	assert.Equal(want.Proposer(), have.Proposer())
	assert.Equal(want.Bytes(), have.Bytes())
	assert.Equal(want.Verify(), have.Verify())
}

func TestVerifyNoCertWithSignature(t *testing.T) {
	parentID := ids.ID{1}
	timestamp := time.Unix(123, 0)
	pChainHeight := uint64(2)
	innerBlockBytes := []byte{3}

	assert := assert.New(t)

	builtBlockIntf, err := BuildUnsigned(parentID, timestamp, pChainHeight, innerBlockBytes)
	assert.NoError(err)

	builtBlock := builtBlockIntf.(*statelessBlock)
	builtBlock.Signature = []byte{0}

	err = builtBlock.Verify()
	assert.Error(err)
}
