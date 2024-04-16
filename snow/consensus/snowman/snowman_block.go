// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
)

// Tracks the state of a snowman block
type snowmanBlock struct {
	// parameters to initialize the snowball instance with
	params snowball.Parameters

	// block that this node contains. For the genesis, this value will be nil
	blk Block

	// shouldFalter is set to true if this node, and all its descendants received
	// less than Alpha votes
	shouldFalter bool

	// sb is the snowball instance used to decide which child is the canonical
	// child of this block. If this node has not had a child issued under it,
	// this value will be nil
	sb snowball.Consensus

	// children is the set of blocks that have been issued that name this block
	// as their parent. If this node has not had a child issued under it, this value
	// will be nil
	children map[ids.ID]Block
}

func (n *snowmanBlock) AddChild(child Block) {
	childID := child.ID()

	// if the snowball instance is nil, this is the first child. So the instance
	// should be initialized.
	if n.sb == nil {
		n.sb = snowball.NewTree(snowball.SnowballFactory, n.params, childID)
		n.children = make(map[ids.ID]Block)
	} else {
		n.sb.Add(childID)
	}

	n.children[childID] = child
}

func (n *snowmanBlock) Accepted() bool {
	// if the block is nil, then this is the genesis which is defined as
	// accepted
	if n.blk == nil {
		return true
	}
	return n.blk.Status() == choices.Accepted
}
