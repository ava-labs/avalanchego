// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tree

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/snowman"
)

type Tree interface {
	Add(snowman.Block)
	Contains(snowman.Block) bool
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
	parent := blk.Parent()
	parentID := parent.ID()
	children, exists := t.nodes[parentID]
	if !exists {
		children = make(map[ids.ID]snowman.Block)
		t.nodes[parentID] = children
	}
	blkID := blk.ID()
	children[blkID] = blk
}

func (t *tree) Contains(blk snowman.Block) bool {
	parent := blk.Parent()
	parentID := parent.ID()
	children := t.nodes[parentID]
	blkID := blk.ID()
	_, exists := children[blkID]
	return exists
}

func (t *tree) Accept(blk snowman.Block) error {
	// accept the provided block
	if err := blk.Accept(); err != nil {
		return err
	}

	// get the siblings of the block
	parent := blk.Parent()
	parentID := parent.ID()
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
