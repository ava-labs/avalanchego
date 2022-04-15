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
)

type AncestorTree interface {
	Add(blkID ids.ID, parentID ids.ID)
	Has(blkID ids.ID) bool
	GetRoot(blkID ids.ID) ids.ID
	Remove(blkID ids.ID)
	RemoveSubtree(blkID ids.ID)
	Len() int
}

type ancestorTree struct {
	childToParent    map[ids.ID]ids.ID
	parentToChildren map[ids.ID]ids.Set
}

func NewAncestorTree() AncestorTree {
	return &ancestorTree{
		childToParent:    make(map[ids.ID]ids.ID),
		parentToChildren: make(map[ids.ID]ids.Set),
	}
}

// Add maps given blkID to given parentID
func (p *ancestorTree) Add(blkID ids.ID, parentID ids.ID) {
	p.childToParent[blkID] = parentID

	children := p.parentToChildren[parentID]
	children.Add(blkID)
	p.parentToChildren[parentID] = children
}

// GetRoot returns the oldest parent of blkID, might return blkID if no parent is available.
func (p *ancestorTree) GetRoot(blkID ids.ID) ids.ID {
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

// Has returns if blkID is in the tree or not
func (p *ancestorTree) Has(blkID ids.ID) bool {
	_, ok := p.childToParent[blkID]
	return ok
}

// Remove removes blkID from the tree
func (p *ancestorTree) Remove(blkID ids.ID) {
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

// Returns tree length
func (p *ancestorTree) Len() int {
	return len(p.childToParent)
}

// RemoveSubtree removes whole subtree that blkID holds
func (p *ancestorTree) RemoveSubtree(blkID ids.ID) {
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
