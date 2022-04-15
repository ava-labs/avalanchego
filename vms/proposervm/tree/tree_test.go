// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tree

import (
	"testing"

	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/snow/choices"
	"github.com/chain4travel/caminogo/snow/consensus/snowman"
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
		ParentV: Genesis.ID(),
	}

	tr := New()

	_, contains := tr.Get(block)
	assert.False(contains)

	tr.Add(block)

	_, contains = tr.Get(block)
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
		ParentV: Genesis.ID(),
	}

	blockToReject := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.ID(),
	}

	tr := New()

	tr.Add(blockToAccept)
	tr.Add(blockToReject)

	_, contains := tr.Get(blockToAccept)
	assert.True(contains)

	_, contains = tr.Get(blockToReject)
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
		ParentV: Genesis.ID(),
	}

	blockToReject := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.ID(),
	}

	blockToRejectChild := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: blockToReject.ID(),
	}

	tr := New()

	tr.Add(blockToAccept)
	tr.Add(blockToReject)
	tr.Add(blockToRejectChild)

	_, contains := tr.Get(blockToAccept)
	assert.True(contains)

	_, contains = tr.Get(blockToReject)
	assert.True(contains)

	_, contains = tr.Get(blockToRejectChild)
	assert.True(contains)

	err := tr.Accept(blockToAccept)
	assert.NoError(err)
	assert.Equal(choices.Accepted, blockToAccept.Status())
	assert.Equal(choices.Rejected, blockToReject.Status())
	assert.Equal(choices.Rejected, blockToRejectChild.Status())
}
