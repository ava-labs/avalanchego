// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman/snowmantest"
)

func TestPendingBlocksEmpty(t *testing.T) {
	require := require.New(t)

	p := newPendingBlocks()
	require.Zero(p.Len())

	unknownID := ids.GenerateTestID()
	blk, ok := p.Get(unknownID)
	require.False(ok)
	require.Nil(blk)
	require.Empty(p.ChildrenOf(unknownID))
}

func TestPendingBlocksAdd(t *testing.T) {
	require := require.New(t)

	p := newPendingBlocks()
	child := snowmantest.BuildChild(snowmantest.Genesis)
	p.Add(child)

	require.Equal(1, p.Len())
	got, ok := p.Get(child.ID())
	require.True(ok)
	require.Equal(child, got)

	require.Equal(map[ids.ID]uint64{child.ID(): child.Height()}, p.ChildrenOf(snowmantest.GenesisID))
	require.Empty(p.ChildrenOf(child.ID()))
}

func TestPendingBlocksAddIsIdempotent(t *testing.T) {
	require := require.New(t)

	p := newPendingBlocks()
	child := snowmantest.BuildChild(snowmantest.Genesis)
	p.Add(child)
	p.Add(child)

	require.Equal(1, p.Len())
	require.Equal(map[ids.ID]uint64{child.ID(): child.Height()}, p.ChildrenOf(snowmantest.GenesisID))
}

func TestPendingBlocksChildThenParent(t *testing.T) {
	require := require.New(t)

	p := newPendingBlocks()
	parent := snowmantest.BuildChild(snowmantest.Genesis)
	child := snowmantest.BuildChild(parent)
	p.Add(child)

	// Even though the parent hasn't been added yet, the child is still indexed under its parent.
	_, ok := p.Get(parent.ID())
	require.False(ok)
	require.Equal(map[ids.ID]uint64{child.ID(): child.Height()}, p.ChildrenOf(parent.ID()))

	p.Add(parent)
	require.Equal(map[ids.ID]uint64{child.ID(): child.Height()}, p.ChildrenOf(parent.ID()))
}

func TestPendingBlocksSiblings(t *testing.T) {
	require := require.New(t)

	p := newPendingBlocks()
	parent := snowmantest.BuildChild(snowmantest.Genesis)
	a := snowmantest.BuildChild(parent)
	b := snowmantest.BuildChild(parent)
	b.HeightV = a.Height() + 5
	p.Add(a)
	p.Add(b)

	require.Equal(map[ids.ID]uint64{
		a.ID(): a.Height(),
		b.ID(): b.Height(),
	}, p.ChildrenOf(parent.ID()))
}

func TestPendingBlocksChain(t *testing.T) {
	require := require.New(t)

	p := newPendingBlocks()
	chain := snowmantest.BuildDescendants(snowmantest.Genesis, 3)
	for _, blk := range chain {
		p.Add(blk)
	}

	// Each block is listed only under its direct parent.
	require.Equal(map[ids.ID]uint64{chain[0].ID(): chain[0].Height()}, p.ChildrenOf(snowmantest.GenesisID))
	require.Equal(map[ids.ID]uint64{chain[1].ID(): chain[1].Height()}, p.ChildrenOf(chain[0].ID()))
	require.Equal(map[ids.ID]uint64{chain[2].ID(): chain[2].Height()}, p.ChildrenOf(chain[1].ID()))
	require.Empty(p.ChildrenOf(chain[2].ID()))
}

func TestPendingBlocksRemove(t *testing.T) {
	require := require.New(t)

	p := newPendingBlocks()
	child := snowmantest.BuildChild(snowmantest.Genesis)
	p.Add(child)
	p.Remove(child.ID())

	require.Zero(p.Len())
	_, ok := p.Get(child.ID())
	require.False(ok)

	// Removing the last child of a parent frees the parent's entry rather than leaving an empty map behind
	require.Empty(p.ChildrenOf(snowmantest.GenesisID))
	require.Empty(p.childrenByID)
}

func TestPendingBlocksRemoveDetachesFromParent(t *testing.T) {
	require := require.New(t)

	p := newPendingBlocks()
	parent := snowmantest.BuildChild(snowmantest.Genesis)
	a := snowmantest.BuildChild(parent)
	b := snowmantest.BuildChild(parent)
	p.Add(a)
	p.Add(b)
	p.Remove(a.ID())

	require.Equal(1, p.Len())
	require.Equal(map[ids.ID]uint64{b.ID(): b.Height()}, p.ChildrenOf(parent.ID()))
}

func TestPendingBlocksRemoveParentKeepsChild(t *testing.T) {
	require := require.New(t)

	p := newPendingBlocks()
	parent := snowmantest.BuildChild(snowmantest.Genesis)
	child := snowmantest.BuildChild(parent)
	p.Add(parent)
	p.Add(child)
	p.Remove(parent.ID())

	require.Equal(1, p.Len())
	_, ok := p.Get(parent.ID())
	require.False(ok)
	got, ok := p.Get(child.ID())
	require.True(ok)
	require.Equal(child, got)
	require.Equal(map[ids.ID]uint64{child.ID(): child.Height()}, p.ChildrenOf(parent.ID()))
}

func TestPendingBlocksRemoveUnknownIsNoOp(t *testing.T) {
	require := require.New(t)

	p := newPendingBlocks()
	child := snowmantest.BuildChild(snowmantest.Genesis)
	p.Add(child)

	p.Remove(ids.GenerateTestID())

	require.Equal(1, p.Len())
	got, ok := p.Get(child.ID())
	require.True(ok)
	require.Equal(child, got)
	require.Equal(map[ids.ID]uint64{child.ID(): child.Height()}, p.ChildrenOf(snowmantest.GenesisID))
}

func TestPendingBlocksReAddAfterRemove(t *testing.T) {
	require := require.New(t)

	p := newPendingBlocks()
	child := snowmantest.BuildChild(snowmantest.Genesis)
	p.Add(child)
	p.Remove(child.ID())
	p.Add(child)

	require.Equal(1, p.Len())
	got, ok := p.Get(child.ID())
	require.True(ok)
	require.Equal(child, got)
	require.Equal(map[ids.ID]uint64{child.ID(): child.Height()}, p.ChildrenOf(snowmantest.GenesisID))
}
