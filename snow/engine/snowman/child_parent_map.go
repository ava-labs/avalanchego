// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package snowman

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
)

type AncestorTree interface {
	Add(blkID ids.ID, parentID ids.ID) error
	GetParent(blkID ids.ID) (ids.ID, bool)
	Has(blkID ids.ID) bool
	GetOldestAncestor(blkID ids.ID) (ids.ID, error)
	Remove(blkID ids.ID)
	Purge(blkID ids.ID)
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

func (p *childParentMap) Add(blkID ids.ID, parentID ids.ID) error {
	// ASK: should we consider cases for circular dependencies or self reference etc?
	p.childToParent[blkID] = parentID
	children, ok := p.parentToChildren[parentID]
	if !ok {
		children = ids.NewSet(1)
	}
	children.Add(blkID)
	p.parentToChildren[parentID] = children
	return nil
}

func (p *childParentMap) GetParent(blkID ids.ID) (ids.ID, bool) {
	parentID, ok := p.childToParent[blkID]
	return parentID, ok
}

func (p *childParentMap) GetOldestAncestor(blkID ids.ID) (ids.ID, error) {
	// might be ends up in an infinite loop in case of circular dependency
	currentID := blkID
	for {
		parentID, ok := p.childToParent[currentID]
		switch {
		case !ok:
			return currentID, nil
		case parentID == blkID:
			return ids.ID{}, fmt.Errorf("found circularity")
		}
		currentID = parentID
	}
}

func (p *childParentMap) Has(blkID ids.ID) bool {
	_, ok := p.childToParent[blkID]
	return ok
}

func (p *childParentMap) Remove(blkID ids.ID) {
	p.remove(blkID)
}

func (p *childParentMap) Purge(blkID ids.ID) {
	p.remove(blkID)
	children, ok := p.parentToChildren[blkID]
	if !ok {
		return
	}
	childrenList := children.List()
	for len(childrenList) > 0 {
		newChildrenSize := len(childrenList) - 1
		childID := childrenList[newChildrenSize]
		childrenList = childrenList[:newChildrenSize]
		p.remove(childID)
		for grandChildID := range p.parentToChildren[childID] {
			childrenList = append(childrenList, grandChildID)
		}
	}
}

func (p *childParentMap) remove(blkID ids.ID) {
	parent, ok := p.childToParent[blkID]
	if !ok {
		return
	}
	children, ok := p.parentToChildren[parent]
	if ok {
		children.Remove(blkID)
		if children.Len() == 0 {
			delete(p.parentToChildren, parent)
		}
	}
	delete(p.childToParent, blkID)
}
