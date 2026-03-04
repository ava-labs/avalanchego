// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tree

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
)

func TestAcceptSingleBlock(t *testing.T) {
	require := require.New(t)

	tr := New()

	block := snowmantest.BuildChild(snowmantest.Genesis)
	_, contains := tr.Get(block)
	require.False(contains)

	tr.Add(block)

	_, contains = tr.Get(block)
	require.True(contains)

	require.NoError(tr.Accept(t.Context(), block))
	require.Equal(snowtest.Accepted, block.Status)

	_, contains = tr.Get(block)
	require.False(contains)
}

func TestAcceptBlockConflict(t *testing.T) {
	require := require.New(t)

	tr := New()

	blockToAccept := snowmantest.BuildChild(snowmantest.Genesis)
	blockToReject := snowmantest.BuildChild(snowmantest.Genesis)

	// add conflicting blocks
	tr.Add(blockToAccept)
	_, contains := tr.Get(blockToAccept)
	require.True(contains)

	tr.Add(blockToReject)
	_, contains = tr.Get(blockToReject)
	require.True(contains)

	// accept one of them
	require.NoError(tr.Accept(t.Context(), blockToAccept))

	// check their statuses and that they are removed from the tree
	require.Equal(snowtest.Accepted, blockToAccept.Status)
	_, contains = tr.Get(blockToAccept)
	require.False(contains)

	require.Equal(snowtest.Rejected, blockToReject.Status)
	_, contains = tr.Get(blockToReject)
	require.False(contains)
}

func TestAcceptChainConflict(t *testing.T) {
	require := require.New(t)

	tr := New()

	blockToAccept := snowmantest.BuildChild(snowmantest.Genesis)
	blockToReject := snowmantest.BuildChild(snowmantest.Genesis)
	blockToRejectChild := snowmantest.BuildChild(blockToReject)

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
	require.NoError(tr.Accept(t.Context(), blockToAccept))

	// check their statuses and whether they are removed from tree
	require.Equal(snowtest.Accepted, blockToAccept.Status)
	_, contains = tr.Get(blockToAccept)
	require.False(contains)

	require.Equal(snowtest.Rejected, blockToReject.Status)
	_, contains = tr.Get(blockToReject)
	require.False(contains)

	require.Equal(snowtest.Rejected, blockToRejectChild.Status)
	_, contains = tr.Get(blockToRejectChild)
	require.False(contains)
}
