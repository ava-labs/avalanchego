// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"github.com/ava-labs/avalanchego/ids"
)

type AncestorTree interface {
	Add(blkID ids.ID, parentID ids.ID)
	Has(blkID ids.ID) bool
	GetRoot(blkID ids.ID) (ids.ID, bool)
	Remove(blkID ids.ID)
	RemoveSubtree(blkID ids.ID)
}

type childParentMap struct {
	childToParent    map[ids.ID]ids.ID
	parentToChildren map[ids.ID]ids.Set
}

func NewAncestorTree() AncestorTree {
	return &childParentMap{
		childToParent:    make(map[ids.ID]ids.ID),
		parentToChildren: make(map[ids.ID]ids.Set),
	}
}

// Add maps given blkID to given parentID
func (p *childParentMap) Add(blkID ids.ID, parentID ids.ID) {
	p.childToParent[blkID] = parentID

	children := p.parentToChildren[parentID]
	children.Add(blkID)
	p.parentToChildren[parentID] = children
}

// GetRoot returns the oldest parent of blkID
func (p *childParentMap) GetRoot(blkID ids.ID) (ids.ID, bool) {
	// return false if we cannot find any parent
	currentID, ok := p.childToParent[blkID]
	if !ok {
		return currentID, false
	}
	for {
		parentID, ok := p.childToParent[currentID]
		// this is the furthest parent available, break loop and return currentID
		if !ok {
			return currentID, true
		}
		// continue to loop with parentID
		currentID = parentID
	}
}

// Has returns if blkID is in the tree or not
func (p *childParentMap) Has(blkID ids.ID) bool {
	_, ok := p.childToParent[blkID]
	return ok
}

// Remove removes blkID from the tree
func (p *childParentMap) Remove(blkID ids.ID) {
	p.remove(blkID)
}

// RemoveSubtree removes whole subtree that blkID holds
func (p *childParentMap) RemoveSubtree(blkID ids.ID) {
	childrenList := []ids.ID{blkID}
	for len(childrenList) > 0 {
		newChildrenSize := len(childrenList) - 1
		childID := childrenList[newChildrenSize]
		childrenList = childrenList[:newChildrenSize]
		p.remove(childID)
		// get children of child
		for grandChildID := range p.parentToChildren[childID] {
			childrenList = append(childrenList, grandChildID)
		}
	}
}

// remove is a helper to remove blkID from tree
func (p *childParentMap) remove(blkID ids.ID) {
	parent, ok := p.childToParent[blkID]
	if !ok {
		return
	}
	// remove blkID from children
	children, ok := p.parentToChildren[parent]
	if ok {
		children.Remove(blkID)
		// this parent has no more children, remove it from map
		if children.Len() == 0 {
			delete(p.parentToChildren, parent)
		}
	}
	// finally delete actual blkID
	delete(p.childToParent, blkID)
}
