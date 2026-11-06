// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

// pendingBlocks is a data structure that keeps track of blocks that have been received but not yet accepted.
// It allows for efficient retrieval of blocks by ID and their children by parent ID.
type pendingBlocks struct {
	blockByID    map[ids.ID]snowman.Block
	childrenByID map[ids.ID]map[ids.ID]uint64
}

// newPendingBlocks creates and returns a new instance of pendingBlocks.
func newPendingBlocks() *pendingBlocks {
	return &pendingBlocks{
		blockByID:    make(map[ids.ID]snowman.Block),
		childrenByID: make(map[ids.ID]map[ids.ID]uint64),
	}
}

// Len returns the number of pending blocks currently stored.
func (p *pendingBlocks) Len() int {
	return len(p.blockByID)
}

// Get retrieves a block by its ID. It returns the block and a boolean indicating whether the block was found.
func (p *pendingBlocks) Get(blkID ids.ID) (snowman.Block, bool) {
	blk, ok := p.blockByID[blkID]
	return blk, ok
}

// ChildrenOf returns a map of child block IDs and their corresponding heights for a given parent block ID.
func (p *pendingBlocks) ChildrenOf(blkID ids.ID) map[ids.ID]uint64 {
	return p.childrenByID[blkID]
}

// Add adds a new block to the pendingBlocks structure.
func (p *pendingBlocks) Add(blk snowman.Block) {
	blkID := blk.ID()

	p.blockByID[blkID] = blk
	parentID := blk.Parent()

	childrenOfParent, exists := p.childrenByID[parentID]
	if !exists {
		childrenOfParent = make(map[ids.ID]uint64)
		p.childrenByID[parentID] = childrenOfParent
	}

	// We assume that the block's height makes blk.ID() unique among all possible heights for the block.
	// Meaning, that any block with a different height will have a different ID.
	childrenOfParent[blk.ID()] = blk.Height()
}

// Remove removes a block from pendingBlocks by its ID.
func (p *pendingBlocks) Remove(blkID ids.ID) {
	blk, ok := p.blockByID[blkID]
	if !ok {
		return
	}

	delete(p.blockByID, blkID)

	parent := blk.Parent()
	childrenOfParent, exists := p.childrenByID[parent]
	if exists {
		delete(childrenOfParent, blkID)
		if len(childrenOfParent) == 0 {
			delete(p.childrenByID, parent)
		}
	}
}
