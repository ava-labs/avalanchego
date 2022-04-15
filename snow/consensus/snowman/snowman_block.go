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

package snowman

import (
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/snow/choices"
	"github.com/chain4travel/caminogo/snow/consensus/snowball"
)

// Tracks the state of a snowman block
type snowmanBlock struct {
	// pointer to the snowman instance this node is managed by
	sm Consensus

	// block that this node contains. For the genesis, this value will be nil
	blk Block

	// shouldFalter is set to true if this node, and all its descendants received
	// less than Alpha votes
	shouldFalter bool

	// sb is the snowball instance used to decided which child is the canonical
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
		n.sb = &snowball.Tree{}
		n.sb.Initialize(n.sm.Parameters(), childID)
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
