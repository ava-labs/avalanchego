// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package ancestor

import (
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

var _ Tree = (*tree)(nil)

// Tree manages a (potentially partial) view of a tree.
//
// For example, assume this is the full tree:
//
//		A
//	  /	  \
//	B		D
//	|		|
//	C		E
//
// A partial view of this tree may be:
//
//		A
//	  /
//	B		D
//	|		|
//	C		E
//
// Or:
//
//	B		D
//	|		|
//	C		E
//
// This structure is designed to update and traverse these partial views.
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

	// Remove the mapping from blkID to its parentID from the tree.
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

func (t *tree) Add(blkID ids.ID, parentID ids.ID) {
	t.childToParent[blkID] = parentID

	children := t.parentToChildren[parentID]
	children.Add(blkID)
	t.parentToChildren[parentID] = children
}

func (t *tree) Has(blkID ids.ID) bool {
	_, ok := t.childToParent[blkID]
	return ok
}

func (t *tree) GetAncestor(blkID ids.ID) ids.ID {
	for {
		parentID, ok := t.childToParent[blkID]
		// this is the furthest parent available, break loop and return blkID
		if !ok {
			return blkID
		}
		// continue to loop with parentID
		blkID = parentID
	}
}

func (t *tree) Remove(blkID ids.ID) {
	parent, ok := t.childToParent[blkID]
	if !ok {
		return
	}
	delete(t.childToParent, blkID)
	// remove blkID from children
	children := t.parentToChildren[parent]
	children.Remove(blkID)
	// this parent has no more children, remove it from map
	if children.Len() == 0 {
		delete(t.parentToChildren, parent)
	}
}

func (t *tree) RemoveDescendants(blkID ids.ID) {
	childrenList := []ids.ID{blkID}
	for len(childrenList) > 0 {
		newChildrenSize := len(childrenList) - 1
		childID := childrenList[newChildrenSize]
		childrenList = childrenList[:newChildrenSize]
		t.Remove(childID)
		// get children of child
		for grandChildID := range t.parentToChildren[childID] {
			childrenList = append(childrenList, grandChildID)
		}
	}
}

func (t *tree) Len() int {
	return len(t.childToParent)
}
