// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tree

import (
	"context"
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

	require.NoError(tr.Accept(context.Background(), block))
	require.Equal(choices.Accepted, block.Status())

	_, contains = tr.Get(block)
	require.False(contains)
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

	// add conflicting blocks
	tr.Add(blockToAccept)
	_, contains := tr.Get(blockToAccept)
	require.True(contains)

	tr.Add(blockToReject)
	_, contains = tr.Get(blockToReject)
	require.True(contains)

	// accept one of them
	require.NoError(tr.Accept(context.Background(), blockToAccept))

	// check their statuses and that they are removed from the tree
	require.Equal(choices.Accepted, blockToAccept.Status())
	_, contains = tr.Get(blockToAccept)
	require.False(contains)

	require.Equal(choices.Rejected, blockToReject.Status())
	_, contains = tr.Get(blockToReject)
	require.False(contains)
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

	// add conflicting blocks.
	tr.Add(blockToAccept)
	_, contains := tr.Get(blockToAccept)
	require.True(contains)

	tr.Add(blockToReject)
	_, contains = tr.Get(blockToReject)
	require.True(contains)

	tr.Add(blockToRejectChild)
	_, contains = tr.Get(blockToRejectChild)
	require.True(contains)

	// accept one of them
	require.NoError(tr.Accept(context.Background(), blockToAccept))

	// check their statuses and whether they are removed from tree
	require.Equal(choices.Accepted, blockToAccept.Status())
	_, contains = tr.Get(blockToAccept)
	require.False(contains)

	require.Equal(choices.Rejected, blockToReject.Status())
	_, contains = tr.Get(blockToReject)
	require.False(contains)

	require.Equal(choices.Rejected, blockToRejectChild.Status())
	_, contains = tr.Get(blockToRejectChild)
	require.False(contains)
}
