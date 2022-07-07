// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package stateful

// var _ conflictChecker = &conflictCheckerImpl{}

// type conflictChecker interface {
// 	conflictsProposalBlock(b *stateless.ProposalBlock, s ids.Set) (bool, error)
// 	conflictsAtomicBlock(b *stateless.AtomicBlock, s ids.Set) (bool, error)
// 	conflictsCommitBlock(b *stateless.CommitBlock, s ids.Set) (bool, error)
// 	conflictsAbortBlock(b *stateless.AbortBlock, s ids.Set) (bool, error)
// 	conflictsStandardBlock(b *stateless.StandardBlock, s ids.Set) (bool, error)
// 	// conflictsCommonBlock(b *stateless.commonBlock, s ids.Set) (bool, error)
// }

// type conflictCheckerImpl struct {
// 	backend
// }

// func (c *conflictCheckerImpl) conflictsAtomicBlock(b *stateless.AtomicBlock, s ids.Set) (bool, error) {
// 	if b.Status() == choices.Accepted {
// 		return false, nil
// 	}
// 	// inputs := c.blkIDToInputs[b.ID()]
// 	blockState, ok := c.blkIDToState[b.ID()]
// 	if !ok {
// 		// TODO do we need this check?
// 		return false, fmt.Errorf("couldn't find state for block %s", b.ID())
// 	}
// 	inputs := blockState.inputs
// 	if inputs.Overlaps(s) {
// 		return true, nil
// 	}
// 	// parent, err := c.parent(b.baseBlk)
// 	// if err != nil {
// 	// 	return false, err
// 	// }
// 	parent, err := c.GetStatefulBlock(b.Parent())
// 	if err != nil {
// 		return false, err
// 	}
// 	return parent.conflicts(s)
// }

// func (c *conflictCheckerImpl) conflictsStandardBlock(b *stateless.StandardBlock, s ids.Set) (bool, error) {
// 	// TODO remove
// 	// if b.status == choices.Accepted {
// 	// 	return false, nil
// 	// }
// 	blkID := b.ID()
// 	// if status := c.blkIDToStatus[blkID]; status == choices.Accepted {
// 	// 	return false, nil
// 	// }
// 	// inputs := c.blkIDToInputs[b.ID()]
// 	blockState, ok := c.blkIDToState[blkID]
// 	if !ok {
// 		// TODO do we need this check?
// 		return false, fmt.Errorf("couldn't find state for block %s", blkID)
// 	}
// 	inputs := blockState.inputs
// 	if inputs.Overlaps(s) {
// 		return true, nil
// 	}
// 	// parent, err := c.parent(b.baseBlk)
// 	// if err != nil {
// 	// 	return false, err
// 	// }
// 	parent, err := c.GetStatefulBlock(b.Parent())
// 	if err != nil {
// 		return false, err
// 	}
// 	return parent.conflicts(s)
// }

// func (c *conflictCheckerImpl) conflictsProposalBlock(b *stateless.ProposalBlock, s ids.Set) (bool, error) {
// 	return c.conflictsCommonBlock(b.CommonBlock, s)
// }

// func (c *conflictCheckerImpl) conflictsAbortBlock(b *stateless.AbortBlock, s ids.Set) (bool, error) {
// 	return c.conflictsCommonBlock(b.CommonBlock, s)
// }

// func (c *conflictCheckerImpl) conflictsCommitBlock(b *stateless.CommitBlock, s ids.Set) (bool, error) {
// 	return c.conflictsCommonBlock(b.CommonBlock, s)
// }

// func (c *conflictCheckerImpl) conflictsCommonBlock(b stateless.CommonBlock, s ids.Set) (bool, error) {
// 	if b.Status() == choices.Accepted {
// 		return false, nil
// 	}
// 	/* TODO remove
// 	parent, err := c.backend.parent(b.baseBlk)
// 	if err != nil {
// 		return false, err
// 	}
// 	*/
// 	parent, err := c.GetStatefulBlock(b.Parent())
// 	if err != nil {
// 		return false, err
// 	}
// 	return parent.conflicts(s)
// }
