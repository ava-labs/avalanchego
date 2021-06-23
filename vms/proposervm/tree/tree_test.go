// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package proposervm

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
	"github.com/stretchr/testify/assert"
)

var (
	GenesisID = ids.GenerateTestID()
	Genesis   = &snowman.TestBlock{TestDecidable: choices.TestDecidable{
		IDV:     GenesisID,
		StatusV: choices.Accepted,
	}}
)

func TestAcceptSingleBlock(t *testing.T) {
	assert := assert.New(t)

	block := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: Genesis,
	}

	tr := New()

	contains := tr.Contains(block)
	assert.False(contains)

	tr.Add(block)

	contains = tr.Contains(block)
	assert.True(contains)

	err := tr.Accept(block)
	assert.NoError(err)
	assert.Equal(choices.Accepted, block.Status())
}

func TestAcceptBlockConflict(t *testing.T) {
	assert := assert.New(t)

	blockToAccept := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: Genesis,
	}

	blockToReject := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: Genesis,
	}

	tr := New()

	tr.Add(blockToAccept)
	tr.Add(blockToReject)

	contains := tr.Contains(blockToAccept)
	assert.True(contains)

	contains = tr.Contains(blockToReject)
	assert.True(contains)

	err := tr.Accept(blockToAccept)
	assert.NoError(err)
	assert.Equal(choices.Accepted, blockToAccept.Status())
	assert.Equal(choices.Rejected, blockToReject.Status())
}

func TestAcceptChainConflict(t *testing.T) {
	assert := assert.New(t)

	blockToAccept := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: Genesis,
	}

	blockToReject := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: Genesis,
	}

	blockToRejectChild := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: blockToReject,
	}

	tr := New()

	tr.Add(blockToAccept)
	tr.Add(blockToReject)
	tr.Add(blockToRejectChild)

	contains := tr.Contains(blockToAccept)
	assert.True(contains)

	contains = tr.Contains(blockToReject)
	assert.True(contains)

	contains = tr.Contains(blockToRejectChild)
	assert.True(contains)

	err := tr.Accept(blockToAccept)
	assert.NoError(err)
	assert.Equal(choices.Accepted, blockToAccept.Status())
	assert.Equal(choices.Rejected, blockToReject.Status())
	assert.Equal(choices.Rejected, blockToRejectChild.Status())
}
