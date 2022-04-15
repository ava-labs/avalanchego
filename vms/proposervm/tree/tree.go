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
	"github.com/chain4travel/caminogo/ids"
	"github.com/chain4travel/caminogo/snow/consensus/snowman"
)

type Tree interface {
	// Add places the block in the tree
	Add(snowman.Block)

	// Get returns the block that was added to this tree whose parent and ID
	// match the provided block. If non-exists, then false will be returned.
	Get(snowman.Block) (snowman.Block, bool)

	// Accept marks the provided block as accepted and rejects every conflicting
	// block.
	Accept(snowman.Block) error
}

type tree struct {
	// parentID -> childID -> childBlock
	nodes map[ids.ID]map[ids.ID]snowman.Block
}

func New() Tree {
	return &tree{
		nodes: make(map[ids.ID]map[ids.ID]snowman.Block),
	}
}

func (t *tree) Add(blk snowman.Block) {
	parentID := blk.Parent()
	children, exists := t.nodes[parentID]
	if !exists {
		children = make(map[ids.ID]snowman.Block)
		t.nodes[parentID] = children
	}
	blkID := blk.ID()
	children[blkID] = blk
}

func (t *tree) Get(blk snowman.Block) (snowman.Block, bool) {
	parentID := blk.Parent()
	children := t.nodes[parentID]
	blkID := blk.ID()
	originalBlk, exists := children[blkID]
	return originalBlk, exists
}

func (t *tree) Accept(blk snowman.Block) error {
	// accept the provided block
	if err := blk.Accept(); err != nil {
		return err
	}

	// get the siblings of the block
	parentID := blk.Parent()
	children := t.nodes[parentID]
	blkID := blk.ID()
	delete(children, blkID)
	delete(t.nodes, parentID)

	// mark the siblings of the accepted block as rejectable
	childrenToReject := make([]snowman.Block, 0, len(children))
	for _, child := range children {
		childrenToReject = append(childrenToReject, child)
	}

	// reject all the rejectable blocks
	for len(childrenToReject) > 0 {
		i := len(childrenToReject) - 1
		child := childrenToReject[i]
		childrenToReject = childrenToReject[:i]

		// reject the block
		if err := child.Reject(); err != nil {
			return err
		}

		// mark the progeny of this block as being rejectable
		blkID := child.ID()
		children := t.nodes[blkID]
		for _, child := range children {
			childrenToReject = append(childrenToReject, child)
		}
		delete(t.nodes, blkID)
	}
	return nil
}
