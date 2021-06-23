package proposervm

import (
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/choices"
)

type node struct {
	proChildren   []*ProposerBlock
	verifiedCores map[ids.ID]struct{} // set of already verified core IDs
}

type BlkTree struct {
	vm   *VM
	tree map[ids.ID](node)
}

func (t *BlkTree) Initialize(vm *VM, rootID ids.ID) {
	t.vm = vm
	t.tree = make(map[ids.ID]node)
	t.tree[rootID] = node{
		proChildren:   make([]*ProposerBlock, 0),
		verifiedCores: make(map[ids.ID]struct{}),
	}
}

func (t *BlkTree) propagateStatusFrom(pb *ProposerBlock) error {
	prntID := pb.Parent().ID()
	node, found := t.tree[prntID]
	if !found {
		return ErrFailedHandlingConflicts
	}

	lastAcceptedID, err := t.vm.state.getLastAcceptedID()
	if err != nil {
		return ErrFailedHandlingConflicts
	}

	lastAcceptedBlk, err := t.vm.state.getProBlock(lastAcceptedID)
	if err != nil {
		return ErrFailedHandlingConflicts
	}

	queue := make([]*ProposerBlock, 0)
	queue = append(queue, node.proChildren...)

	// just level order descent
	for len(queue) != 0 {
		node := queue[0]
		queue = queue[1:]

		if node.coreBlk.ID() != lastAcceptedBlk.coreBlk.ID() {
			if node.coreBlk.Parent().ID() == lastAcceptedBlk.coreBlk.Parent().ID() ||
				node.coreBlk.Parent().Status() == choices.Rejected {
				if err := node.coreReject(); err != nil {
					return ErrFailedHandlingConflicts
				}
			}
		}

		childrenNodes := t.tree[node.ID()].proChildren
		queue = append(queue, childrenNodes...)
	}
	return nil
}

func (t *BlkTree) updateWithAcceptedBlk(blk *ProposerBlock) {
	delete(t.tree, blk.Parent().ID())
}

func (t *BlkTree) iscoreBlkVerified(proBlk *ProposerBlock) bool {
	treeNode, found := t.tree[proBlk.Parent().ID()]
	if !found {
		return false
	}

	_, verified := treeNode.verifiedCores[proBlk.coreBlk.ID()]
	return verified
}

func (t *BlkTree) addVerifiedBlk(proBlk *ProposerBlock) error {
	treeNode, found := t.tree[proBlk.Parent().ID()]
	if !found {
		return fmt.Errorf("attempt to add block with no parent")
	}
	treeNode.proChildren = append(treeNode.proChildren, proBlk)
	if treeNode.verifiedCores == nil {
		treeNode.verifiedCores = make(map[ids.ID]struct{})
	}
	treeNode.verifiedCores[proBlk.coreBlk.ID()] = struct{}{}

	t.tree[proBlk.Parent().ID()] = treeNode
	t.tree[proBlk.ID()] = node{
		proChildren:   make([]*ProposerBlock, 0),
		verifiedCores: make(map[ids.ID]struct{}),
	}
	return nil
}
