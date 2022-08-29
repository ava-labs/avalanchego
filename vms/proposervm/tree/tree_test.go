// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tree

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

var (
	GenesisID = ids.GenerateTestID()
	Genesis   = &snowman.TestBlock{TestDecidable: choices.TestDecidable{
		IDV:     GenesisID,
		StatusV: choices.Accepted,
	}}
)

func TestAcceptSingleBlock(t *testing.T) {
	require := require.New(t)

	block := &snowman.TestBlock{
		TestDecidable: choices.TestDecidable{
			IDV:     ids.GenerateTestID(),
			StatusV: choices.Processing,
		},
		ParentV: Genesis.ID(),
	}

	tr := New()

	_, contains := tr.Get(block)
	require.False(contains)

	tr.Add(block)

	_, contains = tr.Get(block)
	require.True(contains)

	err := tr.Accept(block)
	require.NoError(err)
	require.Equal(choices.Accepted, block.Status())
}

func TestAcceptBlockConflict(t *testing.T) {
	require := require.New(t)

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
	require.True(contains)

	_, contains = tr.Get(blockToReject)
	require.True(contains)

	err := tr.Accept(blockToAccept)
	require.NoError(err)
	require.Equal(choices.Accepted, blockToAccept.Status())
	require.Equal(choices.Rejected, blockToReject.Status())
}

func TestAcceptChainConflict(t *testing.T) {
	require := require.New(t)

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
	require.True(contains)

	_, contains = tr.Get(blockToReject)
	require.True(contains)

	_, contains = tr.Get(blockToRejectChild)
	require.True(contains)

	err := tr.Accept(blockToAccept)
	require.NoError(err)
	require.Equal(choices.Accepted, blockToAccept.Status())
	require.Equal(choices.Rejected, blockToReject.Status())
	require.Equal(choices.Rejected, blockToRejectChild.Status())
}
