// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ancestor

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

var _ Tree = (*tree)(nil)

type Tree interface {
	// Add a mapping from blkID to parentID.
	//
	// Invariant: blkID must not be equal to parentID
	// Invariant: a given blkID must only ever have one parentID
	Add(blkID ids.ID, parentID ids.ID)

	// Has returns if blkID's parentID is known by the tree.
	Has(blkID ids.ID) bool

	// GetAncestor returns the oldest known ancestor of blkID. If there is no
	// known parentID of blkID, blkID will be returned.
	GetAncestor(blkID ids.ID) ids.ID

	// Remove blkID from the tree.
	Remove(blkID ids.ID)

	// RemoveDescendants removes blkID from the tree along with all of its known
	// descendants.
	RemoveDescendants(blkID ids.ID)

	// Len returns the total number of blkID to parentID mappings that are
	// currently tracked by the tree.
	Len() int
}

type tree struct {
	childToParent    map[ids.ID]ids.ID
	parentToChildren map[ids.ID]set.Set[ids.ID]
}

func NewTree() Tree {
	return &tree{
		childToParent:    make(map[ids.ID]ids.ID),
		parentToChildren: make(map[ids.ID]set.Set[ids.ID]),
	}
}

func (p *tree) Add(blkID ids.ID, parentID ids.ID) {
	p.childToParent[blkID] = parentID

	children := p.parentToChildren[parentID]
	children.Add(blkID)
	p.parentToChildren[parentID] = children
}

func (p *tree) Has(blkID ids.ID) bool {
	_, ok := p.childToParent[blkID]
	return ok
}

func (p *tree) GetAncestor(blkID ids.ID) ids.ID {
	for {
		parentID, ok := p.childToParent[blkID]
		// this is the furthest parent available, break loop and return blkID
		if !ok {
			return blkID
		}
		// continue to loop with parentID
		blkID = parentID
	}
}

func (p *tree) Remove(blkID ids.ID) {
	parent, ok := p.childToParent[blkID]
	if !ok {
		return
	}
	delete(p.childToParent, blkID)
	// remove blkID from children
	children := p.parentToChildren[parent]
	children.Remove(blkID)
	// this parent has no more children, remove it from map
	if children.Len() == 0 {
		delete(p.parentToChildren, parent)
	}
}

func (p *tree) RemoveDescendants(blkID ids.ID) {
	childrenList := []ids.ID{blkID}
	for len(childrenList) > 0 {
		newChildrenSize := len(childrenList) - 1
		childID := childrenList[newChildrenSize]
		childrenList = childrenList[:newChildrenSize]
		p.Remove(childID)
		// get children of child
		for grandChildID := range p.parentToChildren[childID] {
			childrenList = append(childrenList, grandChildID)
		}
	}
}

func (p *tree) Len() int {
	return len(p.childToParent)
}
