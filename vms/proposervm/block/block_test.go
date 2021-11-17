// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package block

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/ava-labs/avalanchego/ids"
)

func equal(assert *assert.Assertions, chainID ids.ID, want, have SignedBlock) {
	assert.Equal(want.ID(), have.ID())
	assert.Equal(want.ParentID(), have.ParentID())
	assert.Equal(want.PChainHeight(), have.PChainHeight())
	assert.Equal(want.Timestamp(), have.Timestamp())
	assert.Equal(want.Block(), have.Block())
	assert.Equal(want.Proposer(), have.Proposer())
	assert.Equal(want.Bytes(), have.Bytes())
	assert.Equal(want.Verify(false, chainID), have.Verify(false, chainID))
	assert.Equal(want.Verify(true, chainID), have.Verify(true, chainID))
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

	err = builtBlock.Verify(false, ids.Empty)
	assert.Error(err)

	err = builtBlock.Verify(true, ids.Empty)
	assert.Error(err)
}
