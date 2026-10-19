// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

type pendingBlocks struct {
	blockByID    map[ids.ID]snowman.Block
	childrenByID map[ids.ID]map[ids.ID]uint64
}

func newPendingBlocks() *pendingBlocks {
	return &pendingBlocks{
		blockByID:    make(map[ids.ID]snowman.Block),
		childrenByID: make(map[ids.ID]map[ids.ID]uint64),
	}
}

func (p *pendingBlocks) Len() int {
	return len(p.blockByID)
}

func (p *pendingBlocks) Get(blkID ids.ID) (snowman.Block, bool) {
	blk, ok := p.blockByID[blkID]
	return blk, ok
}

func (p *pendingBlocks) ChildrenOf(blkID ids.ID) map[ids.ID]uint64 {
	return p.childrenByID[blkID]
}

func (p *pendingBlocks) Add(blk snowman.Block) {
	blkID := blk.ID()

	p.blockByID[blkID] = blk
	parentID := blk.Parent()

	childrenOfParent, exists := p.childrenByID[parentID]
	if !exists {
		childrenOfParent = make(map[ids.ID]uint64)
		p.childrenByID[parentID] = childrenOfParent
	}

	childrenOfParent[blk.ID()] = blk.Height()
}

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
