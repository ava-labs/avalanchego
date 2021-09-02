// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
)

func equal(assert *assert.Assertions, chainID ids.ID, want, have Block) {
	assert.Equal(want.ID(), have.ID())
	assert.Equal(want.ParentID(), have.ParentID())
	assert.Equal(want.PChainHeight(), have.PChainHeight())
	assert.Equal(want.Timestamp(), have.Timestamp())
	assert.Equal(want.Block(), have.Block())
	assert.Equal(want.Proposer(), have.Proposer())
	assert.Equal(want.Bytes(), have.Bytes())
	assert.Equal(want.Verify(chainID), have.Verify(chainID))
}
